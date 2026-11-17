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

var _ = registerParity(parityCase{source: newExternalPartitionsDataSource, resource: newExternalPartitionResource, collection: true, filters: []string{"database", "schema", "table"}})

// externalPartitionsRead reads the partition listing as a transcript step.
func externalPartitionsRead(t *testing.T, filters map[string]string) func(sqlclient.Client) ([]map[string]attr.Value, map[string]string, error) {
	t.Helper()

	return func(client sqlclient.Client) ([]map[string]attr.Value, map[string]string, error) {
		items, identity, diagnostics := discoveryRead(t, newExternalPartitionsDataSource, filters, client)
		if diagnostics.HasError() {
			return nil, nil, errors.New(diagnostics.Errors()[0].Detail())
		}

		return items, identity, nil
	}
}

// TestExternalPartitionsListingTranscript lists the partitions of partitioned tables, keyed by partition key, in value order,
// and filters by schema and table.
func TestExternalPartitionsListingTranscript(t *testing.T) {
	c := externalTablesFake()
	events := fakeState[*externalTableFamily](c, "external_table").tables["events"]
	events.partitions = append([]externalTableFakePartition{{values: []string{"2024-01-02"}, location: "s3://example-bucket/events/event_date=2024-01-02"}}, events.partitions...)

	items := listingTranscript(t, "external_partitions", "database", c, externalPartitionsRead(t, nil))
	require.Len(t, items, 2)
	assert.Equal(t, map[string]attr.Value{
		"database": types.StringValue("admin"), "schema": types.StringValue(externalTableFakeSchema), "table": types.StringValue("events"),
		"values": externalTableTestMap("event_date", "2024-01-01"), "location": types.StringValue("s3://example-bucket/events/event_date=2024-01-01"),
	}, items[0])
	assert.Equal(t, externalTableTestMap("event_date", "2024-01-02"), items[1]["values"])

	items = listingTranscript(t, "external_partitions", "table", c, externalPartitionsRead(t, map[string]string{"schema": externalTableFakeSchema, "table": "Events"}))
	assert.Len(t, items, 2, "table names compare without case")

	items = listingTranscript(t, "external_partitions", "unpartitioned_table", c, externalPartitionsRead(t, map[string]string{"table": "metrics"}))
	assert.Empty(t, items, "without partition keys the partitions are not read")

	discoveryFailures(t, newExternalPartitionsDataSource, nil)
}

// TestExternalPartitionListings keeps the partitions of the database's tables, skips those an equally named schema of
// another database reports, and rejects ambiguous or malformed rows.
func TestExternalPartitionListings(t *testing.T) {
	keys := map[[2]string][]string{{"lake", "sales"}: {"month", "region"}}
	row := func(table, values, location string) sqlclient.Row {
		return sqlclient.Row{"schemaname": "lake", "tablename": table, "values": values, "location": location}
	}

	partitions, err := externalPartitionListings([]sqlclient.Row{
		row("sales", `["2024-02","eu"]`, "s3://b/2"),
		row("other", `["x"]`, "s3://b/other"),
		row("sales", `["2024-01"]`, "s3://elsewhere"),
		row("sales", `["2024-01","eu"]`, "s3://b/1"),
		row("sales", `["2024-01","eu"]`, "s3://b/1"),
	}, keys)
	require.NoError(t, err)
	assert.Equal(t, []externalPartitionListing{
		{schema: "lake", table: "sales", keys: []string{"month", "region"}, values: []string{"2024-01", "eu"}, location: "s3://b/1"},
		{schema: "lake", table: "sales", keys: []string{"month", "region"}, values: []string{"2024-02", "eu"}, location: "s3://b/2"},
	}, partitions)

	_, err = externalPartitionListings([]sqlclient.Row{row("sales", `["2024-01","eu"]`, "s3://b/1"), row("sales", `["2024-01","eu"]`, "s3://c/1")}, keys)
	require.ErrorContains(t, err, "ambiguous")

	_, err = externalPartitionListings([]sqlclient.Row{row("sales", `not json`, "s3://b/1")}, keys)
	require.Error(t, err)
}

// TestExternalPartitionsListingReportsPartitionFailure reports a failed partition read instead of an empty listing.
func TestExternalPartitionsListingReportsPartitionFailure(t *testing.T) {
	c := fullCatalog()
	_, _, diagnostics := discoveryRead(t, newExternalPartitionsDataSource, nil, queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		if strings.Contains(sql, "svv_external_partitions") {
			return nil, errors.New("glue unavailable")
		}

		return c.Query(ctx, target, sql, parameters)
	}))
	require.True(t, diagnostics.HasError())
	assert.Contains(t, diagnostics.Errors()[0].Detail(), "glue unavailable")
}

// TestExternalPartitionsListingRejectsMalformedMetadata propagates malformed key and partition metadata through the
// data source, without returning an apparently successful empty listing.
func TestExternalPartitionsListingRejectsMalformedMetadata(t *testing.T) {
	for _, test := range []struct {
		name, catalog, want string
		row                 sqlclient.Row
	}{
		{"partition keys", "svv_external_columns", "incomplete catalog metadata", sqlclient.Row{
			"schemaname": externalTableFakeSchema, "tablename": "events", "columnname": "event_date",
			"external_type": "date", "columnnum": "invalid", "part_key": "1",
		}},
		{"partition values", "svv_external_partitions", "partition values", sqlclient.Row{
			"schemaname": externalTableFakeSchema, "tablename": "events", "values": "not json", "location": "s3://bucket/events",
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := fullCatalog()
			injected := false
			client := queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
				if strings.Contains(sql, test.catalog) {
					injected = true
					return []sqlclient.Row{test.row}, nil
				}

				return c.Query(ctx, target, sql, parameters)
			})
			source := newExternalPartitionsDataSource()
			state, diagnostics := readSource(t, source, collectionConfig(t, source, nil), client)
			require.True(t, injected, "the listing must read the malformed metadata")
			require.True(t, diagnostics.HasError())
			assert.Contains(t, diagnostics.Errors()[0].Detail(), test.want)
			assert.True(t, state.Raw.IsNull(), "malformed metadata must not publish a partial listing")
		})
	}
}

// TestExternalPartitionsSQL pins the partition listing and its filters.
func TestExternalPartitionsSQL(t *testing.T) {
	checkSQL(t, "external_partitions_sql", []sqlCase{
		{"all", discoverySQL(listExternalPartitionsQuery("", ""))},
		{"schema_table", discoverySQL(listExternalPartitionsQuery(`Lake"Schema`, `Odd"Table`))},
	})
}
