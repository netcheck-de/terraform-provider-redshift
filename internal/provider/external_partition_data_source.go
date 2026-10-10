package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// externalPartitionDataSource reads one partition of an external table. It is not built on catalogSpec, whose
// identity fields are strings, because the partition identity encodes the values map.
type externalPartitionDataSource struct {
	// dataSourceClient provides read-only catalog execution and binding checks.
	dataSourceClient
}

var _ = registerDataSource(newExternalPartitionDataSource)

// newExternalPartitionDataSource constructs a read-only partition lookup.
func newExternalPartitionDataSource() datasource.DataSource { return &externalPartitionDataSource{} }

// Metadata identifies the external partition data source to Terraform.
func (d *externalPartitionDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_external_partition"
}

// Schema defines the partition selector and its observed location.
func (d *externalPartitionDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up one partition of a partitioned Redshift Spectrum external table.",
		Attributes: map[string]schema.Attribute{
			"id":       dataSourceIDAttribute(),
			"database": schema.StringAttribute{Required: true, MarkdownDescription: "Local Redshift database holding the external schema."},
			"schema":   schema.StringAttribute{Required: true, MarkdownDescription: "External schema of the table."},
			"table":    schema.StringAttribute{Required: true, MarkdownDescription: "Partitioned external table."},
			"values":   schema.MapAttribute{ElementType: types.StringType, Required: true, MarkdownDescription: "Partition value for every partition key of the table, keyed by partition key name."},
			"location": schema.StringAttribute{Computed: true, MarkdownDescription: "S3 location the catalog records for the partition; the catalog drops a folder's trailing slash and truncates locations to 128 characters."},
		},
	}
}

// Read resolves the partition without taking ownership; a missing partition or table is an error.
func (d *externalPartitionDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data externalPartitionModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	reader := &externalPartitionResource{d.resourceClient}
	_, found, err := reader.read(ctx, &data)
	if err != nil {
		resp.Diagnostics.AddError("Read external partition", err.Error())
		return
	}
	if !found {
		resp.Diagnostics.AddError("External partition not found", "No partition with these values exists in external table "+data.Table.ValueString()+".")
		return
	}
	data.ID = reader.partitionIdentity(data)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
