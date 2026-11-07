package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newGrantsDataSource)

// newGrantsDataSource lists the scoped grants of one database, or of one of its schemas, as redshift_grant reads them.
func newGrantsDataSource() datasource.DataSource {
	return newCollectionDataSource(collectionSpec{
		name:          "grants",
		databaseField: "database_name",
		description: "Lists the grants `redshift_grant` manages on one local or shared database, or on one schema of it, one element per recipient and scope, from `SHOW GRANTS ON DATABASE` or `SHOW GRANTS ON SCHEMA`, without taking ownership. " +
			"Elements cover roles, users, and producer datashares; grants to groups and `PUBLIC` are left out. `FUNCTIONS` and `PROCEDURES` share one Redshift catalog scope, which is listed under the name the catalog reports. " +
			"Superusers see every grant; other users see only the grants `SHOW GRANTS` shows them. " + discoverySharedScope,
		filters: map[string]schema.Attribute{
			"database_name": discoveryDatabaseFilter("scoped grants"),
			"schema_name":   discoveryFilter("Lists the grants on this schema and the scoped grants limited to it; database-wide grants when omitted."),
			"role":          schema.StringAttribute{Optional: true, MarkdownDescription: "Only grants to this role.", Validators: []validator.String{stringvalidator.LengthAtLeast(1), stringvalidator.ConflictsWith(path.MatchRoot("user"))}},
			"user":          discoveryFilter("Only grants to this user."),
		},
		element: describedOutputs(collectionElement(newGrantResource, "database_name", "schema_name", "role", "user", "datashare", "scope", "privileges", "grant_option_privileges"), map[string]string{
			"database_name":           "Database receiving the grants.",
			"schema_name":             "Schema the grants apply to; null for database-wide grants.",
			"role":                    "Receiving role; null for other recipients.",
			"user":                    "Receiving user; null for other recipients.",
			"datashare":               "Receiving producer datashare; null for other recipients.",
			"scope":                   "`DATABASE`, `SCHEMA`, or the object class of a scoped grant: `SCHEMAS`, `TABLES`, `FUNCTIONS`, `PROCEDURES`, `LANGUAGES`, `COPY JOBS`, or `TEMPLATES`.",
			"privileges":              "Explicit privileges the recipient holds in this scope, with `TEMP` reported as `TEMPORARY`.",
			"grant_option_privileges": "Subset of `privileges` the recipient holds `WITH GRANT OPTION`; only users hold grant options.",
		}),
		list: func(ctx context.Context, client *resourceClient, data types.Object) ([]map[string]attr.Value, error) {
			database, schemaName := discoveryDatabaseOf(client, data, "database_name"), objectString(data, "schema_name")

			// Local grants are read in their database and shared ones from admin, as redshift_grant reads them.
			target, exists, err := discoveryGrantTarget(ctx, client, database)
			if err != nil || !exists {
				return []map[string]attr.Value{}, err
			}

			rows, err := client.queryDatabase(ctx, target, listGrantsStatement(database, schemaName), nil)
			if err != nil {
				return nil, err
			}

			return grantsItems(database, schemaName, objectString(data, "role"), objectString(data, "user"), rows), nil
		},
	})
}
