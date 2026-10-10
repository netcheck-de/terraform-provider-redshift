package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newDatashareGrantDataSource, resource: newDatashareGrantResource, selectors: []string{"database", "datashare", "account_id", "namespace_id", "via_data_catalog"}})

// TestDatashareGrantLookupViaDataCatalog reports the Data Catalog form with the resource's identity, which records
// the flag only when it is set.
func TestDatashareGrantLookupViaDataCatalog(t *testing.T) {
	for _, via := range []types.Bool{types.BoolValue(true), types.BoolValue(false), types.BoolNull()} {
		source := newDatashareGrantDataSource()
		data := catalogLookupObject(t, source, map[string]string{"database": "admin", "datashare": "producer", "account_id": "123456789012"})
		lookupValue(&data, "via_data_catalog", via)
		state, diagnostics := readSource(t, source, data, &catalog{shareGrant: true})
		require.False(t, diagnostics.HasError(), "%v", diagnostics)
		var observed types.Object
		require.False(t, state.Get(context.Background(), &observed).HasError())
		assert.Equal(t, types.BoolValue(true), observed.Attributes()["exists"])
		fields := map[string]string{"datashare": "producer", "account_id": "123456789012"}
		if via.ValueBool() {
			fields["via_data_catalog"] = "true"
		}
		assertLookupIdentity(t, observed.Attributes()["id"].(types.String), "admin", fields)
	}
	source := newDatashareGrantDataSource()
	data := catalogLookupObject(t, source, map[string]string{"database": "admin", "datashare": "producer", "namespace_id": "12345678-1234-1234-1234-123456789abc"})
	lookupValue(&data, "via_data_catalog", types.BoolValue(true))
	_, diagnostics := readSource(t, source, data, &catalog{shareNamespaceGrant: true})
	assert.True(t, diagnostics.HasError(), "a namespace has no Data Catalog form")
}

// TestDatashareGrantLookup observes account usage without modifying sharing authorization.
func TestDatashareGrantLookup(t *testing.T) {
	exerciseCatalogLookup(t, newDatashareGrantDataSource, map[string]string{"database": "admin", "datashare": "producer", "account_id": "123456789012"}, map[string]attr.Value{"exists": types.BoolValue(true)}, &catalog{shareGrant: true})
}

// TestDatashareNamespaceGrantLookup checks namespace observations, null IDs on absence, and read-only API errors.
func TestDatashareNamespaceGrantLookup(t *testing.T) {
	exerciseCatalogLookup(t, newDatashareGrantDataSource, map[string]string{"database": "admin", "datashare": "producer", "namespace_id": "12345678-1234-1234-1234-123456789abc"}, map[string]attr.Value{"exists": types.BoolValue(true)}, &catalog{shareNamespaceGrant: true, shareGrant: true})
}

// TestDatashareGrantLookupRejectsAmbiguousConsumer ensures read-only lookup rejects competing selectors.
func TestDatashareGrantLookupRejectsAmbiguousConsumer(t *testing.T) {
	source := newDatashareGrantDataSource()
	data := catalogLookupObject(t, source, map[string]string{"database": "admin", "datashare": "producer", "account_id": "123456789012", "namespace_id": "12345678-1234-1234-1234-123456789abc"})
	_, diagnostics := readSource(t, source, data, queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
		t.Fatal("ambiguous consumer reached SQL")
		return nil, nil
	}))
	assert.True(t, diagnostics.HasError())
}

// TestDatashareGrantLookupUsesProducerIdentity checks local IDs independently of the provider database.
func TestDatashareGrantLookupUsesProducerIdentity(t *testing.T) {
	client := &catalog{shareGrant: true, localDB: true}
	fields := map[string]string{"database": "analytics", "datashare": "producer", "account_id": "123456789012"}
	exerciseCatalogLookup(t, newDatashareGrantDataSource, fields, map[string]attr.Value{"exists": types.BoolValue(true)}, queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		if !strings.Contains(sql, "svv_redshift_databases") {
			assert.Equal(t, "analytics", target.Database)
		}
		return client.Query(ctx, target, sql, parameters)
	}))
}
