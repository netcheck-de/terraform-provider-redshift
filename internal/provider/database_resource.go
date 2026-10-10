package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
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
	// Owner is the SQL user owning a local database; null for shared databases.
	Owner types.String `tfsdk:"owner"`
	// ConnectionLimit caps concurrent connections to a local database; -1 means UNLIMITED.
	ConnectionLimit types.Int64 `tfsdk:"connection_limit"`
	// Collation is the create-only case sensitivity of a local database.
	Collation types.String `tfsdk:"collation"`
	// IsolationLevel is the SERIALIZABLE or SNAPSHOT isolation of a local database.
	IsolationLevel types.String `tfsdk:"isolation_level"`
}

// databaseResourceModel is the resource state: the attributes the data source shares, plus the timeouts only the
// resource has.
type databaseResourceModel struct {
	databaseModel
	// Timeouts bounds create, including the wait for an inbound datashare, update, and delete.
	Timeouts timeouts.Value `tfsdk:"timeouts"`
}

// databaseShareWait bounds the wait for an inbound datashare when no create timeout is configured, so a share that
// was never associated fails instead of blocking the apply. The schema and template spell it out as five minutes.
const databaseShareWait = 5 * time.Minute

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

var _ = registerResource(newDatabaseResource)

// newDatabaseResource constructs a local or datashare-backed database handler.
func newDatabaseResource() resource.Resource { return &databaseResource{} }

// Metadata identifies the database resource to Terraform.
func (r *databaseResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_database"
}

// Schema defines database identity, optional share binding, permission mode, and local database options.
func (r *databaseResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a local database or a consumer database bound to a producer datashare.",
		Attributes: map[string]schema.Attribute{
			"id":                 idAttribute(),
			"database_type":      schema.StringAttribute{Computed: true, MarkdownDescription: "`LOCAL` or `SHARED`."},
			"share_name":         schema.StringAttribute{Computed: true, MarkdownDescription: "Producer share name; null for local databases."},
			"producer_account":   schema.StringAttribute{Computed: true, MarkdownDescription: "Producer account ID; null for local databases."},
			"producer_namespace": schema.StringAttribute{Computed: true, MarkdownDescription: "Producer namespace ID; null for local databases."},
			"name": schema.StringAttribute{
				Required: true, MarkdownDescription: "Database name. Changing it replaces the database.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"datashare_arn": schema.StringAttribute{
				Optional: true, MarkdownDescription: "Producer datashare ARN. Omit for a local database; associate the share through AWS before creating a consumer database. Changing it replaces the database.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"with_permissions": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(true),
				MarkdownDescription: "Require object-level grants for a shared database; defaults to `true`. Changing it replaces a shared database (`datashare_arn` set); ignored for local databases.",
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplaceIf(
					sharedDatabaseReplacement, "Replaces shared databases; ignored for local databases.", "Replaces shared databases; ignored for local databases.",
				)},
			},
			"owner": schema.StringAttribute{
				Optional: true, Computed: true,
				MarkdownDescription: "SQL user owning a local database. Set with `OWNER` at creation and changed in place with `ALTER DATABASE ... OWNER TO`, which requires a superuser. Omit it to keep and report the current owner. Not supported for shared databases, where it is null.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"connection_limit": schema.Int64Attribute{
				Optional: true, Computed: true,
				MarkdownDescription: "Maximum number of concurrent connections to a local database; `-1` means `UNLIMITED`, the Redshift default. Superusers are exempt. Updated in place with `ALTER DATABASE ... CONNECTION LIMIT`. Omit it to keep and report the current limit. Not supported for shared databases, where it is null.",
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
				Validators:          []validator.Int64{int64validator.AtLeast(databaseUnlimited)},
			},
			"collation": schema.StringAttribute{
				Optional: true, Computed: true,
				MarkdownDescription: "`CASE_SENSITIVE` (the Redshift default) or `CASE_INSENSITIVE` string comparison for a local database, set with `COLLATE` at creation. It is read with `DB_COLLATION()`, which needs a session inside the database, only at creation and import, so a `connection_limit` does not block later refreshes; when that session is refused, the value is kept with a warning. Omit it to keep and report the current collation. Not supported for shared databases, where it is null. Changing it replaces the database.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown(), stringplanmodifier.RequiresReplace()},
				Validators:          []validator.String{stringvalidator.OneOf("CASE_SENSITIVE", "CASE_INSENSITIVE")},
			},
			"isolation_level": schema.StringAttribute{
				Optional: true, Computed: true,
				MarkdownDescription: "`SERIALIZABLE` or `SNAPSHOT` (the Redshift default) isolation for a local database. Updated in place with `ALTER DATABASE ... ISOLATION LEVEL`, which fails while other sessions are connected to the database. Omit it to keep and report the current level. Not supported for shared databases, where it is null.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				Validators:          []validator.String{stringvalidator.OneOf("SERIALIZABLE", "SNAPSHOT")},
			},
		},
		Blocks: map[string]schema.Block{
			timeoutsBlockName: operationTimeoutsBlock(ctx, " For a shared database it includes the wait for the associated "+
				"datashare to appear in the SQL catalog, which without a create timeout ends after five minutes."),
		},
	}
}

// databaseLocalOptions lists the configured options that only a local database accepts; CREATE DATABASE ...
// FROM DATASHARE has no OWNER, CONNECTION LIMIT, COLLATE or ISOLATION LEVEL clause.
func databaseLocalOptions(data databaseModel) []string {
	var configured []string
	for _, option := range []struct {
		name  string
		value attr.Value
	}{{"owner", data.Owner}, {"connection_limit", data.ConnectionLimit}, {"collation", data.Collation}, {"isolation_level", data.IsolationLevel}} {
		if !option.value.IsNull() && !option.value.IsUnknown() {
			configured = append(configured, option.name)
		}
	}
	return configured
}

// validateDatabase rejects local-only options on a shared database before Create writes state, and at plan time.
func validateDatabase(data databaseModel) error {
	if data.DatashareARN.IsNull() || data.DatashareARN.IsUnknown() {
		return nil
	}
	if conflicts := databaseLocalOptions(data); len(conflicts) > 0 {
		return fmt.Errorf("shared databases do not accept %s; remove it or datashare_arn", strings.Join(conflicts, ", "))
	}
	return nil
}

// ValidateConfig surfaces local-only options on shared databases at plan time; unknown values are checked at apply.
func (r *databaseResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data databaseResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !req.Config.Raw.IsFullyKnown() {
		return
	}
	if err := validateDatabase(data.databaseModel); err != nil {
		resp.Diagnostics.AddError("Invalid database options", err.Error())
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
	rows, err := r.query(ctx, readDatabaseStatement(name), nil)
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
	if len(rows) != 1 || (!strings.EqualFold(rows[0]["database_type"], databaseTypeLocal) && !strings.EqualFold(rows[0]["database_type"], databaseTypeShared)) {
		return databaseModel{}, false, fmt.Errorf("expected one local or shared database named %q", name)
	}
	row := rows[0]
	databaseType := strings.ToUpper(row["database_type"])
	data := databaseModel{
		Name: types.StringValue(row["database_name"]), DatabaseType: types.StringValue(databaseType),
		WithPermissions: types.BoolValue(false),
		Owner:           types.StringNull(), ConnectionLimit: types.Int64Null(), Collation: types.StringNull(), IsolationLevel: types.StringNull(),
	}
	if databaseType == databaseTypeLocal {
		return data, true, r.databaseLocalMetadata(ctx, &data, row["database_isolation_level"])
	}
	if databaseType == databaseTypeShared {
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

// databaseLocalMetadata fills the options only local databases have from the administration database: the
// isolation level from SHOW DATABASES and the owner and limit from PG_DATABASE_INFO. The collation is left to
// databaseReadCollation, because it needs a session inside the database itself.
func (r *resourceClient) databaseLocalMetadata(ctx context.Context, data *databaseModel, isolation string) error {
	name := data.Name.ValueString()
	level, err := databaseIsolationLevel(isolation)
	if err != nil {
		return fmt.Errorf("database %q: %w", name, err)
	}
	rows, err := r.selectRows(ctx, r.database.ValueString(), readDatabaseOptionsQuery(name))
	if err != nil {
		return err
	}
	if len(rows) != 1 || strings.TrimSpace(rows[0]["owner"]) == "" {
		return fmt.Errorf("database %q has no unique owner in pg_database_info", name)
	}
	limit, err := databaseConnectionLimitValue(rows[0]["connection_limit"])
	if err != nil {
		return fmt.Errorf("database %q: %w", name, err)
	}
	data.Owner, data.ConnectionLimit = types.StringValue(strings.TrimSpace(rows[0]["owner"])), types.Int64Value(limit)
	data.IsolationLevel = types.StringValue(level)
	return nil
}

// databaseCollation runs DB_COLLATION() in a session inside the database, because no catalog view reports the
// collation of another database.
func (r *resourceClient) databaseCollation(ctx context.Context, name string) (string, error) {
	rows, err := r.selectRows(ctx, name, readDatabaseCollationQuery())
	if err != nil {
		return "", err
	}
	if len(rows) != 1 {
		return "", fmt.Errorf("database %q returned no collation", name)
	}
	collation := strings.ToUpper(strings.TrimSpace(rows[0]["collation"]))
	if _, err := sqlclient.OneOf(collation, databaseCollations...); err != nil {
		return "", fmt.Errorf("database %q has unsupported collation %q", name, rows[0]["collation"])
	}
	return collation, nil
}

// databaseReadCollation stores the collation of a local database. A session in the database is not always
// available: CONNECTION LIMIT is enforced for non-superusers, and a limit of 0 or one used up by other sessions
// refuses the connection. The collation is supplementary, so a failed read is a warning that keeps the current
// value rather than an error that would block every refresh of a database that exists.
func (r *resourceClient) databaseReadCollation(ctx context.Context, data *databaseModel, diagnostics *diag.Diagnostics) {
	if data.DatabaseType.ValueString() != databaseTypeLocal {
		return
	}
	collation, err := r.databaseCollation(ctx, data.Name.ValueString())
	if err != nil {
		if data.Collation.IsUnknown() {
			data.Collation = types.StringNull()
		}
		diagnostics.AddWarning("Read database collation", fmt.Sprintf(
			"DB_COLLATION() could not run in a session inside database %q, so the collation stays %s: %v. "+
				"Non-superusers are subject to the database's CONNECTION LIMIT.", data.Name.ValueString(), data.Collation, err))
		return
	}
	data.Collation = types.StringValue(collation)
}

// Database types as the provider reports them; SVV_REDSHIFT_DATABASES spells them in lowercase.
const (
	databaseTypeLocal  = "LOCAL"
	databaseTypeShared = "SHARED"
)

// databaseIsolationLevel maps the catalog's "Snapshot Isolation" and "Serializable" to the configuration keywords.
func databaseIsolationLevel(catalog string) (string, error) {
	switch value := strings.ToLower(catalog); {
	case strings.Contains(value, "snapshot"):
		return "SNAPSHOT", nil
	case strings.Contains(value, "serializable"):
		return "SERIALIZABLE", nil
	default:
		return "", fmt.Errorf("unrecognized isolation level %q", catalog)
	}
}

// databaseConnectionLimitValue parses PG_DATABASE_INFO.datconnlimit, a text column that holds UNLIMITED or -1
// for no limit and a number otherwise.
func databaseConnectionLimitValue(catalog string) (int64, error) {
	value := strings.TrimSpace(catalog)
	if strings.EqualFold(value, "UNLIMITED") {
		return databaseUnlimited, nil
	}
	limit, err := strconv.ParseInt(value, 10, 64)
	if err != nil || limit < databaseUnlimited {
		return 0, fmt.Errorf("unrecognized connection limit %q", catalog)
	}
	return limit, nil
}

// databaseConverged reports the first configured option the catalog does not hold after Create or Update.
func databaseConverged(expected, observed databaseModel) error {
	for _, option := range []struct {
		name               string
		expected, observed attr.Value
	}{
		{"with_permissions", expected.WithPermissions, observed.WithPermissions},
		{"owner", expected.Owner, observed.Owner},
		{"connection_limit", expected.ConnectionLimit, observed.ConnectionLimit},
		{"collation", expected.Collation, observed.Collation},
		{"isolation_level", expected.IsolationLevel, observed.IsolationLevel},
	} {
		if !option.expected.IsNull() && !option.expected.IsUnknown() && !option.expected.Equal(option.observed) {
			return fmt.Errorf("%s is %s in the catalog, not the planned %s", option.name, option.observed, option.expected)
		}
	}
	return nil
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
		if observed.DatabaseType.ValueString() != databaseTypeLocal {
			return false, fmt.Errorf("database %q is not a local database; migrate it explicitly", data.Name.ValueString())
		}
		// Collation is create-only and needs a session inside the database, so it is read only at creation,
		// import, or while it is still null, and otherwise kept.
		observed.Collation = data.Collation
		if observed.Collation.IsUnknown() {
			observed.Collation = types.StringNull()
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
	if observed.DatabaseType.ValueString() != databaseTypeShared || observed.ShareName.ValueString() != source.Name ||
		observed.ProducerAccount.ValueString() != source.Account || observed.ProducerNamespace.ValueString() != source.Namespace {
		return false, fmt.Errorf("database %q has an incompatible type or producer binding; migrate it explicitly", data.Name.ValueString())
	}
	*data = observed
	return true, nil
}

// databaseClearUnknown nulls the options the plan left to the catalog, so state written before verification
// finishes is fully known even when the readback fails.
func databaseClearUnknown(data *databaseModel) {
	for _, value := range []*types.String{&data.Owner, &data.Collation, &data.IsolationLevel} {
		if value.IsUnknown() {
			*value = types.StringNull()
		}
	}
	if data.ConnectionLimit.IsUnknown() {
		data.ConnectionLimit = types.Int64Null()
	}
}

// verifyCreatedDatabase re-reads a created local database, stores what the catalog holds, and reports an absent
// database or an option that did not converge.
func (r *databaseResource) verifyCreatedDatabase(ctx context.Context, data databaseResourceModel, resp *resource.CreateResponse) {
	expected := data.databaseModel
	found, err := r.read(ctx, &data.databaseModel)
	if err == nil && found {
		r.databaseReadCollation(ctx, &data.databaseModel, &resp.Diagnostics)
		resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
		err = databaseConverged(expected, data.databaseModel)
	} else if err == nil {
		err = errors.New("the local database is absent after creation")
	}
	if err != nil {
		resp.Diagnostics.AddError("Verify database", err.Error())
	}
}

// Create creates a local database or waits for its incoming datashare before binding it.
func (r *databaseResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data databaseResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	ctx, done := boundOperation(ctx, "create", data.Timeouts.Create, &resp.Diagnostics)
	defer done()
	if resp.Diagnostics.HasError() {
		return
	}
	if err := validateDatabase(data.databaseModel); err != nil {
		resp.Diagnostics.AddError("Invalid database options", err.Error())
		return
	}
	target, err := r.connection(r.database.ValueString())
	if err != nil || target.Database == data.Name.ValueString() {
		resp.Diagnostics.AddError("Create database", "Configure a local administration database different from the database being created.")
		return
	}
	if data.DatashareARN.IsNull() {
		statement, err := createDatabaseStatement(data.databaseModel)
		if err == nil {
			err = r.exec(ctx, r.database.ValueString(), statement)
		}
		if err != nil {
			resp.Diagnostics.AddError("Create database", err.Error())
			return
		}
		data.ID = r.identity(r.database.ValueString(), map[string]string{"name": data.Name.ValueString()})
		data.DatabaseType = types.StringValue(databaseTypeLocal)
		data.ShareName, data.ProducerAccount, data.ProducerNamespace = types.StringNull(), types.StringNull(), types.StringNull()
		databaseClearUnknown(&data.databaseModel)
		resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
		r.verifyCreatedDatabase(ctx, data, resp)
		return
	}
	source, err := parseShare(data.DatashareARN.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Create shared database", err.Error())
		return
	}
	// AWS association can complete before the share appears in the SQL catalog. A create timeout bounds the wait with
	// the rest of the operation; without one, only the wait is bounded, by databaseShareWait.
	wait, _ := data.Timeouts.Create(ctx, databaseShareWait)
	waitCtx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	for {
		rows, err := r.selectRows(waitCtx, r.database.ValueString(), readDatabaseInboundShareQuery(source))
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
			resp.Diagnostics.AddError("Discover associated datashare", fmt.Sprintf("The datashare did not become visible in the SQL catalog within %s.", wait))
			return
		case <-time.After(time.Second):
		}
	}
	statement, err := createDatabaseStatement(data.databaseModel)
	if err == nil {
		err = r.exec(ctx, r.database.ValueString(), statement)
	}
	if err != nil {
		resp.Diagnostics.AddError("Create shared database", err.Error())
		return
	}
	data.ID = r.identity(r.database.ValueString(), map[string]string{"name": data.Name.ValueString(), "datashare_arn": data.DatashareARN.ValueString()})
	data.DatabaseType = types.StringValue(databaseTypeShared)
	data.ShareName = types.StringValue(source.Name)
	data.ProducerAccount = types.StringValue(source.Account)
	data.ProducerNamespace = types.StringValue(source.Namespace)
	databaseClearUnknown(&data.databaseModel)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	expectedPermissions := data.WithPermissions
	found, err := r.read(ctx, &data.databaseModel)
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
	var data databaseResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, &data.databaseModel)
	if err != nil {
		resp.Diagnostics.AddError("Read database", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	if data.Collation.IsNull() {
		r.databaseReadCollation(ctx, &data.databaseModel, &resp.Diagnostics)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// errDatabaseVanished reports a database dropped outside Terraform while an update was planned against it.
var errDatabaseVanished = errors.New("the database disappeared during the update; refresh the plan")

// Update applies the in-place options of a local database and verifies them, and verifies the immutable binding
// of a shared database. The ALTER statements start from the catalog rather than the prior state, so an option
// changed outside Terraform since the last refresh is still moved to the plan.
func (r *databaseResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data, prior databaseResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	ctx, done := boundOperation(ctx, "update", data.Timeouts.Update, &resp.Diagnostics)
	defer done()
	if resp.Diagnostics.HasError() {
		return
	}
	if err := validateDatabase(data.databaseModel); err != nil {
		resp.Diagnostics.AddError("Invalid database options", err.Error())
		return
	}
	expected := data.databaseModel
	found, err := r.read(ctx, &prior.databaseModel)
	if err == nil && !found {
		err = errDatabaseVanished
	}
	var statements []string
	if err == nil {
		statements, err = alterDatabaseStatements(prior.databaseModel, data.databaseModel)
	}
	if err == nil {
		err = r.exec(ctx, r.database.ValueString(), statements...)
	}
	if err == nil {
		// Collation never changes in place; keeping the prior value avoids a session inside the database.
		if data.Collation.IsUnknown() {
			data.Collation = prior.Collation
		}
		if found, err = r.read(ctx, &data.databaseModel); err == nil && !found {
			err = errDatabaseVanished
		}
	}
	if err == nil {
		err = databaseConverged(expected, data.databaseModel)
	}
	if err != nil {
		resp.Diagnostics.AddError("Update database", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Delete drops the selected database and verifies catalog removal.
func (r *databaseResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state databaseResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	ctx, done := boundOperation(ctx, "delete", state.Timeouts.Delete, &resp.Diagnostics)
	defer done()
	if resp.Diagnostics.HasError() {
		return
	}
	data := state.databaseModel

	found, err := r.read(ctx, &data)
	if err == nil && found {
		err = r.exec(ctx, r.database.ValueString(), dropDatabaseStatement(data))
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
