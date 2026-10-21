package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCoverageExternalPartitionValidation(t *testing.T) {
	model := externalPartitionTestModel()
	model.Schema = types.StringValue("")
	require.ErrorContains(t, validateExternalPartitionKnownConfig(model), "nonempty schema")
	_, err := externalPartitionSpecFrom(model, []string{"event_date"})
	require.ErrorContains(t, err, "nonempty schema")
	model = externalPartitionTestModel()
	model.Values = types.MapNull(types.StringType)
	require.ErrorContains(t, validateExternalPartitionConfig(model), "at least one")
	assert.False(t, externalPartitionKeysMatch(nil, nil))
	assert.False(t, externalPartitionKeysMatch(map[string]string{"day": "1"}, []string{"day", "hour"}))
}

func TestCoverageExternalPartitionVerification(t *testing.T) {
	for _, test := range []struct {
		name, operation, want string
		absent                bool
	}{
		{"create absent", "create", "absent after adding", true},
		{"create wrong location", "create", "after adding the partition", false},
		{"update disappeared", "update", "disappeared during the update", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := fullCatalog()
			model := externalPartitionTestModel()
			if test.operation == "create" {
				externalTableFakeOf(c).partitions = nil
			}
			client := queryFunc(func(ctx context.Context, connection sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
				if strings.Contains(sql, " ADD PARTITION ") || strings.Contains(sql, " SET LOCATION ") {
					if test.absent {
						externalTableFakeOf(c).partitions = nil
						return nil, nil
					}
					rows, err := c.Query(ctx, connection, sql, parameters)
					externalTableFakeOf(c).partitions[0].location = "s3://bucket/wrong"
					return rows, err
				}
				return c.Query(ctx, connection, sql, parameters)
			})
			plan := model
			if test.operation == "update" {
				plan.Location = types.StringValue("s3://bucket/moved")
			}
			_, err := externalPartitionState(t, client, test.operation, model, plan)
			require.ErrorContains(t, err, test.want)
		})
	}
}

func TestCoverageExternalPartitionMalformedCatalog(t *testing.T) {
	r := &externalPartitionResource{}
	configureTestResource(t, r, queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
		return []sqlclient.Row{{"columnname": "day", "external_type": "date", "columnnum": "invalid", "part_key": "1"}}, nil
	}))
	_, err := r.partitionKeys(context.Background(), externalPartitionTestModel())
	require.Error(t, err)
	r = &externalPartitionResource{}
	configureTestResource(t, r, fullCatalog())
	model := externalPartitionTestModel()
	model.Schema = types.StringValue("")
	_, found, err := r.read(context.Background(), &model)
	require.ErrorContains(t, err, "parameter :schema is empty")
	assert.False(t, found)
}

func TestCoverageExternalPartitionLookupFailure(t *testing.T) {
	_, diagnostics := readSource(t, newExternalPartitionDataSource(), externalPartitionTestModel(), queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
		return []sqlclient.Row{{"columnname": "day", "external_type": "date", "columnnum": "invalid", "part_key": "1"}}, nil
	}))
	require.True(t, diagnostics.HasError())
	assert.Equal(t, "Read external partition", diagnostics.Errors()[0].Summary())
}
