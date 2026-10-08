package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// externalSchemaResource manages a Redshift schema mapping to an existing Glue database.
type externalSchemaResource struct {
	// resourceClient provides SQL execution and warehouse ownership checks.
	resourceClient
}

// externalSchemaModel is the Terraform state for a Glue-backed schema mapping.
type externalSchemaModel struct {
	// ID records the warehouse/database/schema identity.
	ID types.String `tfsdk:"id"`
	// Database contains the Redshift schema mapping.
	Database types.String `tfsdk:"database"`
	// Name identifies the external SQL schema.
	Name types.String `tfsdk:"name"`
	// GlueDatabase selects the existing AWS Glue catalog database.
	GlueDatabase types.String `tfsdk:"glue_database"`
	// IAMRoleARN selects the attached Spectrum role used to access catalog data.
	IAMRoleARN types.String `tfsdk:"iam_role_arn"`
	// Region selects the Glue catalog region; omission uses the warehouse region.
	Region types.String `tfsdk:"region"`
	// RefreshRevision triggers explicit replacement to rebuild the schema mapping.
	RefreshRevision types.String `tfsdk:"refresh_revision"`
}

// newExternalSchemaResource constructs a Glue-backed external schema handler.
func newExternalSchemaResource() resource.Resource { return &externalSchemaResource{} }

// Metadata identifies the external schema resource to Terraform.
func (r *externalSchemaResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_external_schema"
}

// Schema defines Glue catalog binding, IAM role, and explicit refresh revision.
func (r *externalSchemaResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages one Glue Data Catalog external schema in a local Redshift database.",
		Attributes: map[string]schema.Attribute{
			"id":               idAttribute(),
			"database":         schema.StringAttribute{Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}, MarkdownDescription: "Local Redshift database owning the external schema."},
			"name":             schema.StringAttribute{Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}, MarkdownDescription: "Local external schema name."},
			"glue_database":    schema.StringAttribute{Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}, MarkdownDescription: "AWS Glue database referenced by the external schema."},
			"iam_role_arn":     schema.StringAttribute{Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}, MarkdownDescription: "IAM role attached to the Redshift namespace for Glue catalog access."},
			"region":           schema.StringAttribute{Optional: true, Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}, MarkdownDescription: "Glue catalog AWS region; defaults to the warehouse region and is read from the catalog."},
			"refresh_revision": schema.StringAttribute{Optional: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}, MarkdownDescription: "Bump to recreate the external schema after a Glue catalog change; deletion is restrictive."},
		},
	}
}

// read verifies Glue schema kind and refreshes catalog/IAM settings.
func (r *externalSchemaResource) read(ctx context.Context, data *externalSchemaModel) (bool, error) {
	if err := r.bound(data.ID, data.Database.ValueString()); err != nil {
		return false, err
	}
	rows, err := r.queryDatabase(ctx, data.Database.ValueString(),
		"SELECT schemaname, eskind, databasename, esoptions FROM svv_external_schemas WHERE schemaname = :name",
		map[string]string{"name": data.Name.ValueString()})
	if err != nil || len(rows) == 0 {
		return false, err
	}
	if len(rows) != 1 || rows[0]["eskind"] != "1" {
		return false, fmt.Errorf("external schema %q is not a unique Glue Data Catalog schema", data.Name.ValueString())
	}
	var options map[string]string
	if err := json.Unmarshal([]byte(rows[0]["esoptions"]), &options); err != nil {
		return false, fmt.Errorf("decode external schema options: %w", err)
	}
	role := options["IAM_ROLE"]
	if role == "" {
		role = options["iam_role"]
	}
	if role == "" {
		return false, fmt.Errorf("external schema has no IAM_ROLE catalog option")
	}
	if (!data.GlueDatabase.IsNull() && !data.GlueDatabase.IsUnknown() && data.GlueDatabase.ValueString() != rows[0]["databasename"]) ||
		(!data.IAMRoleARN.IsNull() && !data.IAMRoleARN.IsUnknown() && data.IAMRoleARN.ValueString() != role) {
		return false, fmt.Errorf("external schema %q points to another Glue database or IAM role; migrate explicitly", data.Name.ValueString())
	}
	data.Name = types.StringValue(strings.TrimSpace(rows[0]["schemaname"]))
	data.GlueDatabase = types.StringValue(rows[0]["databasename"])
	data.IAMRoleARN = types.StringValue(role)
	region := options["REGION"]
	if region == "" {
		region = options["region"]
	}
	if region != "" {
		data.Region = types.StringValue(region)
	} else {
		data.Region = types.StringNull()
	}
	return true, nil
}

// Create maps the Glue database into Redshift and verifies its external schema options.
func (r *externalSchemaResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data externalSchemaModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	sql := "CREATE EXTERNAL SCHEMA " + sqlclient.Identifier(data.Name.ValueString()) + " FROM DATA CATALOG DATABASE " + sqlclient.Literal(data.GlueDatabase.ValueString()) + " IAM_ROLE " + sqlclient.Literal(data.IAMRoleARN.ValueString())
	if data.Region.ValueString() != "" {
		sql += " REGION " + sqlclient.Literal(data.Region.ValueString())
	}
	if _, err := r.queryDatabase(ctx, data.Database.ValueString(), sql, nil); err != nil {
		resp.Diagnostics.AddError("Create external schema", err.Error())
		return
	}
	data.ID = r.identity(data.Database.ValueString(), map[string]string{"name": data.Name.ValueString()})
	// The default region is not known until catalog readback succeeds.
	if data.Region.IsUnknown() {
		data.Region = types.StringNull()
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	found, err := r.read(ctx, &data)
	switch {
	case err != nil:
		resp.Diagnostics.AddError("Verify external schema", err.Error())
	case !found:
		resp.Diagnostics.AddError("Verify external schema", "The external schema is absent after creation.")
	default:
		resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	}
}

// Read refreshes the external schema or removes a missing schema from state.
func (r *externalSchemaResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data externalSchemaModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, &data)
	switch {
	case err != nil:
		resp.Diagnostics.AddError("Read external schema", err.Error())
	case !found:
		resp.State.RemoveResource(ctx)
	default:
		resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	}
}

// Update verifies the immutable external schema binding and refreshes observed options.
func (r *externalSchemaResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data externalSchemaModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, &data)
	switch {
	case err != nil:
		resp.Diagnostics.AddError("Read external schema", err.Error())
	case !found:
		resp.Diagnostics.AddError("Update external schema", "The external schema disappeared; refresh the plan.")
	default:
		resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	}
}

// Delete removes the Redshift schema mapping without deleting Glue objects.
func (r *externalSchemaResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data externalSchemaModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, &data)
	if err == nil && found {
		_, err = r.queryDatabase(ctx, data.Database.ValueString(), "DROP SCHEMA "+sqlclient.Identifier(data.Name.ValueString()), nil)
		if err == nil {
			found, err = r.read(ctx, &data)
			if err == nil && found {
				resp.Diagnostics.AddError("Delete external schema", "The external schema remains after deletion.")
			}
		}
	}
	if err != nil {
		resp.Diagnostics.AddError("Delete external schema", err.Error())
	}
}

// ImportState restores external schema ownership from its database/name identity.
func (r *externalSchemaResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importIdentity(ctx, req, resp, "database", "name")
}
