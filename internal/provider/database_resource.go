package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// databaseResource manages local databases and consumer databases bound to datashares.
type databaseResource struct {
	// resourceClient provides SQL execution and warehouse ownership checks.
	resourceClient
}

// databaseModel is the Terraform state for a local or shared database.
type databaseModel struct {
	// ID binds the database to its administration warehouse and database.
	ID types.String `tfsdk:"id"`
	// Name identifies the managed database.
	Name types.String `tfsdk:"name"`
	// DatashareARN selects a producer share; null means a local database.
	DatashareARN types.String `tfsdk:"datashare_arn"`
	// WithPermissions requires explicit object grants on shared databases.
	WithPermissions types.Bool `tfsdk:"with_permissions"`
	// DatabaseType reports whether the database is local or shared.
	DatabaseType types.String `tfsdk:"database_type"`
	// ShareName identifies the backing producer share, when shared.
	ShareName types.String `tfsdk:"share_name"`
	// ProducerAccount identifies the backing share's AWS account.
	ProducerAccount types.String `tfsdk:"producer_account"`
	// ProducerNamespace identifies the backing share's warehouse namespace.
	ProducerNamespace types.String `tfsdk:"producer_namespace"`
}

// shareSource is the producer identity encoded by a datashare ARN.
type shareSource struct {
	// Account is the producer AWS account ID.
	Account string
	// Namespace is the producer warehouse namespace UUID.
	Namespace string
	// Name is the producer SQL datashare name.
	Name string
}

// sharedDatabaseOptions decodes the complete SHOW DATABASES parameters for a shared database.
type sharedDatabaseOptions struct {
	// ShareName identifies the backing datashare.
	ShareName string `json:"datashare_name"`
	// ProducerAccount identifies the producer AWS account.
	ProducerAccount string `json:"datashare_producer_account"`
	// ProducerNamespace identifies the producer namespace UUID.
	ProducerNamespace string `json:"datashare_producer_namespace"`
	// Permissions is required to distinguish explicit isolation from missing metadata.
	Permissions *bool `json:"permissions"`
}

// parseShare extracts the producer account, namespace, and share name from a datashare ARN.
func parseShare(value string) (shareSource, error) {
	parsed, err := arn.Parse(value)
	if err != nil || parsed.Service != "redshift" || parsed.Partition == "" || parsed.Region == "" || parsed.AccountID == "" || !strings.HasPrefix(parsed.Resource, "datashare:") {
		return shareSource{}, fmt.Errorf("invalid Redshift datashare ARN %q", value)
	}
	namespace, name, ok := strings.Cut(strings.TrimPrefix(parsed.Resource, "datashare:"), "/")
	if !ok || namespace == "" || name == "" {
		return shareSource{}, fmt.Errorf("datashare ARN must include a producer namespace and share name")
	}
	return shareSource{Account: parsed.AccountID, Namespace: namespace, Name: name}, nil
}

// newDatabaseResource constructs a local or datashare-backed database handler.
func newDatabaseResource() resource.Resource { return &databaseResource{} }

// Metadata identifies the database resource to Terraform.
func (r *databaseResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_database"
}

// Schema defines database identity, optional share binding, and permission mode.
func (r *databaseResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a local database or a consumer database bound to a producer datashare.",
		Attributes: map[string]schema.Attribute{
			"id":                 idAttribute(),
			"database_type":      schema.StringAttribute{Computed: true, MarkdownDescription: "`local` or `shared`."},
			"share_name":         schema.StringAttribute{Computed: true, MarkdownDescription: "Producer share name; null for local databases."},
			"producer_account":   schema.StringAttribute{Computed: true, MarkdownDescription: "Producer account ID; null for local databases."},
			"producer_namespace": schema.StringAttribute{Computed: true, MarkdownDescription: "Producer namespace ID; null for local databases."},
			"name": schema.StringAttribute{
				Required: true, MarkdownDescription: "Database name; changing it replaces the resource.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"datashare_arn": schema.StringAttribute{
				Optional: true, MarkdownDescription: "Producer datashare ARN. Omit for a local database; associate the share through AWS before creating a consumer database. Changing it replaces the resource.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"with_permissions": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(true),
				MarkdownDescription: "Require object-level grants for a shared database; defaults to `true`. Changing it replaces a shared database (`datashare_arn` set); ignored for local databases.",
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplaceIf(
					sharedDatabaseReplacement, "Replaces shared databases; ignored for local databases.", "Replaces shared databases; ignored for local databases.",
				)},
			},
		},
	}
}

// sharedDatabaseReplacement replaces only shared databases, because local databases ignore with_permissions.
// A state whose datashare_arn cannot be read is treated as shared so replacement is never skipped silently.
func sharedDatabaseReplacement(ctx context.Context, req planmodifier.BoolRequest, resp *boolplanmodifier.RequiresReplaceIfFuncResponse) {
	var arn types.String
	if diagnostics := req.State.GetAttribute(ctx, path.Root("datashare_arn"), &arn); diagnostics.HasError() {
		resp.RequiresReplace = true
		return
	}
	resp.RequiresReplace = !arn.IsNull()
}

// databaseMetadata reads and decodes the same catalog-backed attributes for resources and lookups.
func (r *resourceClient) databaseMetadata(ctx context.Context, name string) (databaseModel, bool, error) {
	// SVV_REDSHIFT_DATABASES.database_options is VARCHAR(128) and truncates the
	// producer JSON before its permissions flag. SHOW returns complete parameters.
	rows, err := r.query(ctx, "SHOW DATABASES LIKE "+sqlclient.Literal(name), nil)
	matching := make([]sqlclient.Row, 0, 1)
	for _, row := range rows {
		// LIKE treats underscores/percent signs as wildcards; retain exact names.
		if row["database_name"] == name {
			matching = append(matching, row)
		}
	}
	rows = matching
	if err != nil || len(rows) == 0 {
		return databaseModel{}, false, err
	}
	if len(rows) != 1 || (rows[0]["database_type"] != "local" && rows[0]["database_type"] != "shared") {
		return databaseModel{}, false, fmt.Errorf("expected one local or shared database named %q", name)
	}
	row := rows[0]
	data := databaseModel{
		Name: types.StringValue(row["database_name"]), DatabaseType: types.StringValue(row["database_type"]),
		WithPermissions: types.BoolValue(false),
	}
	if row["database_type"] == "shared" {
		var options sharedDatabaseOptions
		if err := json.Unmarshal([]byte(row["parameters"]), &options); err != nil {
			return databaseModel{}, false, fmt.Errorf("decode shared database %q parameters: %w", name, err)
		}
		if options.Permissions == nil {
			return databaseModel{}, false, fmt.Errorf("shared database %q has no permissions catalog option", name)
		}
		data.WithPermissions = types.BoolValue(*options.Permissions)
		data.ShareName = types.StringValue(strings.TrimSpace(options.ShareName))
		data.ProducerAccount = types.StringValue(strings.TrimSpace(options.ProducerAccount))
		data.ProducerNamespace = types.StringValue(strings.TrimSpace(options.ProducerNamespace))
		if data.ShareName.ValueString() == "" || data.ProducerAccount.ValueString() == "" || data.ProducerNamespace.ValueString() == "" {
			return databaseModel{}, false, fmt.Errorf("shared database %q has no complete inbound producer binding", name)
		}
	}
	return data, true, nil
}

// read verifies local/shared database identity and refreshes its catalog-backed metadata.
func (r *databaseResource) read(ctx context.Context, data *databaseModel) (bool, error) {
	if err := r.bound(data.ID, r.database.ValueString()); err != nil {
		return false, err
	}
	observed, found, err := r.databaseMetadata(ctx, data.Name.ValueString())
	if err != nil || !found {
		return found, err
	}
	observed.ID, observed.DatashareARN = data.ID, data.DatashareARN
	if data.DatashareARN.IsNull() {
		if observed.DatabaseType.ValueString() != "local" {
			return false, fmt.Errorf("database %q is not a local database; migrate it explicitly", data.Name.ValueString())
		}
		// Preserve the ignored local-database argument rather than changing a known Terraform plan value.
		observed.WithPermissions = data.WithPermissions
		if data.WithPermissions.IsNull() || data.WithPermissions.IsUnknown() {
			observed.WithPermissions = types.BoolValue(true)
		}
		*data = observed
		return true, nil
	}
	source, err := parseShare(data.DatashareARN.ValueString())
	if err != nil {
		return false, err
	}
	if observed.DatabaseType.ValueString() != "shared" || observed.ShareName.ValueString() != source.Name ||
		observed.ProducerAccount.ValueString() != source.Account || observed.ProducerNamespace.ValueString() != source.Namespace {
		return false, fmt.Errorf("database %q has an incompatible type or producer binding; migrate it explicitly", data.Name.ValueString())
	}
	*data = observed
	return true, nil
}

// Create creates a local database or waits for its incoming datashare before binding it.
func (r *databaseResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data databaseModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	target, err := r.connection(r.database.ValueString())
	if err != nil || target.Database == data.Name.ValueString() {
		resp.Diagnostics.AddError("Create database", "Configure a local administration database different from the database being created.")
		return
	}
	if data.DatashareARN.IsNull() {
		if _, err := r.query(ctx, "CREATE DATABASE "+sqlclient.Identifier(data.Name.ValueString()), nil); err != nil {
			resp.Diagnostics.AddError("Create database", err.Error())
			return
		}
		data.ID = r.identity(r.database.ValueString(), map[string]string{"name": data.Name.ValueString()})
		data.DatabaseType = types.StringValue("local")
		data.ShareName, data.ProducerAccount, data.ProducerNamespace = types.StringNull(), types.StringNull(), types.StringNull()
		resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
		found, err := r.read(ctx, &data)
		switch {
		case err != nil:
			resp.Diagnostics.AddError("Verify database", err.Error())
		case !found:
			resp.Diagnostics.AddError("Verify database", "The local database is absent after creation.")
		default:
			resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
		}
		return
	}
	source, err := parseShare(data.DatashareARN.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Create shared database", err.Error())
		return
	}
	// AWS association can complete before the share appears in the SQL catalog.
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	for {
		rows, err := r.query(waitCtx,
			"SELECT consumer_database FROM svv_datashares WHERE share_type = 'INBOUND' AND share_name = :share AND producer_account = :account AND producer_namespace = :namespace",
			map[string]string{"share": source.Name, "account": source.Account, "namespace": source.Namespace})
		if err != nil {
			resp.Diagnostics.AddError("Discover associated datashare", err.Error())
			return
		}
		if len(rows) > 0 {
			if strings.TrimSpace(rows[0]["consumer_database"]) != "" {
				resp.Diagnostics.AddError("Create shared database", "The datashare is already bound to a database; import that database instead.")
				return
			}
			break
		}
		select {
		case <-waitCtx.Done():
			resp.Diagnostics.AddError("Discover associated datashare", "The datashare did not become visible within five minutes.")
			return
		case <-time.After(time.Second):
		}
	}
	sql := "CREATE DATABASE " + sqlclient.Identifier(data.Name.ValueString())
	if data.WithPermissions.ValueBool() {
		sql += " WITH PERMISSIONS"
	}
	sql += " FROM DATASHARE " + sqlclient.Identifier(source.Name) + " OF ACCOUNT " + sqlclient.Literal(source.Account) + " NAMESPACE " + sqlclient.Literal(source.Namespace)
	if _, err := r.query(ctx, sql, nil); err != nil {
		resp.Diagnostics.AddError("Create shared database", err.Error())
		return
	}
	data.ID = r.identity(r.database.ValueString(), map[string]string{"name": data.Name.ValueString(), "datashare_arn": data.DatashareARN.ValueString()})
	data.DatabaseType = types.StringValue("shared")
	data.ShareName = types.StringValue(source.Name)
	data.ProducerAccount = types.StringValue(source.Account)
	data.ProducerNamespace = types.StringValue(source.Namespace)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	expectedPermissions := data.WithPermissions
	found, err := r.read(ctx, &data)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	if err != nil {
		resp.Diagnostics.AddError("Verify shared database", err.Error())
		return
	}
	if !found || !data.WithPermissions.Equal(expectedPermissions) {
		resp.Diagnostics.AddError("Verify shared database", "The database is absent or its permission mode does not match after creation.")
		return
	}
}

// Read refreshes database attributes and removes absent databases from state.
func (r *databaseResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data databaseModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, &data)
	if err != nil {
		resp.Diagnostics.AddError("Read shared database", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update verifies an immutable database binding without recreating it.
func (r *databaseResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data databaseModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	expectedPermissions := data.WithPermissions
	found, err := r.read(ctx, &data)
	if err != nil {
		resp.Diagnostics.AddError("Read shared database", err.Error())
		return
	}
	if !found || !data.WithPermissions.Equal(expectedPermissions) {
		resp.Diagnostics.AddError("Update shared database", "Database identity or permission mode changed during the update; refresh the plan.")
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Delete drops the selected database and verifies catalog removal.
func (r *databaseResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data databaseModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, &data)
	if err == nil && found {
		_, err = r.query(ctx, "DROP DATABASE "+sqlclient.Identifier(data.Name.ValueString()), nil)
		if err == nil {
			found, err = r.read(ctx, &data)
			if err == nil && found {
				resp.Diagnostics.AddError("Delete shared database", "The database remains after deletion.")
			}
		}
	}
	if err != nil {
		resp.Diagnostics.AddError("Delete shared database", err.Error())
	}
}

// ImportState restores local or shared database ownership from JSON.
func (r *databaseResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importIdentity(ctx, req, resp, "name")
	var values map[string]string
	if err := json.Unmarshal([]byte(req.ID), &values); err == nil && values["datashare_arn"] != "" {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("datashare_arn"), values["datashare_arn"])...)
	}
}
