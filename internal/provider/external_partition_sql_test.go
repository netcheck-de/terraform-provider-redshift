package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// externalPartitionTestModel is the representative partition the lifecycle cases and the fake catalog share.
func externalPartitionTestModel() externalPartitionModel {
	return externalPartitionModel{
		ID: types.StringNull(), Database: types.StringValue("admin"), Schema: types.StringValue("example_external"), Table: types.StringValue("events"),
		Values:   externalTableTestMap("event_date", "2024-01-01"),
		Location: types.StringValue("s3://example-bucket/events/event_date=2024-01-01/"),
	}
}

// TestExternalPartitionSQL pins the partition statements, with keys in partition-key order and quoting edge cases.
func TestExternalPartitionSQL(t *testing.T) {
	spec := func(data externalPartitionModel, keys ...string) func() (externalPartitionSpec, error) {
		return func() (externalPartitionSpec, error) { return externalPartitionSpecFrom(data, keys) }
	}
	render := func(build func() (externalPartitionSpec, error), statement func(externalPartitionSpec) string) func() (string, error) {
		return func() (string, error) {
			built, err := build()
			if err != nil {
				return "", err
			}
			return statement(built), nil
		}
	}
	plain := spec(externalPartitionTestModel(), "event_date")
	multi := externalPartitionTestModel()
	multi.Values = externalTableTestMap("Event", "101", "salesmonth", "2008-01")
	quoted := externalPartitionTestModel()
	quoted.Schema, quoted.Table = types.StringValue(`Lake"Schema`), types.StringValue(`Odd"Table`)
	quoted.Values = externalTableTestMap(`Part"Key`, `it's \here`)
	quoted.Location = types.StringValue(`s3://bucket/it's \part/`)
	moved := func(build func() (externalPartitionSpec, error), location string) func() ([]string, error) {
		return func() ([]string, error) {
			prev, err := build()
			if err != nil {
				return nil, err
			}
			next := prev
			next.location = location
			return alterExternalPartitionStatements(prev, next), nil
		}
	}
	errorCase := func(data externalPartitionModel, keys ...string) func() (string, error) {
		return render(spec(data, keys...), createExternalPartitionStatement)
	}
	missing := externalPartitionTestModel()
	missing.Values = externalTableTestMap("other", "x")
	extra := externalPartitionTestModel()
	extra.Values = externalTableTestMap("event_date", "2024-01-01", "other", "x")
	collision := externalPartitionTestModel()
	collision.Values = externalTableTestMap("event_date", "a", "EVENT_DATE", "b")
	noValues := externalPartitionTestModel()
	noValues.Values = externalTableTestMap()
	checkSQL(t, "external_partition", []sqlCase{
		{"create", render(plain, createExternalPartitionStatement)},
		{"create_multiple_keys_in_key_order", render(spec(multi, "salesmonth", "event"), createExternalPartitionStatement)},
		{"create_quoted", render(spec(quoted, `part"key`), createExternalPartitionStatement)},
		{"alter_location", moved(plain, "s3://example-bucket/events/2024/01/01/")},
		{"alter_unchanged", moved(plain, "s3://example-bucket/events/event_date=2024-01-01/")},
		{"alter_quoted", moved(spec(quoted, `part"key`), `s3://bucket/it's \moved/`)},
		{"drop", render(plain, dropExternalPartitionStatement)},
		{"drop_quoted", render(spec(quoted, `part"key`), dropExternalPartitionStatement)},
		{"error_missing_key", errorCase(missing, "event_date")},
		{"error_extra_key", errorCase(extra, "event_date")},
		{"error_case_collision", errorCase(collision, "event_date")},
		{"error_no_values", errorCase(noValues, "event_date")},
		{"error_unpartitioned_table", errorCase(externalPartitionTestModel())},
		{"read", func() (string, error) {
			built, err := spec(quoted, `part"key`)()
			require.NoError(t, err)
			sql, parameters, err := readExternalPartitionsQuery(built).Build()
			assert.Equal(t, map[string]string{"schema": `Lake"Schema`, "table": `Odd"Table`}, parameters)
			return sql, err
		}},
	})
}

// TestExternalPartitionAlterCoverage keeps a step for the only in-place attribute.
func TestExternalPartitionAlterCoverage(t *testing.T) {
	assertAlterCoverage(t, newExternalPartitionResource(), externalPartitionAlterSteps)
}

// TestExternalPartitionValuesMatch compares catalog arrays in key order and rejects malformed ones.
func TestExternalPartitionValuesMatch(t *testing.T) {
	spec := externalPartitionSpec{values: []string{"2008-01", "101"}}
	match, err := externalPartitionValuesMatch(spec, `["2008-01","101"]`)
	require.NoError(t, err)
	assert.True(t, match)
	match, err = externalPartitionValuesMatch(spec, `["101", "2008-01"]`)
	require.NoError(t, err)
	assert.False(t, match)
	_, err = externalPartitionValuesMatch(spec, `2008-01`)
	require.Error(t, err)
	assert.JSONEq(t, `{"a":"1","b":"2"}`, externalPartitionIDValues(map[string]string{"b": "2", "a": "1"}))
	require.Error(t, validateExternalPartitionConfig(externalPartitionModel{Schema: types.StringValue("s"), Table: types.StringValue(""), Values: externalTableTestMap("a", "1"), Location: types.StringValue("s3://b/")}))
	require.Error(t, validateExternalPartitionValues(map[string]string{" ": "x"}))
}
