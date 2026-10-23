package provider

import (
	"context"
	"strings"

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
		MarkdownDescription: "Looks up a local database with its owner and options, a zero-ETL destination database with its integration, or a shared database with its producer binding.",
		Attributes: map[string]schema.Attribute{
			"id":                 dataSourceIDAttribute(),
			"name":               schema.StringAttribute{Required: true, MarkdownDescription: "Database name."},
			"datashare_arn":      schema.StringAttribute{Computed: true, MarkdownDescription: "Backing producer datashare ARN; null for local databases. Shared lookups require redshift:DescribeDataShares."},
			"database_type":      schema.StringAttribute{Computed: true, MarkdownDescription: databaseTypeDescription},
			"with_permissions":   schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether the shared database requires object grants."},
			"share_name":         schema.StringAttribute{Computed: true, MarkdownDescription: "Producer share name; null for local databases."},
			"producer_account":   schema.StringAttribute{Computed: true, MarkdownDescription: "Producer account ID; null for local databases."},
			"producer_namespace": schema.StringAttribute{Computed: true, MarkdownDescription: "Producer namespace ID; null for local databases."},
			"owner":              schema.StringAttribute{Computed: true, MarkdownDescription: "SQL user owning a local database; null for shared databases."},
			"connection_limit":   schema.Int64Attribute{Computed: true, MarkdownDescription: "Maximum concurrent connections to a local database; `-1` means `UNLIMITED`. Null for shared databases."},
			"collation":          schema.StringAttribute{Computed: true, MarkdownDescription: "`CASE_SENSITIVE` or `CASE_INSENSITIVE` for a local database, read with `DB_COLLATION()` in a session inside it; null for shared databases, or with a warning when that session is refused, for example by the database's connection limit."},
			"isolation_level":    schema.StringAttribute{Computed: true, MarkdownDescription: "`SERIALIZABLE` or `SNAPSHOT` for a local database; null for shared databases."},
			"integration":        databaseIntegrationLookupAttribute(),
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
	observed, found, err := d.databaseCatalog(ctx, data.Name.ValueString(), true)
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
	// SHOW DATABASES does not document the type of a zero-ETL destination, which may report as local, so every
	// database but a shared one is looked up in SVV_INTEGRATION. This supplementary metadata must not prevent a
	// non-superuser from looking up an ordinary local database; Redshift denies access to the view rather than
	// filtering its rows. Only that specific denial is optional, not other catalog errors.
	if data.DatabaseType.ValueString() != databaseTypeShared {
		integration, found, err := d.databaseIntegration(ctx, data.Name.ValueString())
		if err != nil && strings.Contains(strings.ToLower(err.Error()), "permission denied for relation svv_integration") {
			resp.Diagnostics.AddWarning("Database integration metadata unavailable",
				"SVV_INTEGRATION requires a superuser. The database metadata is returned, but integration is null because the provider SQL identity cannot inspect the view.")
			err = nil
		}
		if err != nil {
			resp.Diagnostics.AddError("Read database integration", err.Error())
			return
		}
		if found {
			data.Integration = &integration
			identity["integration_id"] = integration.IntegrationID.ValueString()
		}
	}
	// A destination takes no collation, so the collation is read after the integration, which skips it.
	d.databaseReadCollation(ctx, &data, &resp.Diagnostics)
	if data.DatabaseType.ValueString() == databaseTypeShared {
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
