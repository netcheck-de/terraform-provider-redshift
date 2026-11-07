package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
)

var _ = registerParity(parityCase{source: newDatashareGrantsDataSource, resource: newDatashareGrantResource, collection: true, filters: []string{"datashare"}})

// TestDatashareGrantsList reports an account grant without a namespace and a namespace grant without its account,
// as the resource configures them, and filters by share.
func TestDatashareGrantsList(t *testing.T) {
	grant := func(account, namespace types.String) map[string]attr.Value {
		return map[string]attr.Value{"database": types.StringValue("admin"), "datashare": types.StringValue("producer"), "account_id": account, "namespace_id": namespace}
	}

	for _, test := range []struct {
		name     string
		filters  map[string]string
		expected []map[string]attr.Value
	}{
		{"all", nil, []map[string]attr.Value{
			grant(types.StringValue("123456789012"), types.StringNull()),
			grant(types.StringNull(), types.StringValue("12345678-1234-1234-1234-123456789abc")),
		}},
		{"share", map[string]string{"datashare": "producer"}, []map[string]attr.Value{
			grant(types.StringValue("123456789012"), types.StringNull()),
			grant(types.StringNull(), types.StringValue("12345678-1234-1234-1234-123456789abc")),
		}},
		{"missing_share", map[string]string{"datashare": "missing"}, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := fullCatalog()
			c.shareNamespaceGrant = true
			id, items := readListing(t, newDatashareGrantsDataSource(), test.filters, c)
			assertLookupIdentity(t, id, test.filters)
			assert.Equal(t, test.expected, items)
		})
	}
}

// TestDatashareGrantsListFailure reports a catalog error instead of an empty listing.
func TestDatashareGrantsListFailure(t *testing.T) {
	source := newDatashareGrantsDataSource()
	_, diagnostics := readSource(t, source, collectionConfig(t, source, nil), failingCatalog)
	assert.True(t, diagnostics.HasError())
}

// TestDatashareGrantListSQL pins the consumer listing with and without a share filter.
func TestDatashareGrantListSQL(t *testing.T) {
	built := func(share string) func() (string, error) {
		return func() (string, error) {
			sql, _, err := listDatashareGrantsQuery(share).Build()
			return sql, err
		}
	}
	checkSQL(t, "datashare_grants", []sqlCase{{"all", built("")}, {"share", built(`Odd"Share`)}})
}
