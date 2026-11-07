package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
)

var _ = registerParity(parityCase{source: newSystemGrantsDataSource, resource: newSystemGrantResource, collection: true, filters: []string{"role"}})

// TestSystemGrantsSQL pins the system privilege listing with and without the role filter.
func TestSystemGrantsSQL(t *testing.T) {
	render := func(role string) func() (string, error) {
		return func() (string, error) {
			sql, _, err := listSystemGrantsQuery(role).Build()
			return sql, err
		}
	}
	checkSQL(t, "system_grants", []sqlCase{{"unfiltered", render("")}, {"role", render("operators")}})
}

// TestSystemGrantsListing groups each role's system permissions into one element and binds the role filter.
func TestSystemGrantsListing(t *testing.T) {
	var parameters map[string]string

	client := queryFunc(func(_ context.Context, connection sqlclient.Connection, _ string, params map[string]string) ([]sqlclient.Row, error) {
		assert.Equal(t, "admin", connection.Database)

		parameters = params

		return []sqlclient.Row{
			{"identity_name": "auditors", "privilege_type": "ACCESS SYSTEM TABLE"},
			{"identity_name": "operators", "privilege_type": "CREATE ROLE"},
			{"identity_name": "operators", "privilege_type": "CREATE USER"},
		}, nil
	})

	items := listingItems(t, newSystemGrantsDataSource(), nil, client)
	assert.Empty(t, parameters)

	if assert.Len(t, items, 2) {
		assert.Equal(t, types.StringValue("auditors"), items[0]["role"])
		assert.Equal(t, stringSet("CREATE ROLE", "CREATE USER"), items[1]["privileges"])
	}

	listingItems(t, newSystemGrantsDataSource(), map[string]string{"role": "operators"}, client)
	assert.Equal(t, map[string]string{"role": "operators"}, parameters)

	assertListingFails(t, newSystemGrantsDataSource(), nil, nil)
}
