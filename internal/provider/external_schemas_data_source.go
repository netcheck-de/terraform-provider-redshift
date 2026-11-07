package provider

import (
	"context"
	"fmt"
	"maps"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newExternalSchemasDataSource)

// externalSchemaListed are the element attributes of the external schema listing: the identity, the source form,
// the owner, and every option the lookup reads from the catalog.
var externalSchemaListed = []string{
	"database", "name", "source_type", "owner", "glue_database", "source_database", "source_schema", "iam_role_arn",
	"region", "uri", "port", "secret_arn", "authentication", "authentication_arn",
}

// newExternalSchemasDataSource lists the external schemas of one database with their source options.
func newExternalSchemasDataSource() datasource.DataSource {
	descriptions := map[string]string{"database": "Database containing the external schema.", "name": "External schema name."}
	maps.Copy(descriptions, externalSchemaOutputDescriptions)

	return newCollectionDataSource(collectionSpec{
		name:          "external_schemas",
		databaseField: "database",
		description: "Lists the external schemas of one local database from `PG_NAMESPACE` joined to `SVV_EXTERNAL_SCHEMAS`, with their source form, recorded options, and owner. " +
			"`SVV_EXTERNAL_SCHEMAS` shows a regular user only their own external schemas, so only a superuser connection lists every one. Schemas of source kinds `redshift_external_schema` cannot manage, such as system-table schemas, are omitted.",
		filters: map[string]schema.Attribute{
			"database": discoveryDatabaseFilter("external schemas"),
		},
		element: describedOutputs(collectionElement(newExternalSchemaResource, externalSchemaListed...), descriptions),
		list:    externalSchemasList,
	})
}

// externalSchemasList reads every visible external schema of the selected database in one query and decodes each
// row as the lookup does.
func externalSchemasList(ctx context.Context, client *resourceClient, filters types.Object) ([]map[string]attr.Value, error) {
	database := discoveryDatabase(client, filters)

	rows, err := client.selectRows(ctx, database, listExternalSchemasQuery())
	if err != nil {
		return nil, err
	}

	kinds := externalSchemaKinds()
	null := types.StringNull()
	items := make([]map[string]attr.Value, 0, len(rows))

	for _, row := range rows {
		if _, supported := kinds[row["eskind"]]; !supported {
			continue
		}

		prior := externalSchemaModel{
			Database: types.StringValue(database), SourceSchema: null, IAMRoleARN: null, Region: null, URI: null,
			Port: types.Int64Null(), SecretARN: null, Authentication: null, AuthenticationARN: null,
		}

		observed, err := externalSchemaFromRow(prior, row)
		if err != nil {
			return nil, fmt.Errorf("database %q: %w", database, err)
		}

		items = append(items, map[string]attr.Value{
			"database": observed.Database, "name": observed.Name, "source_type": observed.SourceType, "owner": observed.Owner,
			"glue_database": observed.GlueDatabase, "source_database": observed.SourceDatabase, "source_schema": observed.SourceSchema,
			"iam_role_arn": observed.IAMRoleARN, "region": observed.Region, "uri": observed.URI, "port": observed.Port,
			"secret_arn": observed.SecretARN, "authentication": observed.Authentication, "authentication_arn": observed.AuthenticationARN,
		})
	}

	return items, nil
}
