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

// TestDatashareSchemaLookup reads both membership existence and the actual future-object policy.
func TestDatashareSchemaLookup(t *testing.T) {
	for _, includeNew := range []bool{false, true} {
		exerciseCatalogLookup(t, newDatashareSchemaDataSource, map[string]string{"database": "admin", "datashare": "producer", "schema": "serving"}, map[string]attr.Value{"exists": types.BoolValue(true), "include_new": types.BoolValue(includeNew)}, &catalog{shareSchema: true, includeNew: includeNew})
	}
}

// TestDatashareSchemaLookupUsesProducerIdentity checks local IDs independently of the provider database.
func TestDatashareSchemaLookupUsesProducerIdentity(t *testing.T) {
	client := &catalog{shareSchema: true, localDB: true}
	fields := map[string]string{"database": "analytics", "datashare": "producer", "schema": "serving"}
	expected := map[string]attr.Value{"exists": types.BoolValue(true), "include_new": types.BoolValue(false)}
	exerciseCatalogLookup(t, newDatashareSchemaDataSource, fields, expected, queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		if !strings.Contains(sql, "svv_redshift_databases") {
			assert.Equal(t, "analytics", target.Database)
		}
		return client.Query(ctx, target, sql, parameters)
	}))
}
