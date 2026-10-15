package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
)

var _ = registerParity(parityCase{source: newDatashareTableDataSource, resource: newDatashareTableResource, selectors: []string{"database", "datashare", "schema", "table"}})

// TestDatashareTableLookup checks explicit relation membership without adding or removing tables.
func TestDatashareTableLookup(t *testing.T) {
	exerciseCatalogLookup(t, newDatashareTableDataSource, map[string]string{"database": "admin", "datashare": "producer", "schema": "serving", "table": "table"}, map[string]attr.Value{"exists": types.BoolValue(true)}, &catalog{shareTable: true})
}

// TestDatashareTableLookupUsesProducerIdentity checks local IDs independently of the provider database.
func TestDatashareTableLookupUsesProducerIdentity(t *testing.T) {
	client := &catalog{shareTable: true, localDB: true}
	fields := map[string]string{"database": "analytics", "datashare": "producer", "schema": "serving", "table": "table"}
	exerciseCatalogLookup(t, newDatashareTableDataSource, fields, map[string]attr.Value{"exists": types.BoolValue(true)}, queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		if !strings.Contains(sql, "svv_redshift_databases") {
			assert.Equal(t, "analytics", target.Database)
		}
		return client.Query(ctx, target, sql, parameters)
	}))
}
