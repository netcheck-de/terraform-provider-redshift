package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// databaseDataSource exposes local/shared database metadata without owning the object.
type databaseDataSource struct {
	// dataSourceClient provides read-only catalog execution and binding checks.
	dataSourceClient
}

// databaseData shares the resource's readable attributes without lifecycle ownership.
type databaseData = databaseModel

var _ = registerDataSource(newDatabaseDataSource)

// newDatabaseDataSource constructs a read-only local/shared database lookup.
func newDatabaseDataSource() datasource.DataSource { return &databaseDataSource{} }

// Metadata identifies the database data source to Terraform.
func (d *databaseDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_database"
}

// Schema defines the lookup name and observed database/share attributes.
func (d *databaseDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a local or shared database and its producer binding.",
		Attributes: map[string]schema.Attribute{
			"id":                 dataSourceIDAttribute(),
			"name":               schema.StringAttribute{Required: true, MarkdownDescription: "Database name."},
			"datashare_arn":      schema.StringAttribute{Computed: true, MarkdownDescription: "Backing producer datashare ARN; null for local databases. Shared lookups require redshift:DescribeDataShares."},
			"database_type":      schema.StringAttribute{Computed: true, MarkdownDescription: "`local` or `shared`."},
			"with_permissions":   schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether the shared database requires object grants."},
			"share_name":         schema.StringAttribute{Computed: true, MarkdownDescription: "Producer share name; null for local databases."},
			"producer_account":   schema.StringAttribute{Computed: true, MarkdownDescription: "Producer account ID; null for local databases."},
			"producer_namespace": schema.StringAttribute{Computed: true, MarkdownDescription: "Producer namespace ID; null for local databases."},
		},
	}
}

// Read looks up database metadata and reports an error when the database is absent.
func (d *databaseDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data databaseData
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	observed, found, err := d.databaseMetadata(ctx, data.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Read database", err.Error())
		return
	}
	if !found {
		resp.Diagnostics.AddError("Database not found", "No database named "+data.Name.ValueString()+" exists.")
		return
	}
	data = observed
	identity := map[string]string{"name": data.Name.ValueString()}
	if data.DatabaseType.ValueString() == "shared" {
		if d.datashareARN == nil {
			resp.Diagnostics.AddError("Discover datashare ARN", "The provider has no datashare metadata discovery client.")
			return
		}
		source := shareSource{Account: data.ProducerAccount.ValueString(), Namespace: data.ProducerNamespace.ValueString(), Name: data.ShareName.ValueString()}
		value, err := d.datashareARN(ctx, source)
		if err != nil {
			resp.Diagnostics.AddError("Discover datashare ARN", err.Error())
			return
		}
		binding, err := parseShare(value)
		if err != nil || binding != source {
			resp.Diagnostics.AddError("Discover datashare ARN", "The discovered ARN does not match the database's producer binding.")
			return
		}
		data.DatashareARN = types.StringValue(value)
		identity["datashare_arn"] = value
	}
	data.ID = d.identity(d.database.ValueString(), identity)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
