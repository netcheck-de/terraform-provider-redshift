package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newUserDataSource, resource: newUserResource, selectors: []string{"name"}})

// TestUserLookup checks non-secret user capability lookup.
func TestUserLookup(t *testing.T) {
	state, diagnostics := readSource(t, newUserDataSource(), userData{Name: types.StringValue("grafana")}, &catalog{user: true, createDB: true})
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	var data userData
	require.False(t, state.Get(context.Background(), &data).HasError())
	assert.False(t, data.Superuser.ValueBool())
	assert.True(t, data.CreateDB.ValueBool())
	assert.Equal(t, types.Int64Value(-1), data.ConnectionLimit)
	assert.Equal(t, types.Int64Value(0), data.SessionTimeout)
	assert.Equal(t, types.StringValue("RESTRICTED"), data.SyslogAccess)
	assert.True(t, data.ValidUntil.IsNull())
	assert.True(t, data.ExternalID.IsNull())
	assert.Nil(t, data.SearchPath)
	assert.Nil(t, data.SessionDefaults)
	assertLookupIdentity(t, data.ID, "admin", map[string]string{"name": "grafana"})
}

// TestUserLookupReportsOptions checks that the lookup reports stored defaults, expiration, and SVV_USER_INFO
// options, and leaves the latter null when the view hides the user.
func TestUserLookupReportsOptions(t *testing.T) {
	c := &catalog{user: true}
	family := fakeState[*userFamily](c, "user")
	family.validUntil, family.connectionLimit, family.sessionTimeout = "2030-01-01 00:00:00+00", "4", "120"
	family.syslog, family.externalID, family.disabled = "UNRESTRICTED", "abc", true
	family.config = []string{`search_path="$user", public`, "statement_timeout=1000"}
	state, diagnostics := readSource(t, newUserDataSource(), userData{Name: types.StringValue("grafana")}, c)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	var data userData
	require.False(t, state.Get(context.Background(), &data).HasError())
	assert.Equal(t, types.StringValue("2030-01-01T00:00:00Z"), data.ValidUntil)
	assert.Equal(t, types.Int64Value(4), data.ConnectionLimit)
	assert.Equal(t, types.Int64Value(120), data.SessionTimeout)
	assert.Equal(t, types.StringValue("UNRESTRICTED"), data.SyslogAccess)
	assert.Equal(t, types.StringValue("abc"), data.ExternalID)
	assert.Equal(t, []string{"$user", "public"}, data.SearchPath)
	assert.Equal(t, map[string]string{"statement_timeout": "1000"}, data.SessionDefaults)

	family.infoHidden = true
	state, diagnostics = readSource(t, newUserDataSource(), userData{Name: types.StringValue("grafana")}, c)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	require.False(t, state.Get(context.Background(), &data).HasError())
	assert.True(t, data.ConnectionLimit.IsNull())
	assert.True(t, data.SyslogAccess.IsNull())
}
