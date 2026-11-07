package provider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newUsersDataSource, resource: newUserResource, collection: true})

// listedItems reads a listing with the given filters and returns its elements, asserting that it only reads.
func listedItems(t *testing.T, factory func() datasource.DataSource, filters map[string]string, client sqlclient.Client) ([]types.Object, diag.Diagnostics) {
	t.Helper()

	source := factory()

	state, diagnostics := readSource(t, source, collectionConfig(t, source, filters), queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		require.True(t, strings.HasPrefix(sql, "SELECT "), "listing must not mutate: %s", sql)
		return client.Query(ctx, target, sql, parameters)
	}))
	if diagnostics.HasError() {
		return nil, diagnostics
	}

	var data types.Object
	require.False(t, state.Get(context.Background(), &data).HasError())

	var items []types.Object
	for _, item := range collectionResult(source, data).Elements() {
		items = append(items, item.(types.Object))
	}

	return items, diagnostics
}

// assertListedAsLookup checks that every listed attribute equals what the singular lookup reports for the object.
func assertListedAsLookup(t *testing.T, item types.Object, lookup func() datasource.DataSource, fields map[string]string, client sqlclient.Client) {
	t.Helper()

	source := lookup()
	state, diagnostics := readSource(t, source, catalogLookupObject(t, source, fields), client)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)

	var observed types.Object
	require.False(t, state.Get(context.Background(), &observed).HasError())

	for name, value := range item.Attributes() {
		assert.True(t, value.Equal(observed.Attributes()[name]), "%s: listed %v, lookup %v", name, value, observed.Attributes()[name])
	}
}

// failingCatalog fails every query, for the error paths of the listings.
var failingCatalog = queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
	return nil, errors.New("catalog unavailable")
})

// TestUsersListing lists users with their options exactly as the user lookup reports them.
func TestUsersListing(t *testing.T) {
	c := &catalog{user: true, createDB: true}
	family := fakeState[*userFamily](c, "user")
	family.validUntil, family.connectionLimit, family.sessionTimeout = "2030-01-01 00:00:00+00", "4", "120"
	family.syslog, family.externalID, family.disabled = "UNRESTRICTED", "abc", true
	family.config = []string{`search_path="$user", public`, "statement_timeout=1000"}

	items, diagnostics := listedItems(t, newUsersDataSource, nil, c)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	require.Len(t, items, 1)
	assert.Equal(t, "grafana", objectString(items[0], "name"))
	assert.Equal(t, types.Int64Value(4), items[0].Attributes()["connection_limit"])
	assert.NotContains(t, items[0].Attributes(), "password_disabled")
	assertListedAsLookup(t, items[0], newUserDataSource, map[string]string{"name": "grafana"}, c)

	// A user without an expiration is reported as null, as the lookup does.
	family.validUntil = ""
	items, diagnostics = listedItems(t, newUsersDataSource, nil, c)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.True(t, items[0].Attributes()["valid_until"].IsNull())
	assertListedAsLookup(t, items[0], newUserDataSource, map[string]string{"name": "grafana"}, c)
}

// TestUsersListingRegularUser reports SVV_USER_INFO options only for the users the view shows, as for a regular user
// who sees only its own row.
func TestUsersListingRegularUser(t *testing.T) {
	client := queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
		if strings.Contains(sql, "FROM svv_user_info") {
			return []sqlclient.Row{{"user_name": "alice", "connection_limit": "UNLIMITED", "syslog_access": "RESTRICTED", "session_timeout": "0", "external_user_id": ""}}, nil
		}

		return []sqlclient.Row{
			{"usename": "alice", "usesuper": "false", "usecreatedb": "false", "valuntil": "infinity", "useconfig": ""},
			{"usename": "bob", "usesuper": "true", "usecreatedb": "true", "valuntil": "", "useconfig": "timezone=UTC"},
		}, nil
	})

	items, diagnostics := listedItems(t, newUsersDataSource, nil, client)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	require.Len(t, items, 2)

	alice, bob := items[0].Attributes(), items[1].Attributes()
	assert.Equal(t, types.Int64Value(-1), alice["connection_limit"])
	assert.Equal(t, types.StringValue("RESTRICTED"), alice["syslog_access"])
	assert.True(t, alice["valid_until"].IsNull())
	assert.Equal(t, types.BoolValue(true), bob["superuser"])

	for _, name := range []string{"connection_limit", "session_timeout", "syslog_access", "external_id"} {
		assert.True(t, bob[name].IsNull(), "%s must be null for a user SVV_USER_INFO hides", name)
	}

	assert.Equal(t, types.MapValueMust(types.StringType, map[string]attr.Value{"timezone": types.StringValue("UTC")}), bob["session_defaults"])
}

// TestUsersListingErrors reports catalog failures and malformed rows.
func TestUsersListingErrors(t *testing.T) {
	malformed := queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
		return []sqlclient.Row{{"usename": "alice", "usesuper": "maybe", "usecreatedb": "false"}}, nil
	})
	infoFails := queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
		if strings.Contains(sql, "svv_user_info") {
			return nil, errors.New("view unavailable")
		}

		return nil, nil
	})

	for name, client := range map[string]sqlclient.Client{"catalog error": failingCatalog, "malformed flag": malformed, "info error": infoFails} {
		t.Run(name, func(t *testing.T) {
			_, diagnostics := listedItems(t, newUsersDataSource, nil, client)
			assert.True(t, diagnostics.HasError())
		})
	}
}
