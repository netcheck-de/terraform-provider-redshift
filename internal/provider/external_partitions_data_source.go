package provider

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

var _ = registerDataSource(newExternalPartitionsDataSource)

// newExternalPartitionsDataSource lists the partitions of the external tables of one database, the objects
// redshift_external_partition manages.
func newExternalPartitionsDataSource() datasource.DataSource {
	return newCollectionDataSource(collectionSpec{
		name:          "external_partitions",
		databaseField: "database",
		description: "Lists the partitions of the partitioned Redshift Spectrum external tables of one local database, from `SVV_EXTERNAL_PARTITIONS` with partition keys from `SVV_EXTERNAL_COLUMNS`. " +
			"Superusers see every partition; other users see only the tables they have access to. " + discoveryLocalScope,
		filters: map[string]schema.Attribute{
			"database": discoveryDatabaseFilter("partitions"),
			"schema":   discoveryFilter("Only partitions of tables in this external schema; compared without case."),
			"table":    discoveryFilter("Only partitions of this external table; compared without case."),
		},
		element: describedOutputs(collectionElement(newExternalPartitionResource, "database", "schema", "table", "values", "location"), map[string]string{
			"values":   "Partition value for every partition key of the table, keyed by the partition key name the catalog reports.",
			"location": "S3 location the catalog records for the partition; the catalog drops a folder's trailing slash and truncates locations to 128 characters.",
		}),
		list: externalPartitionsList,
	})
}

// externalPartitionListing is one listed partition.
type externalPartitionListing struct {
	// schema and table identify the partitioned table as the catalog spells them.
	schema, table string
	// keys and values are the partition keys and values in key order.
	keys, values []string
	// location is the catalog location.
	location string
}

// externalPartitionsList reads the partition keys of the matching tables, then their partitions with one query.
// SVV_EXTERNAL_PARTITIONS has no database column, so the partition keys of the database's tables decide which
// partitions belong to it.
func externalPartitionsList(ctx context.Context, client *resourceClient, filters types.Object) ([]map[string]attr.Value, error) {
	database, schemaName, table := discoveryDatabase(client, filters), objectString(filters, "schema"), objectString(filters, "table")
	items := []map[string]attr.Value{}

	if exists, err := client.localDatabaseExists(ctx, database); err != nil || !exists {
		return items, err
	}

	columnRows, err := client.selectRows(ctx, database, listExternalTableColumnsQuery(database, schemaName, table))
	if err != nil {
		return nil, err
	}

	keys := map[[2]string][]string{}

	for relation, rows := range externalTableColumnGroups(columnRows) {
		_, partitionKeys, err := externalTableCatalogColumns(rows)
		if err != nil {
			return nil, err
		}

		for _, key := range partitionKeys {
			keys[relation] = append(keys[relation], key.name)
		}
	}

	if len(keys) == 0 {
		return items, nil
	}

	rows, err := client.selectRows(ctx, database, listExternalPartitionsQuery(schemaName, table))
	if err != nil {
		return nil, err
	}

	partitions, err := externalPartitionListings(rows, keys)
	if err != nil {
		return nil, err
	}

	for _, partition := range partitions {
		values := map[string]attr.Value{}
		for i, key := range partition.keys {
			values[key] = types.StringValue(partition.values[i])
		}

		items = append(items, map[string]attr.Value{
			"database": types.StringValue(database), "schema": types.StringValue(partition.schema), "table": types.StringValue(partition.table),
			"values": types.MapValueMust(types.StringType, values), "location": types.StringValue(partition.location),
		})
	}

	return items, nil
}

// externalPartitionListings matches partition rows to the partition keys of the database's tables, in schema, table,
// and value order. Rows of tables without keys in the database, or whose values do not fit the keys, belong to an
// equally named external schema of another database. The same Glue table seen through both schemas reports the same
// location; different locations for the same values are ambiguous, as for the lookup.
func externalPartitionListings(rows []sqlclient.Row, keys map[[2]string][]string) ([]externalPartitionListing, error) {
	var partitions []externalPartitionListing

	// seen maps schema, table, and the canonical values array to the partition's index.
	seen := map[[3]string]int{}

	for _, row := range rows {
		relation := [2]string{row["schemaname"], row["tablename"]}

		names, ok := keys[relation]
		if !ok {
			continue
		}

		values, err := externalPartitionCatalogValues(row["values"])
		if err != nil {
			return nil, err
		}

		if len(values) != len(names) {
			continue
		}

		encoded, _ := json.Marshal(values) // String slices are always JSON-serializable.
		key := [3]string{relation[0], relation[1], string(encoded)}

		if i, ok := seen[key]; ok {
			if partitions[i].location != row["location"] {
				return nil, fmt.Errorf("external partition %s of table %s.%s is ambiguous in the catalog: locations %s, %s", row["values"], relation[0], relation[1], partitions[i].location, row["location"])
			}

			continue
		}

		seen[key] = len(partitions)
		partitions = append(partitions, externalPartitionListing{schema: relation[0], table: relation[1], keys: names, values: values, location: row["location"]})
	}

	slices.SortFunc(partitions, func(a, b externalPartitionListing) int {
		return cmp.Or(cmp.Compare(a.schema, b.schema), cmp.Compare(a.table, b.table), slices.Compare(a.values, b.values))
	})

	return partitions, nil
}
