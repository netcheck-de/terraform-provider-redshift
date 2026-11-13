package provider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newExternalTablesDataSource, resource: newExternalTableResource, collection: true, filters: []string{"database", "schema"}})

// externalTablesFake adds an unpartitioned Parquet table to the representative partitioned text table.
func externalTablesFake() *catalog {
	c := fullCatalog()
	fakeState[*externalTableFamily](c, "external_table").tables["metrics"] = &externalTableFake{
		location: "s3://example-bucket/metrics", inputFormat: externalTableFileFormats["PARQUET"],
		outputFormat: "org.apache.hadoop.hive.ql.io.parquet.MapredParquetOutputFormat", serde: externalTableImpliedSerdes["PARQUET"],
		serdeParameters: map[string]string{"serialization.format": "1"}, parameters: map[string]string{"EXTERNAL": "TRUE"},
		columns: []externalTableFakeColumn{{"id", "bigint"}, {"payload", "string"}},
	}

	return c
}

// externalTablesRead reads the external table listing as a transcript step.
func externalTablesRead(t *testing.T, filters map[string]string) func(sqlclient.Client) ([]map[string]attr.Value, map[string]string, error) {
	t.Helper()

	return func(client sqlclient.Client) ([]map[string]attr.Value, map[string]string, error) {
		items, identity, diagnostics := discoveryRead(t, newExternalTablesDataSource, filters, client)
		if diagnostics.HasError() {
			return nil, nil, errors.New(diagnostics.Errors()[0].Detail())
		}

		return items, identity, nil
	}
}

// TestExternalTablesListingTranscript lists every table with the definition the lookup reports, from two catalog reads.
func TestExternalTablesListingTranscript(t *testing.T) {
	c := externalTablesFake()
	items := listingTranscript(t, "external_tables", "database", c, externalTablesRead(t, nil))
	require.Equal(t, []string{"events", "metrics"}, discoveryNames(items, "name"))

	lookup, ok := externalTableLookupState(t, c, "events")
	require.True(t, ok)
	assert.Equal(t, map[string]attr.Value{
		"database": lookup.Database, "schema": lookup.Schema, "name": lookup.Name, "column": lookup.Columns,
		"partition_key": lookup.PartitionKeys, "field_delimiter": lookup.FieldDelimiter, "line_delimiter": lookup.LineDelimiter,
		"serde": lookup.Serde, "serde_properties": lookup.SerdeProperties, "stored_as": lookup.StoredAs,
		"input_format": lookup.InputFormat, "output_format": lookup.OutputFormat, "location": lookup.Location,
		"table_properties": lookup.TableProperties,
	}, items[0], "the listing reports what the lookup reports")
	assert.Equal(t, externalTableTestColumns("id", "BIGINT", "payload", "STRING"), items[1]["column"])
	assert.True(t, items[1]["partition_key"].IsNull(), "an unpartitioned table has no partition keys")
	assert.Equal(t, types.StringValue("PARQUET"), items[1]["stored_as"])

	items = listingTranscript(t, "external_tables", "schema", c, externalTablesRead(t, map[string]string{"schema": "EXAMPLE_EXTERNAL"}))
	assert.Equal(t, []string{"events", "metrics"}, discoveryNames(items, "name"), "schema names compare without case")

	items = listingTranscript(t, "external_tables", "other_schema", c, externalTablesRead(t, map[string]string{"schema": "other"}))
	assert.Empty(t, items, "without tables the columns are not read")

	discoveryFailures(t, newExternalTablesDataSource, nil, sqlclient.Row{"schemaname": "lake", "tablename": ""})
}

// TestExternalTablesListingRejectsIncompleteColumns reports a table whose columns the catalog does not report.
func TestExternalTablesListingRejectsIncompleteColumns(t *testing.T) {
	c := externalTablesFake()
	_, _, diagnostics := discoveryRead(t, newExternalTablesDataSource, nil, queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		rows, err := c.Query(ctx, target, sql, parameters)
		if strings.Contains(sql, "svv_external_columns") {
			// Drop the columns of metrics.
			var kept []sqlclient.Row

			for _, row := range rows {
				if row["tablename"] != "metrics" {
					kept = append(kept, row)
				}
			}

			return kept, err
		}

		return rows, err
	}))
	require.True(t, diagnostics.HasError())
	assert.Contains(t, diagnostics.Errors()[0].Detail(), "metrics")
}

// TestExternalTablesSQL pins the table and column listings and their filters.
func TestExternalTablesSQL(t *testing.T) {
	checkSQL(t, "external_tables_sql", []sqlCase{
		{"tables", discoverySQL(listExternalTablesQuery("admin", ""))},
		{"tables_schema", discoverySQL(listExternalTablesQuery("admin", `Lake"Schema`))},
		{"columns", discoverySQL(listExternalTableColumnsQuery("admin", "", ""))},
		{"columns_schema_table", discoverySQL(listExternalTableColumnsQuery("admin", `Lake"Schema`, `Odd"Table`))},
	})
}
