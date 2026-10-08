package provider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDatabaseLookup checks local and shared database lookup attributes.
func TestDatabaseLookup(t *testing.T) {
	for _, name := range []string{"warehouse", "analytics"} {
		t.Run(name, func(t *testing.T) {
			state, diagnostics := readSource(t, newDatabaseDataSource(), databaseData{Name: types.StringValue(name)}, &catalog{localDB: true, database: true, permissions: true})
			require.False(t, diagnostics.HasError(), "%v", diagnostics)
			var data databaseData
			require.False(t, state.Get(context.Background(), &data).HasError())
			assert.Equal(t, name, data.Name.ValueString())
			if name == "analytics" {
				assert.Equal(t, "shared", data.DatabaseType.ValueString())
				assert.True(t, data.WithPermissions.ValueBool())
				assert.Equal(t, "source", data.ShareName.ValueString())
				assert.Equal(t, "123456789012", data.ProducerAccount.ValueString())
				assert.Equal(t, shareARN, data.DatashareARN.ValueString())
				assert.JSONEq(t, `{"workgroup_name":"warehouse","database":"admin","name":"analytics","datashare_arn":"`+shareARN+`"}`, data.ID.ValueString())
			} else {
				assert.Equal(t, "local", data.DatabaseType.ValueString())
				assert.True(t, data.ShareName.IsNull())
				assert.True(t, data.DatashareARN.IsNull())
				assert.JSONEq(t, `{"workgroup_name":"warehouse","database":"admin","name":"warehouse"}`, data.ID.ValueString())
			}
		})
	}
}

// TestDatabaseReadableMetadataMatchesResources checks shared catalog decoding and import-compatible IDs.
func TestDatabaseReadableMetadataMatchesResources(t *testing.T) {
	for _, name := range []string{"warehouse", "analytics"} {
		t.Run(name, func(t *testing.T) {
			c := &catalog{localDB: true, database: true, permissions: true}
			value := strings.Replace(shareARN, "eu-central-1", "us-west-2", 1)
			lookup := func(_ context.Context, source shareSource) (string, error) {
				expected, err := parseShare(value)
				require.NoError(t, err)
				assert.Equal(t, expected, source)
				return value, nil
			}
			state, diagnostics := readSource(t, newDatabaseDataSource(), databaseData{Name: types.StringValue(name)}, c, lookup)
			require.False(t, diagnostics.HasError(), "%v", diagnostics)
			var observed databaseData
			require.False(t, state.Get(context.Background(), &observed).HasError())
			r := &databaseResource{testResourceClient(c)}
			managed := databaseModel{Name: types.StringValue(name), WithPermissions: types.BoolValue(false)}
			fields := map[string]string{"name": name}
			if name == "analytics" {
				managed.DatashareARN = types.StringValue(value)
				fields["datashare_arn"] = value
			}
			managed.ID = r.identity("admin", fields)
			found, err := r.read(context.Background(), &managed)
			require.NoError(t, err)
			assert.True(t, found)
			assert.Equal(t, managed, observed)
		})
	}
}

// TestDatabaseLookupRejectsFailedARNDiscovery prevents incomplete or incorrectly inferred ARN outputs.
func TestDatabaseLookupRejectsFailedARNDiscovery(t *testing.T) {
	for _, lookup := range []func(context.Context, shareSource) (string, error){
		nil,
		func(context.Context, shareSource) (string, error) { return "", errors.New("access denied") },
		func(context.Context, shareSource) (string, error) { return "invalid", nil },
		func(context.Context, shareSource) (string, error) {
			return strings.Replace(shareARN, "/source", "/other", 1), nil
		},
	} {
		_, diagnostics := readSource(t, newDatabaseDataSource(), databaseData{Name: types.StringValue("analytics")}, &catalog{database: true}, lookup)
		assert.True(t, diagnostics.HasError())
	}
}

// TestLocalDatabaseLookupClearsShareMetadataWithoutAWS checks local reads never need a metadata client.
func TestLocalDatabaseLookupClearsShareMetadataWithoutAWS(t *testing.T) {
	input := databaseData{Name: types.StringValue("warehouse"), DatashareARN: types.StringValue(shareARN), ShareName: types.StringValue("stale")}
	state, diagnostics := readSource(t, newDatabaseDataSource(), input, &catalog{localDB: true}, nil)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	var observed databaseData
	require.False(t, state.Get(context.Background(), &observed).HasError())
	assert.True(t, observed.DatashareARN.IsNull())
	assert.True(t, observed.ShareName.IsNull())
	assert.True(t, observed.ProducerAccount.IsNull())
	assert.True(t, observed.ProducerNamespace.IsNull())
	assert.False(t, observed.WithPermissions.ValueBool())
}

// TestDatabaseLookupRejectsAmbiguousAndUnboundShares verifies share identity is unambiguous.
func TestDatabaseLookupRejectsAmbiguousAndUnboundShares(t *testing.T) {
	for _, rows := range [][]dataapi.Row{
		{{"database_name": "analytics", "database_type": "external"}},
		{{"database_name": "analytics", "database_type": "shared"}},
		{{"database_name": "analytics", "database_type": "local"}, {"database_name": "analytics", "database_type": "local"}},
	} {
		_, diagnostics := readSource(t, newDatabaseDataSource(), databaseData{Name: types.StringValue("analytics")}, queryFunc(func(context.Context, dataapi.Connection, string, map[string]string) ([]dataapi.Row, error) {
			return rows, nil
		}))
		assert.True(t, diagnostics.HasError())
	}
}
