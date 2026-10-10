package provider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerLifecycleCase(lifecycleCase{
	name: "external partition", new: newExternalPartitionResource, model: externalPartitionTestModel(),
	absent: func(c *catalog) { externalTableFakeOf(c).partitions = nil },
	// DROP PARTITION is an ALTER TABLE clause, which the default deletion check does not know.
	isDeletion: func(sql string) bool { return strings.Contains(sql, " DROP PARTITION ") },
})

var _ = registerReplacementPolicy("redshift_external_partition", map[string]replaceRule{
	"database": replaceAlways,
	"schema":   replaceAlways,
	"table":    replaceAlways,
	"values":   replaceAlways,
	"location": replaceNever,
})

var _ = registerValidateConfigCase("external_partition", validateConfigCase{
	new:   newExternalPartitionResource,
	valid: externalPartitionTestModel(),
	invalid: func() externalPartitionModel {
		model := externalPartitionTestModel()
		model.Values = externalTableTestMap("event_date", "a", "Event_Date", "b")
		return model
	}(),
	unknown: func() externalPartitionModel {
		model := externalPartitionTestModel()
		model.Location = types.StringUnknown()
		model.Values = externalTableTestMap()
		return model
	}(),
})

// externalPartitionState runs one lifecycle operation and decodes the resulting state.
func externalPartitionState(t *testing.T, client dataapi.Client, operation string, prior, planned externalPartitionModel) (externalPartitionModel, error) {
	t.Helper()
	r := &externalPartitionResource{}
	configureTestResource(t, r, client)
	state, diagnostics := applyOperation(t, r, operation, prior, planned, nil)
	var observed externalPartitionModel
	if !state.Raw.IsNull() {
		require.False(t, state.Get(context.Background(), &observed).HasError())
	}
	if diagnostics.HasError() {
		return observed, errors.New(diagnostics.Errors()[0].Summary() + ": " + diagnostics.Errors()[0].Detail())
	}
	return observed, nil
}

// TestExternalPartitionLifecycle adds a partition with keys in catalog order, moves it, and drops it.
func TestExternalPartitionLifecycle(t *testing.T) {
	c := fullCatalog()
	model := externalPartitionTestModel()
	model.Values = externalTableTestMap("EVENT_DATE", "2024-02-01")
	model.Location = types.StringValue("s3://example-bucket/events/event_date=2024-02-01/")
	created, err := externalPartitionState(t, c, "create", model, model)
	require.NoError(t, err)
	assert.Equal(t, model.Location, created.Location, "the catalog's missing trailing slash is not drift")
	assert.JSONEq(t, `{"workgroup_name":"warehouse","database":"admin","schema":"example_external","table":"events","values":"{\"EVENT_DATE\":\"2024-02-01\"}"}`, created.ID.ValueString())
	moved := created
	moved.Location = types.StringValue("s3://example-bucket/archive/2024-02-01/")
	updated, err := externalPartitionState(t, c, "update", created, moved)
	require.NoError(t, err)
	assert.Equal(t, moved.Location, updated.Location)
	_, err = externalPartitionState(t, c, "delete", updated, updated)
	require.NoError(t, err)
	assert.Equal(t, []string{
		`ALTER TABLE "example_external"."events" ADD PARTITION ("EVENT_DATE" = '2024-02-01') LOCATION 's3://example-bucket/events/event_date=2024-02-01/'`,
		`ALTER TABLE "example_external"."events" PARTITION ("EVENT_DATE" = '2024-02-01') SET LOCATION 's3://example-bucket/archive/2024-02-01/'`,
		`ALTER TABLE "example_external"."events" DROP PARTITION ("EVENT_DATE" = '2024-02-01')`,
	}, c.writes)
	assert.Len(t, externalTableFakeOf(c).partitions, 1, "the representative partition is untouched")
}

// TestExternalPartitionImport restores the values map from the JSON-string identity field and reads the location.
func TestExternalPartitionImport(t *testing.T) {
	r := &externalPartitionResource{}
	configureTestResource(t, r, fullCatalog())
	id := `{"workgroup_name":"warehouse","database":"admin","schema":"example_external","table":"events","values":"{\"event_date\":\"2024-01-01\"}"}`
	require.False(t, importAndRead(t, r, id).HasError())
	imported := resource.ImportStateResponse{State: emptyState(t, r)}
	r.ImportState(context.Background(), resource.ImportStateRequest{ID: id}, &imported)
	var data externalPartitionModel
	require.False(t, imported.State.Get(context.Background(), &data).HasError())
	assert.Equal(t, externalTableTestMap("event_date", "2024-01-01"), data.Values)
	for _, invalid := range []string{
		`{"workgroup_name":"warehouse","database":"admin","schema":"example_external","table":"events"}`,
		`{"workgroup_name":"warehouse","database":"admin","schema":"example_external","table":"events","values":"[1]"}`,
		`{"workgroup_name":"warehouse","database":"admin","schema":"example_external","table":"events","values":"{}"}`,
		`{"workgroup_name":"warehouse","database":"admin","schema":"example_external","values":"{\"a\":\"b\"}"}`,
	} {
		response := resource.ImportStateResponse{State: emptyState(t, r)}
		r.ImportState(context.Background(), resource.ImportStateRequest{ID: invalid}, &response)
		assert.True(t, response.Diagnostics.HasError(), invalid)
	}
}

// TestExternalPartitionFailurePaths covers catalog checks before ADD PARTITION, malformed rows, and drift.
func TestExternalPartitionFailurePaths(t *testing.T) {
	model := externalPartitionTestModel()
	t.Run("values must match the partition keys", func(t *testing.T) {
		c := fullCatalog()
		wrong := model
		wrong.Values = externalTableTestMap("day", "1")
		_, err := externalPartitionState(t, c, "create", wrong, wrong)
		require.ErrorContains(t, err, "values must set every partition key of the table: event_date")
		assert.Empty(t, c.writes)
	})
	t.Run("invalid location before SQL", func(t *testing.T) {
		invalid := model
		invalid.Location = types.StringValue("/tmp/data")
		calls := 0
		_, err := externalPartitionState(t, queryFunc(func(context.Context, dataapi.Connection, string, map[string]string) ([]dataapi.Row, error) {
			calls++
			return nil, nil
		}), "create", invalid, invalid)
		require.ErrorContains(t, err, "s3://")
		assert.Zero(t, calls)
	})
	t.Run("existing partition", func(t *testing.T) {
		_, err := externalPartitionState(t, fullCatalog(), "create", model, model)
		require.ErrorContains(t, err, "already exists")
	})
	t.Run("malformed values", func(t *testing.T) {
		client := queryFunc(func(_ context.Context, _ dataapi.Connection, sql string, _ map[string]string) ([]dataapi.Row, error) {
			if strings.Contains(sql, "svv_external_columns") {
				return []dataapi.Row{{"columnname": "event_date", "external_type": "date", "columnnum": "1", "part_key": "1"}}, nil
			}
			return []dataapi.Row{{"values": "not json", "location": "s3://x"}}, nil
		})
		_, err := externalPartitionState(t, client, "read", model, model)
		require.ErrorContains(t, err, "decode external partition values")
	})
	// Equally named external schemas of two databases both list their partitions in svv_external_partitions.
	partitionRows := func(rows ...dataapi.Row) dataapi.Client {
		return queryFunc(func(_ context.Context, _ dataapi.Connection, sql string, _ map[string]string) ([]dataapi.Row, error) {
			if strings.Contains(sql, "svv_external_columns") {
				return []dataapi.Row{{"columnname": "event_date", "external_type": "date", "columnnum": "1", "part_key": "1"}}, nil
			}
			return rows, nil
		})
	}
	t.Run("schema configured with uppercase letters", func(t *testing.T) {
		folded := model
		folded.Schema = types.StringValue("Example_External")
		observed, err := externalPartitionState(t, fullCatalog(), "read", folded, folded)
		require.NoError(t, err)
		assert.Equal(t, model.Location, observed.Location)
	})
	t.Run("same partition through two schemas", func(t *testing.T) {
		row := dataapi.Row{"values": `["2024-01-01"]`, "location": "s3://example-bucket/events/event_date=2024-01-01"}
		observed, err := externalPartitionState(t, partitionRows(row, row), "read", model, model)
		require.NoError(t, err)
		assert.Equal(t, model.Location, observed.Location)
	})
	t.Run("different partitions with the same values", func(t *testing.T) {
		_, err := externalPartitionState(t, partitionRows(
			dataapi.Row{"values": `["2024-01-01"]`, "location": "s3://example-bucket/events/event_date=2024-01-01"},
			dataapi.Row{"values": `["2024-01-01"]`, "location": "s3://other-bucket/events/event_date=2024-01-01"},
		), "read", model, model)
		require.ErrorContains(t, err, "is ambiguous in the catalog")
	})
	t.Run("drifted location is reported", func(t *testing.T) {
		c := fullCatalog()
		externalTableFakeOf(c).partitions[0].location = "s3://example-bucket/elsewhere"
		observed, err := externalPartitionState(t, c, "read", model, model)
		require.NoError(t, err)
		assert.Equal(t, "s3://example-bucket/elsewhere", observed.Location.ValueString())
	})
	t.Run("partition keys changed under the resource", func(t *testing.T) {
		c := fullCatalog()
		externalTableFakeOf(c).partitionKeys = []externalTableFakeColumn{{"day", "date"}}
		r := &externalPartitionResource{}
		configureTestResource(t, r, c)
		state := testState(t, r, model)
		resp := resource.ReadResponse{State: state}
		r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)
		require.False(t, resp.Diagnostics.HasError())
		assert.True(t, resp.State.Raw.IsNull())
	})
	t.Run("update does not converge", func(t *testing.T) {
		c := fullCatalog()
		ignoring := queryFunc(func(ctx context.Context, target dataapi.Connection, sql string, parameters map[string]string) ([]dataapi.Row, error) {
			if strings.Contains(sql, " SET LOCATION ") {
				return nil, nil
			}
			return c.Query(ctx, target, sql, parameters)
		})
		moved := model
		moved.Location = types.StringValue("s3://example-bucket/moved/")
		_, err := externalPartitionState(t, ignoring, "update", model, moved)
		require.ErrorContains(t, err, "after the update")
	})
}

// TestExternalPartitionTranscripts records a multi-key partition, whose keys follow the table's key order.
func TestExternalPartitionTranscripts(t *testing.T) {
	multi := func(c *catalog) {
		table := externalTableFakeOf(c)
		table.partitionKeys = []externalTableFakeColumn{{"salesmonth", "char(10)"}, {"event", "int"}}
		table.partitions = []externalTableFakePartition{{values: []string{"2008-01", "101"}, location: "s3://example-bucket/events/salesmonth=2008-01/event=101"}}
	}
	model := externalPartitionTestModel()
	model.Values = externalTableTestMap("event", "102", "salesmonth", "2008-01")
	model.Location = types.StringValue("s3://example-bucket/events/salesmonth=2008-01/event=102/")
	existing := model
	existing.Values = externalTableTestMap("event", "101", "salesmonth", "2008-01")
	existing.Location = types.StringValue("s3://example-bucket/events/salesmonth=2008-01/event=101/")
	runTranscripts(t, "spectrum/external_partition", newExternalPartitionResource, []transcriptCase{
		{name: "create_multiple_keys", operation: "create", catalog: catalogWith(multi), planned: model},
		{name: "read_multiple_keys", operation: "read", catalog: catalogWith(multi), prior: existing},
		{name: "delete_multiple_keys", operation: "delete", catalog: catalogWith(multi), prior: existing},
	})
}
