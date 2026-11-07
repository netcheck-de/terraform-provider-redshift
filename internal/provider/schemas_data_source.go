package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

var _ = registerDataSource(newSchemasDataSource)

// newSchemasDataSource lists the local schemas of one database with their owners and quotas.
func newSchemasDataSource() datasource.DataSource {
	return newCollectionDataSource(collectionSpec{
		name:          "schemas",
		databaseField: "database",
		description: "Lists the local schemas of one database from `PG_NAMESPACE`, including system schemas such as `pg_catalog`, with their owners and disk quotas. " +
			"Schemas that `PG_EXTERNAL_SCHEMA` records as external are left out; `redshift_external_schemas` lists those. `PG_EXTERNAL_SCHEMA` shows a regular user only the external schemas whose metadata they may access, so for a regular user other external schemas appear as local ones. " + discoveryLocalScope,
		filters: map[string]schema.Attribute{
			"database": discoveryDatabaseFilter("local schemas"),
		},
		element: describedOutputs(collectionElement(newSchemaResource, "database", "name", "owner", "quota"), map[string]string{
			"database": "Database containing the schema.",
			"name":     "Schema name.",
			"owner":    "SQL user owning the schema.",
			"quota":    schemaQuotaLookupDescription,
		}),
		list: schemasList,
	})
}

// schemasList reads the schemas, the visible quotas, and the connected user in the selected database: three
// queries, however many schemas it holds. A schema without a quota row is UNLIMITED when the connection may see
// its quota, as in the lookup, and null otherwise.
func schemasList(ctx context.Context, client *resourceClient, filters types.Object) ([]map[string]attr.Value, error) {
	database, local, err := discoveryLocalDatabase(ctx, client, filters, "database")
	if err != nil || !local {
		return []map[string]attr.Value{}, err
	}

	rows, err := client.selectRows(ctx, database, listSchemasQuery())
	if err != nil {
		return nil, err
	}

	quotaRows, err := client.selectRows(ctx, database, listSchemaQuotasQuery(database))
	if err != nil {
		return nil, err
	}

	quotas := map[string]sqlclient.Row{}
	for _, row := range quotaRows {
		quotas[row["schema_name"]] = row
	}

	session, err := schemaSessionOf(ctx, client, database)
	if err != nil {
		return nil, err
	}

	items := make([]map[string]attr.Value, 0, len(rows))
	for _, row := range rows {
		name, owner := row["schema_name"], row["owner"]
		if name == "" || owner == "" {
			return nil, fmt.Errorf("PG_NAMESPACE returned a schema without a name or owner in database %q", database)
		}

		quota := types.Int64Null()
		// SVV_REDSHIFT_SCHEMA_QUOTA pads its names, which the query trims.
		if visible, ok := quotas[strings.Trim(name, " ")]; ok {
			value, err := schemaQuotaValue([]sqlclient.Row{visible})
			if err != nil {
				return nil, fmt.Errorf("schema %q: %w", name, err)
			}

			quota = types.Int64Value(value)
		} else if session.sees(owner) {
			quota = types.Int64Value(schemaUnlimited)
		}

		items = append(items, map[string]attr.Value{
			"database": types.StringValue(database), "name": types.StringValue(name), "owner": types.StringValue(owner), "quota": quota,
		})
	}

	return items, nil
}
