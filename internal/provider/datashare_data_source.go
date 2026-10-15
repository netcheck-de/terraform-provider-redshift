package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
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
	// Owner is the owning database user.
	Owner types.String `tfsdk:"owner"`
	// ShareID is the catalog's numeric datashare ID.
	ShareID types.Int64 `tfsdk:"share_id"`
	// ProducerAccount is the AWS account of the producer namespace.
	ProducerAccount types.String `tfsdk:"producer_account"`
	// ProducerNamespace is the producer namespace UUID.
	ProducerNamespace types.String `tfsdk:"producer_namespace"`
	// CreatedAt is the catalog creation timestamp as text.
	CreatedAt types.String `tfsdk:"created_at"`
}

var _ = registerDataSource(newDatashareDataSource)

// newDatashareDataSource constructs a read-only producer share lookup.
func newDatashareDataSource() datasource.DataSource { return &datashareDataSource{} }

// Metadata identifies the datashare data source to Terraform.
func (d *datashareDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_datashare"
}

// Schema mirrors the resource, so observed catalog attributes cannot drift from the managed ones.
func (d *datashareDataSource) Schema(ctx context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	var source resource.SchemaResponse
	newDatashareResource().Schema(ctx, resource.SchemaRequest{}, &source)
	attributes := lookupSchemaAttributes(source.Schema.Attributes, source.Schema.Blocks, false, nil)
	attributes["id"] = dataSourceIDAttribute()
	attributes["publicly_accessible"] = datasharePublicObservation
	resp.Schema = schema.Schema{MarkdownDescription: "Looks up a producer datashare in a local database.", Attributes: attributes}
}

// datasharePublicObservation replaces the resource's default and update wording, which does not apply to an
// observed value.
var datasharePublicObservation = schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether public workgroups may consume the share."}

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
	data.Name, data.PublicAccessible, data.Owner, data.ShareID = share.Name, share.PublicAccessible, share.Owner, share.ShareID
	data.ProducerAccount, data.ProducerNamespace, data.CreatedAt = share.ProducerAccount, share.ProducerNamespace, share.CreatedAt
	data.ID = d.identity(data.Database.ValueString(), map[string]string{"name": data.Name.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
