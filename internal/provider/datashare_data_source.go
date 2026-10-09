package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// datashareDataSource exposes outbound share metadata without owning the share.
type datashareDataSource struct {
	// dataSourceClient provides read-only catalog execution and binding checks.
	dataSourceClient
}

// datashareData contains producer share lookup keys and observed settings.
type datashareData struct {
	// ID is the paired resource's JSON identity.
	ID types.String `tfsdk:"id"`
	// Name identifies the outbound SQL share.
	Name types.String `tfsdk:"name"`
	// Database is the producer database owning the share.
	Database types.String `tfsdk:"database"`
	// PublicAccessible reports whether public consumer workgroups are allowed.
	PublicAccessible types.Bool `tfsdk:"publicly_accessible"`
}

var _ = registerDataSource(newDatashareDataSource)

// newDatashareDataSource constructs a read-only producer share lookup.
func newDatashareDataSource() datasource.DataSource { return &datashareDataSource{} }

// Metadata identifies the datashare data source to Terraform.
func (d *datashareDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_datashare"
}

// Schema defines the producer share lookup and computed public-access setting.
func (d *datashareDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a producer datashare in a local database.",
		Attributes: map[string]schema.Attribute{
			"id":                  dataSourceIDAttribute(),
			"name":                schema.StringAttribute{Required: true, MarkdownDescription: "Datashare name."},
			"database":            schema.StringAttribute{Required: true, MarkdownDescription: "Producer database owning the share."},
			"publicly_accessible": schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether public workgroups may consume it."},
		},
	}
}

// Read retrieves outbound share metadata without taking ownership.
func (d *datashareDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data datashareData
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	share := datashareModel{Name: data.Name, Database: data.Database}
	found, err := (&datashareResource{d.resourceClient}).read(ctx, &share)
	if err != nil {
		resp.Diagnostics.AddError("Read datashare", err.Error())
		return
	}
	if !found {
		resp.Diagnostics.AddError("Datashare not found", "No outbound datashare named "+data.Name.ValueString()+" exists in the configured database.")
		return
	}
	data.Name, data.PublicAccessible = share.Name, share.PublicAccessible
	data.ID = d.identity(data.Database.ValueString(), map[string]string{"name": data.Name.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
