package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
)

var _ = registerParity(parityCase{source: newDatasharePrivilegesDataSource, resource: newDatasharePrivilegeResource, collection: true, filters: []string{"datashare_name", "grantee", "grantee_type"}})

// datasharePrivilegeItem is one expected element on the representative share.
func datasharePrivilegeItem(granteeType, grantee string, privileges ...string) map[string]attr.Value {
	values := make([]attr.Value, 0, len(privileges))
	for _, privilege := range privileges {
		values = append(values, types.StringValue(privilege))
	}

	return map[string]attr.Value{
		"database_name": types.StringValue("admin"), "datashare_name": types.StringValue("producer"), "grantee": types.StringValue(grantee),
		"grantee_type": types.StringValue(granteeType), "privileges": types.SetValueMust(types.StringType, values),
	}
}

// TestDatasharePrivilegesList groups permissions per share and grantee, names PUBLIC public, and filters by share,
// grantee, and grantee type.
func TestDatasharePrivilegesList(t *testing.T) {
	for _, test := range []struct {
		name     string
		filters  map[string]string
		expected []map[string]attr.Value
	}{
		{"all", nil, []map[string]attr.Value{
			datasharePrivilegeItem("PUBLIC", "public", "SHARE"),
			datasharePrivilegeItem("ROLE", "example:readers", "ALTER", "SHARE"),
			datasharePrivilegeItem("USER", "grafana", "ALTER"),
		}},
		{"grantee", map[string]string{"datashare_name": "producer", "grantee": "example:readers"}, []map[string]attr.Value{
			datasharePrivilegeItem("ROLE", "example:readers", "ALTER", "SHARE"),
		}},
		{"public", map[string]string{"grantee": "public"}, []map[string]attr.Value{datasharePrivilegeItem("PUBLIC", "public", "SHARE")}},
		{"grantee_type", map[string]string{"grantee_type": "USER"}, []map[string]attr.Value{datasharePrivilegeItem("USER", "grafana", "ALTER")}},
		{"other_grantee_type", map[string]string{"grantee": "grafana", "grantee_type": "ROLE"}, nil},
		{"missing_share", map[string]string{"datashare_name": "missing"}, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := fullCatalog()
			fake := fakeState[*fakeDatashares](c, "datashare")
			fake.privileges["role:example:readers"]["SHARE"] = true
			fake.privileges["user:grafana"] = map[string]bool{"ALTER": true}
			fake.privileges["public"] = map[string]bool{"SHARE": true}
			id, items := readListing(t, newDatasharePrivilegesDataSource(), test.filters, c)
			assertLookupIdentity(t, id, test.filters)
			assert.Equal(t, test.expected, items)
		})
	}
}

// TestDatasharePrivilegesListMatchesLookup reports the permissions the lookup observes for the same grantee.
func TestDatasharePrivilegesListMatchesLookup(t *testing.T) {
	_, items := readListing(t, newDatasharePrivilegesDataSource(), nil, fullCatalog())
	assert.Equal(t, []map[string]attr.Value{datasharePrivilegeItem("ROLE", "example:readers", "ALTER")}, items)
	exerciseCatalogLookup(t, newDatasharePrivilegeDataSource, map[string]string{"database_name": "admin", "datashare_name": "producer", "grantee_type": "ROLE", "grantee": "example:readers"}, map[string]attr.Value{
		"privileges": items[0]["privileges"],
	}, fullCatalog())
}

// TestDatasharePrivilegesListFailure reports a catalog error instead of an empty listing.
func TestDatasharePrivilegesListFailure(t *testing.T) {
	source := newDatasharePrivilegesDataSource()
	_, diagnostics := readSource(t, source, collectionConfig(t, source, nil), queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
		return nil, assert.AnError
	}))
	assert.True(t, diagnostics.HasError())
}

// TestDatasharePrivilegeListSQL pins the permission listing for each filter combination; PUBLIC is matched by type.
func TestDatasharePrivilegeListSQL(t *testing.T) {
	built := func(share, grantee string) func() (string, error) {
		return func() (string, error) {
			sql, _, err := listDatasharePrivilegesQuery(share, grantee).Build()
			return sql, err
		}
	}
	checkSQL(t, "datashare_privileges", []sqlCase{
		{"all", built("", "")},
		{"share", built(`Odd"Share`, "")},
		{"grantee", built("producer", "example:readers")},
		{"public", built("", "PUBLIC")},
	})
}
