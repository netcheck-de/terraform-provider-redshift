package provider

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	_ = registerParity(parityCase{source: newRlsPolicyDataSource, resource: newRlsPolicyResource, selectors: []string{"database", "name"}})
	_ = registerParity(parityCase{source: newRlsPoliciesDataSource, resource: newRlsPolicyResource, collection: true, filters: []string{"database"}})
	_ = registerParity(parityCase{source: newRlsPolicyAttachmentDataSource, resource: newRlsPolicyAttachmentResource, selectors: []string{"policy", "database", "schema", "relation", "grantee", "grantee_type"}})
	_ = registerParity(parityCase{source: newTableSecurityDataSource, resource: newTableSecurityResource, selectors: []string{"database", "schema", "relation"}})
)

// TestRlsPolicyLookup observes the catalog definition, absence, and failures without writing.
func TestRlsPolicyLookup(t *testing.T) {
	exerciseCatalogLookup(t, newRlsPolicyDataSource, map[string]string{"database": "admin", "name": "region_filter"}, map[string]attr.Value{
		"columns":                rlsPolicyColumnsValue("region", "character varying(64)"),
		"alias":                  types.StringNull(),
		"predicate":              types.StringValue(rlsFakePredicate),
		"definition_fingerprint": types.StringValue(definitionFingerprint(rlsFakePredicate)),
	}, fullCatalog())
}

// TestRlsPolicyAttachmentLookup reports an existing attachment and exists = false for a missing one.
func TestRlsPolicyAttachmentLookup(t *testing.T) {
	exerciseCatalogLookup(t, newRlsPolicyAttachmentDataSource, map[string]string{
		"database": "admin", "policy": "region_filter", "schema": "public", "relation": "events", "grantee": "analysts", "grantee_type": "ROLE",
	}, map[string]attr.Value{"exists": types.BoolValue(true)}, fullCatalog())
}

// TestTableSecurityLookup observes the relation's settings and reports a missing relation as an error.
func TestTableSecurityLookup(t *testing.T) {
	c := fullCatalog()
	rlsFakeOf(c).conjunction = "or"
	exerciseCatalogLookup(t, newTableSecurityDataSource, map[string]string{"database": "admin", "schema": "public", "relation": "events"}, map[string]attr.Value{
		"row_level_security":           types.BoolValue(true),
		"conjunction_type":             types.StringValue("OR"),
		"datashare_row_level_security": types.BoolValue(true),
	}, c)
}

// TestRlsPoliciesListing lists every policy of the selected database and binds the identity to it.
func TestRlsPoliciesListing(t *testing.T) {
	var target sqlclient.Connection
	var parameters map[string]string
	rows := []sqlclient.Row{
		{"poldb": "analytics", "polname": "open", "polalias": "", "polatts": "[]", "polqual": "true"},
		{"poldb": "analytics", "polname": "region_filter", "polalias": "t", "polatts": rlsFakeColumns, "polqual": `"t"."region" = current_user`},
	}
	client := queryFunc(func(_ context.Context, connection sqlclient.Connection, sql string, params map[string]string) ([]sqlclient.Row, error) {
		require.Contains(t, sql, "FROM svv_rls_policy")
		target, parameters = connection, params
		return rows, nil
	})
	source := newRlsPoliciesDataSource()
	state, diagnostics := readSource(t, source, collectionConfig(t, source, map[string]string{"database": "analytics"}), client)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.Equal(t, "analytics", target.Database)
	assert.Equal(t, map[string]string{"database": "analytics"}, parameters)
	var observed types.Object
	require.False(t, state.Get(context.Background(), &observed).HasError())
	var identity map[string]string
	require.NoError(t, json.Unmarshal([]byte(observed.Attributes()["id"].(types.String).ValueString()), &identity))
	assert.Equal(t, map[string]string{"workgroup_name": "warehouse", "database": "analytics"}, identity)
	items := observed.Attributes()[collectionItems].(types.List).Elements()
	require.Len(t, items, 2)
	open, filtered := items[0].(types.Object).Attributes(), items[1].(types.Object).Attributes()
	assert.Equal(t, types.StringValue("open"), open["name"])
	assert.True(t, open["columns"].IsNull())
	assert.True(t, open["alias"].IsNull())
	assert.Equal(t, rlsPolicyColumnsValue("region", "character varying(64)"), filtered["columns"])
	assert.Equal(t, types.StringValue("t"), filtered["alias"])
	assert.Equal(t, types.StringValue(definitionFingerprint(`"t"."region" = current_user`)), filtered["definition_fingerprint"])

	// The provider database is the default, and an empty database is an empty list.
	rows = nil
	state, diagnostics = readSource(t, source, collectionConfig(t, source, nil), client)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.Equal(t, "admin", target.Database)
	require.False(t, state.Get(context.Background(), &observed).HasError())
	assert.Empty(t, observed.Attributes()[collectionItems].(types.List).Elements())

	for _, failure := range []func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error){
		func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
			return nil, errors.New("catalog unavailable")
		},
		func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
			return []sqlclient.Row{{"poldb": "admin", "polname": "broken", "polatts": "{"}}, nil
		},
	} {
		_, diagnostics = readSource(t, source, collectionConfig(t, source, nil), queryFunc(failure))
		assert.True(t, diagnostics.HasError())
	}
}
