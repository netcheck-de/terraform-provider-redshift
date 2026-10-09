package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// schemaDataSource exposes local schema metadata without owning the schema.
type schemaDataSource struct {
	// dataSourceClient provides read-only catalog execution and binding checks.
	dataSourceClient
}

// schemaData contains schema lookup keys and the observed SQL owner.
type schemaData struct {
	// ID is the paired resource's JSON identity.
	ID types.String `tfsdk:"id"`
	// Database contains the requested schema.
	Database types.String `tfsdk:"database"`
	// Name identifies the requested schema.
	Name types.String `tfsdk:"name"`
	// Owner reports the catalog's SQL owner name.
	Owner types.String `tfsdk:"owner"`
}

var _ = registerDataSource(newSchemaDataSource)

// newSchemaDataSource constructs a read-only local schema lookup.
func newSchemaDataSource() datasource.DataSource { return &schemaDataSource{} }

// Metadata identifies the schema data source to Terraform.
func (d *schemaDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_schema"
}

// Schema defines schema lookup keys and its observed owner.
func (d *schemaDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a local schema in a Redshift database.",
		Attributes: map[string]schema.Attribute{
			"id":       dataSourceIDAttribute(),
			"database": schema.StringAttribute{Required: true, MarkdownDescription: "Database owning the schema."},
			"name":     schema.StringAttribute{Required: true, MarkdownDescription: "Schema name."},
			"owner":    schema.StringAttribute{Computed: true, MarkdownDescription: "Database user owning the schema."},
		},
	}
}

// Read refreshes the requested schema and owner without managing lifecycle.
func (d *schemaDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data schemaData
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	current := schemaModel{Database: data.Database, Name: data.Name}
	found, err := (&schemaResource{d.resourceClient}).read(ctx, &current)
	if err != nil {
		resp.Diagnostics.AddError("Read schema", err.Error())
		return
	}
	if !found {
		resp.Diagnostics.AddError("Schema not found", "No local schema named "+data.Name.ValueString()+" exists in the configured database.")
		return
	}
	data.Name, data.Owner = current.Name, current.Owner
	data.ID = d.identity(data.Database.ValueString(), map[string]string{"name": data.Name.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
