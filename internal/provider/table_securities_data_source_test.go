package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newTableSecuritiesDataSource, resource: newTableSecurityResource, collection: true, filters: []string{"database", "schema"}})

// tableSecurityListing reads a listing and returns its elements' attributes.
func tableSecurityListing(t *testing.T, filters map[string]string, client sqlclient.Client) []map[string]attr.Value {
	t.Helper()

	source := newTableSecuritiesDataSource()
	state, diagnostics := readSource(t, source, collectionConfig(t, source, filters), client)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)

	var data types.Object
	require.False(t, state.Get(context.Background(), &data).HasError())

	var items []map[string]attr.Value
	for _, element := range collectionResult(source, data).Elements() {
		items = append(items, element.(types.Object).Attributes())
	}

	return items
}

// TestTableSecuritiesListing lists the settings the lookup reports for the same relation, narrows them by schema,
// and reports catalog failures and malformed rows.
func TestTableSecuritiesListing(t *testing.T) {
	c := fullCatalog()
	rlsFakeOf(c).conjunction = "or"
	expected := map[string]attr.Value{
		"database": types.StringValue("admin"), "schema": types.StringValue("public"), "relation": types.StringValue("events"),
		"row_level_security": types.BoolValue(true), "conjunction_type": types.StringValue("OR"), "datashare_row_level_security": types.BoolValue(true),
	}

	for _, filters := range []map[string]string{nil, {"database": "admin", "schema": "public"}} {
		assert.Equal(t, []map[string]attr.Value{expected}, tableSecurityListing(t, filters, c))
	}

	assert.Empty(t, tableSecurityListing(t, map[string]string{"schema": "other"}, c))

	var (
		target     sqlclient.Connection
		parameters map[string]string
	)

	client := queryFunc(func(_ context.Context, connection sqlclient.Connection, sql string, params map[string]string) ([]sqlclient.Row, error) {
		require.Contains(t, sql, "FROM svv_rls_relation")

		target, parameters = connection, params

		return []sqlclient.Row{{"relschema": "sales", "relname": "orders", "is_rls_on": "false", "is_rls_datashare_on": "false", "rls_conjunction_type": ""}}, nil
	})
	items := tableSecurityListing(t, map[string]string{"database": "analytics"}, client)
	assert.Equal(t, "analytics", target.Database)
	assert.Equal(t, map[string]string{"database": "analytics"}, parameters, "the view is filtered by the listed database")
	require.Len(t, items, 1)
	assert.Equal(t, types.BoolValue(false), items[0]["row_level_security"])
	assert.Equal(t, types.StringValue("AND"), items[0]["conjunction_type"], "an empty conjunction type reports the default")
	assert.Equal(t, types.BoolValue(false), items[0]["datashare_row_level_security"])

	source := newTableSecuritiesDataSource()
	for _, failure := range []queryFunc{
		func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
			return nil, errors.New("catalog unavailable")
		},
		func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
			return []sqlclient.Row{{"relschema": "public", "relname": "events", "is_rls_on": "maybe", "is_rls_datashare_on": "true"}}, nil
		},
	} {
		_, diagnostics := readSource(t, source, collectionConfig(t, source, nil), failure)
		assert.True(t, diagnostics.HasError())
	}
}
