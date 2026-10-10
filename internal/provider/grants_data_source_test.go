package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newGrantsDataSource, collection: true, filters: []string{"database_name", "object_type", "schema_name", "object_name", "grantee", "grantee_type"}})

// TestGrantsListing reads SHOW GRANTS in the selected database and reports invalid selections as errors.
func TestGrantsListing(t *testing.T) {
	var database, statement string
	client := queryFunc(func(_ context.Context, connection sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		database, statement = connection.Database, sql
		assert.Empty(t, parameters, "SHOW GRANTS takes no bindings")
		return []sqlclient.Row{
			{"schema_name": "serving", "object_name": "events", "object_type": "TABLE", "privilege_type": "SELECT", "identity_name": "readers", "identity_type": "group", "admin_option": "f", "privilege_scope": "TABLE", "database_name": "warehouse", "grantor_name": "admin"},
			{"schema_name": "serving", "object_name": "events", "object_type": "TABLE", "privilege_type": "SELECT", "identity_name": "analyst", "identity_type": "user", "admin_option": "t", "privilege_scope": "TABLE", "database_name": "warehouse", "grantor_name": "admin"},
		}, nil
	})
	for _, test := range []struct {
		name, database, statement string
		filters                   map[string]string
		items                     int
	}{
		{"table", "warehouse", `SHOW GRANTS ON TABLE "warehouse"."serving"."events"`, map[string]string{"database_name": "warehouse", "object_type": "TABLE", "schema_name": "serving", "object_name": "events"}, 2},
		{"table group", "warehouse", `SHOW GRANTS ON TABLE "warehouse"."serving"."events"`, map[string]string{"database_name": "warehouse", "object_type": "TABLE", "schema_name": "serving", "object_name": "events", "grantee_type": "GROUP"}, 1},
		{"user", "admin", `SHOW GRANTS FOR "analyst" FROM DATABASE "admin"`, map[string]string{"grantee": "analyst", "grantee_type": "USER"}, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := newGrantsDataSource()
			state, diagnostics := readSource(t, source, collectionConfig(t, source, test.filters), client)
			require.False(t, diagnostics.HasError(), "%v", diagnostics)
			assert.Equal(t, test.database, database)
			assert.Equal(t, test.statement, statement)
			var observed types.Object
			require.False(t, state.Get(context.Background(), &observed).HasError())
			assert.Len(t, observed.Attributes()[collectionItems].(types.List).Elements(), test.items)
		})
	}
	for name, filters := range map[string]map[string]string{
		"nothing selected": nil,
		"group identity":   {"grantee": "readers", "grantee_type": "GROUP"},
	} {
		t.Run(name, func(t *testing.T) {
			statement = ""
			source := newGrantsDataSource()
			_, diagnostics := readSource(t, source, collectionConfig(t, source, filters), client)
			assert.True(t, diagnostics.HasError())
			assert.Empty(t, statement, "invalid selections never reach SQL")
		})
	}
	source := newGrantsDataSource()
	_, diagnostics := readSource(t, source, collectionConfig(t, source, map[string]string{"object_type": "DATABASE"}), queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
		return nil, errors.New("catalog unavailable")
	}))
	assert.True(t, diagnostics.HasError(), "catalog failures are errors")
}
