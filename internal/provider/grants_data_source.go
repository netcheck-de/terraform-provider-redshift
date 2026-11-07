package provider

import (
	"context"
	"fmt"

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
			"`SHOW GRANTS` shows superusers every grant and other users only the grants visible to them.",
		filters: map[string]schema.Attribute{
			"database_name": schema.StringAttribute{Optional: true, MarkdownDescription: "Local or shared database whose scoped grants are listed; defaults to the provider database.", Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
			"schema_name":   schema.StringAttribute{Optional: true, MarkdownDescription: "Lists the grants on this schema and the scoped grants limited to it; database-wide grants when omitted.", Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
			"role":          schema.StringAttribute{Optional: true, MarkdownDescription: "Only grants to this role.", Validators: []validator.String{stringvalidator.LengthAtLeast(1), stringvalidator.ConflictsWith(path.MatchRoot("user"))}},
			"user":          schema.StringAttribute{Optional: true, MarkdownDescription: "Only grants to this user.", Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
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
			admin, database, schemaName := client.database.ValueString(), objectString(data, "database_name"), objectString(data, "schema_name")
			if database == "" {
				database = admin
			}

			// Local grants are read in their database and shared ones from admin, as redshift_grant reads them.
			kinds, err := client.selectRows(ctx, admin, grantDatabaseTypeQuery(database))
			if err != nil {
				return nil, err
			}

			if len(kinds) == 0 {
				return nil, fmt.Errorf("database %q does not exist", database)
			}

			target := admin
			if kinds[0]["database_type"] == "local" {
				target = database
			}

			rows, err := client.queryDatabase(ctx, target, listGrantsStatement(database, schemaName), nil)
			if err != nil {
				return nil, err
			}

			return grantsItems(database, schemaName, objectString(data, "role"), objectString(data, "user"), rows), nil
		},
	})
}
