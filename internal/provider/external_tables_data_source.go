package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newExternalTablesDataSource)

// externalTablesElements are the listed attributes: everything the lookup observes.
var externalTablesElements = []string{
	"database", "schema", "name", "column", "partition_key", "field_delimiter", "line_delimiter", "serde", "serde_properties",
	"stored_as", "input_format", "output_format", "location", "table_properties",
}

// newExternalTablesDataSource lists the external tables of one database, the objects redshift_external_table manages.
func newExternalTablesDataSource() datasource.DataSource {
	return newCollectionDataSource(collectionSpec{
		name:          "external_tables",
		databaseField: "database",
		description: "Lists the Redshift Spectrum external tables of one local database with their definitions, from `SVV_EXTERNAL_TABLES` and `SVV_EXTERNAL_COLUMNS`. " +
			"Superusers see every table; other users see only the tables they have access to. A database that is not local lists no tables.",
		filters: map[string]schema.Attribute{
			"database": discoveryDatabaseFilter("external tables"),
			"schema":   discoveryFilter("Only tables in this external schema; compared without case."),
		},
		element: describedOutputs(collectionElement(newExternalTableResource, externalTablesElements...), externalTableOutputDescriptions),
		list:    externalTablesList,
	})
}

// externalTablesList reads every matching table and all of their columns with one query each, groups the columns by
// table, and derives each definition as the lookup does.
func externalTablesList(ctx context.Context, client *resourceClient, filters types.Object) ([]map[string]attr.Value, error) {
	database, schemaName := discoveryDatabase(client, filters), objectString(filters, "schema")
	items := []map[string]attr.Value{}

	if exists, err := client.localDatabaseExists(ctx, database); err != nil || !exists {
		return items, err
	}

	tables, err := client.selectRows(ctx, database, listExternalTablesQuery(database, schemaName))
	if err != nil || len(tables) == 0 {
		return items, err
	}

	columnRows, err := client.selectRows(ctx, database, listExternalTableColumnsQuery(database, schemaName, ""))
	if err != nil {
		return nil, err
	}

	columns := externalTableColumnGroups(columnRows)

	for _, table := range tables {
		if table["schemaname"] == "" || table["tablename"] == "" {
			return nil, fmt.Errorf("SVV_EXTERNAL_TABLES returned a table without a schema or name in database %q", database)
		}

		catalog, err := externalTableCatalogOf(table, columns[[2]string{table["schemaname"], table["tablename"]}])
		if err != nil {
			return nil, fmt.Errorf("external table %s.%s: %w", table["schemaname"], table["tablename"], err)
		}

		observed := observeExternalTable(catalog, externalTableLookupModel(database, table["schemaname"], table["tablename"]), externalTableLookup)
		items = append(items, map[string]attr.Value{
			"database": observed.Database, "schema": observed.Schema, "name": observed.Name,
			"column": observed.Columns, "partition_key": observed.PartitionKeys,
			"field_delimiter": observed.FieldDelimiter, "line_delimiter": observed.LineDelimiter,
			"serde": observed.Serde, "serde_properties": observed.SerdeProperties, "stored_as": observed.StoredAs,
			"input_format": observed.InputFormat, "output_format": observed.OutputFormat,
			"location": observed.Location, "table_properties": observed.TableProperties,
		})
	}

	return items, nil
}

// externalTableLookupModel is a lookup configuration of one table: the identity, with every observed value null.
func externalTableLookupModel(database, schemaName, name string) externalTableModel {
	return externalTableModel{
		ID: types.StringNull(), Database: types.StringValue(database), Schema: types.StringValue(schemaName), Name: types.StringValue(name),
		Columns: types.ListNull(externalTableColumnType), PartitionKeys: types.ListNull(externalTableColumnType),
		FieldDelimiter: types.StringNull(), LineDelimiter: types.StringNull(), Serde: types.StringNull(),
		SerdeProperties: types.MapNull(types.StringType), StoredAs: types.StringNull(), InputFormat: types.StringNull(),
		OutputFormat: types.StringNull(), Location: types.StringNull(), TableProperties: types.MapNull(types.StringType),
	}
}
