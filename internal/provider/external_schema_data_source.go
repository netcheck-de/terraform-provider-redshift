package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// externalSchemaDataSource exposes an existing Glue-backed SQL schema mapping.
type externalSchemaDataSource struct {
	// dataSourceClient provides read-only catalog execution and binding checks.
	dataSourceClient
}

// externalSchemaData contains mapping lookup keys and computed Glue/IAM settings.
type externalSchemaData struct {
	// ID is the paired resource's JSON identity.
	ID types.String `tfsdk:"id"`
	// Database contains the external schema mapping.
	Database types.String `tfsdk:"database"`
	// Name identifies the external SQL schema.
	Name types.String `tfsdk:"name"`
	// GlueDatabase reports the mapped catalog database.
	GlueDatabase types.String `tfsdk:"glue_database"`
	// IAMRoleARN reports the attached Spectrum role used by the mapping.
	IAMRoleARN types.String `tfsdk:"iam_role_arn"`
	// Region reports the Glue catalog region recorded in the mapping.
	Region types.String `tfsdk:"region"`
}

var _ = registerDataSource(newExternalSchemaDataSource)

// newExternalSchemaDataSource constructs a read-only Glue schema lookup.
func newExternalSchemaDataSource() datasource.DataSource { return &externalSchemaDataSource{} }

// Metadata identifies the external schema data source to Terraform.
func (d *externalSchemaDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_external_schema"
}

// Schema defines the external schema lookup and observed Glue/IAM settings.
func (d *externalSchemaDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a Glue Data Catalog external schema.",
		Attributes: map[string]schema.Attribute{
			"id":            dataSourceIDAttribute(),
			"database":      schema.StringAttribute{Required: true, MarkdownDescription: "Local Redshift database."},
			"name":          schema.StringAttribute{Required: true, MarkdownDescription: "External schema name."},
			"glue_database": schema.StringAttribute{Computed: true, MarkdownDescription: "Glue database name."},
			"iam_role_arn":  schema.StringAttribute{Computed: true, MarkdownDescription: "Attached catalog access role ARN."},
			"region":        schema.StringAttribute{Computed: true, MarkdownDescription: "Glue catalog AWS region, if recorded in the catalog."},
		},
	}
}

// Read resolves an existing Glue-backed schema without taking ownership.
func (d *externalSchemaDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data externalSchemaData
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	current := externalSchemaModel{Database: data.Database, Name: data.Name}
	found, err := (&externalSchemaResource{d.resourceClient}).read(ctx, &current)
	if err != nil {
		resp.Diagnostics.AddError("Read external schema", err.Error())
		return
	}
	if !found {
		resp.Diagnostics.AddError("External schema not found", "No Glue external schema named "+data.Name.ValueString()+" exists in the configured database.")
		return
	}
	data.Name, data.GlueDatabase, data.IAMRoleARN = current.Name, current.GlueDatabase, current.IAMRoleARN
	data.Region = current.Region
	data.ID = d.identity(data.Database.ValueString(), map[string]string{"name": data.Name.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
