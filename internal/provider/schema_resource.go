package provider

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// schemaResource manages a local SQL schema, its owner, and its disk quota.
type schemaResource struct {
	// resourceClient provides SQL execution and warehouse ownership checks.
	resourceClient
}

// schemaModel is the Terraform state for a local schema.
type schemaModel struct {
	// ID records the database/schema identity within the configured warehouse.
	ID types.String `tfsdk:"id"`
	// Database contains the schema.
	Database types.String `tfsdk:"database"`
	// Name identifies the SQL schema.
	Name types.String `tfsdk:"name"`
	// Owner is the SQL user owning the schema; unset configuration keeps the current owner.
	Owner types.String `tfsdk:"owner"`
	// Quota is the disk quota in megabytes; -1 means UNLIMITED.
	Quota types.Int64 `tfsdk:"quota"`
}

var _ = registerResource(newSchemaResource)

// newSchemaResource constructs a local schema lifecycle handler.
func newSchemaResource() resource.Resource { return &schemaResource{} }

// Metadata identifies the schema resource to Terraform.
func (r *schemaResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_schema"
}

// Schema defines local schema identity, owner, and quota.
func (r *schemaResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages one local schema in a Redshift database, with its owner and disk quota; external schemas are managed by `redshift_external_schema`.",
		Attributes: map[string]schema.Attribute{
			"id": idAttribute(),
			"database": schema.StringAttribute{
				Required: true, MarkdownDescription: "Local database owning the schema.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				Required: true, MarkdownDescription: "Schema name. The provider never renames a schema.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"owner": schema.StringAttribute{
				Optional: true, Computed: true,
				MarkdownDescription: "SQL user owning the schema. Set with `AUTHORIZATION` at creation and changed in place with `ALTER SCHEMA ... OWNER TO`. Omit it to keep and report the current owner, which defaults to the creating user.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"quota": schema.Int64Attribute{
				Optional: true, Computed: true,
				MarkdownDescription: "Disk quota in megabytes, the unit Redshift stores, from 2048 (2 GB) to 524288000 (500 TB); `-1` means `UNLIMITED`, the default. Set with `QUOTA` at creation and changed in place with `ALTER SCHEMA ... QUOTA`; changing a quota requires a superuser. Omit it to keep and report the current quota, read from `SVV_REDSHIFT_SCHEMA_QUOTA`. That view shows a regular user only their own schemas, so reading and verifying the quota of a schema owned by another user requires a superuser connection; otherwise the last known value is kept.",
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
				Validators:          []validator.Int64{int64validator.Any(int64validator.OneOf(schemaUnlimited), int64validator.Between(schemaQuotaMin, schemaQuotaMax))},
			},
		},
	}
	foldNames(&resp.Schema, "database", "name")
}

// catalogRow finds the schema's catalog row, returning a nil row when it or its database is gone. Delete needs only
// this, not the quota lookup read adds.
func (r *schemaResource) catalogRow(ctx context.Context, data schemaModel) (sqlclient.Row, error) {
	if err := r.bound(data.ID, data.Database.ValueString()); err != nil {
		return nil, err
	}

	if exists, err := r.localDatabaseExists(ctx, data.Database.ValueString()); err != nil || !exists {
		return nil, err
	}

	rows, err := r.selectRows(ctx, data.Database.ValueString(), readSchemaQuery(data))
	if err != nil || len(rows) == 0 {
		return nil, err
	}

	if len(rows) != 1 {
		return nil, fmt.Errorf("schema %q has ambiguous catalog metadata", data.Name.ValueString())
	}

	return rows[0], nil
}

// read refreshes the schema, its owner, and its quota under the configured database binding.
func (r *schemaResource) read(ctx context.Context, data *schemaModel) (bool, error) {
	row, err := r.catalogRow(ctx, *data)
	if err != nil || row == nil {
		return false, err
	}

	if row["owner"] == "" {
		return false, fmt.Errorf("schema %q has incomplete catalog metadata", data.Name.ValueString())
	}

	quota, visible, err := r.schemaQuota(ctx, *data, row["owner"])
	if err != nil {
		return false, fmt.Errorf("schema %q: %w", data.Name.ValueString(), err)
	}

	data.Name, data.Owner = types.StringValue(row["schema_name"]), types.StringValue(row["owner"])
	switch {
	case visible:
		data.Quota = types.Int64Value(quota)
	case data.Quota.IsUnknown():
		data.Quota = types.Int64Null()
	}

	return true, nil
}

// schemaQuota reads the schema's quota and reports whether the connection can see it. SVV_REDSHIFT_SCHEMA_QUOTA
// shows a regular user only their own schemas, so a missing row means UNLIMITED only for a superuser or the
// owner; for anyone else the quota is unreadable and the caller keeps the value it already has.
func (r *schemaResource) schemaQuota(ctx context.Context, data schemaModel, owner string) (int64, bool, error) {
	quotas, err := r.selectRows(ctx, data.Database.ValueString(), readSchemaQuotaQuery(data))
	if err != nil {
		return 0, false, err
	}

	if len(quotas) > 0 {
		quota, err := schemaQuotaValue(quotas)
		return quota, err == nil, err
	}

	sessions, err := r.selectRows(ctx, data.Database.ValueString(), readSchemaSessionQuery())
	if err != nil {
		return 0, false, err
	}

	if len(sessions) != 1 {
		return 0, false, errors.New("pg_user has no unique row for the connected user")
	}

	superuser, err := strconv.ParseBool(sessions[0]["superuser"])
	if err != nil {
		return 0, false, fmt.Errorf("unrecognized usesuper %q", sessions[0]["superuser"])
	}

	return schemaUnlimited, superuser || sessions[0]["name"] == owner, nil
}

// schemaQuotaValue decodes visible SVV_REDSHIFT_SCHEMA_QUOTA rows, where an empty or non-positive quota means
// UNLIMITED.
func schemaQuotaValue(rows []sqlclient.Row) (int64, error) {
	if len(rows) > 1 {
		return 0, errors.New("svv_redshift_schema_quota returned several rows")
	}

	if len(rows) == 0 || strings.TrimSpace(rows[0]["quota"]) == "" {
		return schemaUnlimited, nil
	}

	quota, err := strconv.ParseInt(strings.TrimSpace(rows[0]["quota"]), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("unrecognized quota %q", rows[0]["quota"])
	}

	if quota < 1 {
		return schemaUnlimited, nil
	}

	return quota, nil
}

// schemaConverged reports the first configured option the catalog does not hold after Create or Update.
func schemaConverged(expected, observed schemaModel) error {
	for _, option := range []struct {
		name               string
		expected, observed attr.Value
	}{{"owner", expected.Owner, observed.Owner}, {"quota", expected.Quota, observed.Quota}} {
		if !option.expected.IsNull() && !option.expected.IsUnknown() && !option.expected.Equal(option.observed) {
			return fmt.Errorf("%s is %s in the catalog, not the planned %s", option.name, option.observed, option.expected)
		}
	}

	return nil
}

// Create creates a local schema with its optional owner and quota and verifies them.
func (r *schemaResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data schemaModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	statement, err := createSchemaStatement(data)
	if err == nil {
		err = r.exec(ctx, data.Database.ValueString(), statement)
	}

	if err != nil {
		resp.Diagnostics.AddError("Create schema", err.Error())
		return
	}

	data.ID = identity(map[string]string{"database": data.Database.ValueString(), "name": data.Name.ValueString()})
	// A failed verification must still return serializable state for the created schema.
	if data.Owner.IsUnknown() {
		data.Owner = types.StringNull()
	}

	if data.Quota.IsUnknown() {
		data.Quota = types.Int64Null()
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	expected := data

	found, err := r.read(ctx, &data)
	if err == nil && !found {
		err = errors.New("the schema is absent after creation")
	}

	if err == nil {
		resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
		err = schemaConverged(expected, data)
	}

	if err != nil {
		resp.Diagnostics.AddError("Verify schema", err.Error())
	}
}

// Read refreshes schema attributes or removes a missing schema from state.
func (r *schemaResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data schemaModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	found, err := r.read(ctx, &data)
	if err != nil {
		resp.Diagnostics.AddError("Read schema", err.Error())
		return
	}

	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	data.ID = normalizeLegacyIdentity(data.ID, true)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// errSchemaVanished reports a schema dropped outside Terraform while an update was planned against it.
var errSchemaVanished = errors.New("the schema disappeared during the update; refresh the plan")

// Update changes the owner and quota in place, starting from the catalog so changes made outside Terraform since
// the last refresh are still corrected, and verifies the result.
func (r *schemaResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data, prior schemaModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)

	if resp.Diagnostics.HasError() {
		return
	}

	expected := data

	found, err := r.read(ctx, &prior)
	if err == nil && !found {
		err = errSchemaVanished
	}

	var statements []string
	if err == nil {
		statements, err = alterSchemaStatements(prior, data)
	}

	if err == nil {
		err = r.exec(ctx, data.Database.ValueString(), statements...)
	}

	if err == nil {
		if found, err = r.read(ctx, &data); err == nil && !found {
			err = errSchemaVanished
		}
	}

	if err == nil {
		err = schemaConverged(expected, data)
	}

	if err != nil {
		resp.Diagnostics.AddError("Update schema", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Delete drops the schema without CASCADE; quota visibility does not affect deletion.
func (r *schemaResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data schemaModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	row, err := r.catalogRow(ctx, data)
	if err == nil && row != nil {
		err = r.exec(ctx, data.Database.ValueString(), dropSchemaStatement(data))
	}

	if err != nil {
		resp.Diagnostics.AddError("Delete schema", err.Error())
	}
}

// ImportState restores schema database/name ownership from JSON.
func (r *schemaResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importIdentity(ctx, req, resp, "database", "name")
}
