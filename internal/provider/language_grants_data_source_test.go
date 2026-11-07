package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
)

var _ = registerParity(parityCase{source: newLanguageGrantsDataSource, resource: newLanguageGrantResource, collection: true, filters: []string{"database_name", "grantee", "grantee_type"}})

// TestLanguageGrantsSQL pins the language privilege listing with and without the grantee filter.
func TestLanguageGrantsSQL(t *testing.T) {
	render := func(grantee string) func() (string, error) {
		return func() (string, error) {
			sql, _, err := listLanguageGrantsQuery(grantee).Build()
			return sql, err
		}
	}
	checkSQL(t, "language_grants", []sqlCase{{"unfiltered", render("")}, {"grantee", render("analyst")}})
}

// TestLanguageGrantsListing lists the fake's language grants in the selected database, one element per language and
// grantee, with grant options and canonical language names, narrowed by grantee and grantee type.
func TestLanguageGrantsListing(t *testing.T) {
	c := fullCatalog()
	fake := fakeState[*languageGrantFake](c, "language_grant")
	fake.usage[languageGrantFakeKey{"sql", "analyst", "user"}] = true

	var database string

	client := queryFunc(func(ctx context.Context, connection sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		database = connection.Database
		return c.Query(ctx, connection, sql, parameters)
	})

	items := listingItems(t, newLanguageGrantsDataSource(), map[string]string{"database_name": "analytics"}, client)
	assert.Equal(t, "analytics", database)

	if assert.Len(t, items, 2) {
		assert.Equal(t, types.StringValue("PLPGSQL"), items[0]["language_name"])
		assert.Equal(t, types.StringValue("ROLE"), items[0]["grantee_type"])
		assert.Equal(t, stringSet(), items[0]["grant_option_privileges"])
		assert.Equal(t, types.StringValue("SQL"), items[1]["language_name"])
		assert.Equal(t, types.StringValue("analyst"), items[1]["grantee"])
		assert.Equal(t, types.StringValue("analytics"), items[1]["database_name"])
		assert.Equal(t, stringSet("USAGE"), items[1]["privileges"])
		assert.Equal(t, stringSet("USAGE"), items[1]["grant_option_privileges"])
	}

	items = listingItems(t, newLanguageGrantsDataSource(), map[string]string{"grantee": "analyst"}, client)
	assert.Equal(t, "admin", database)
	assert.Len(t, items, 1)

	for kind, language := range map[string]string{"USER": "SQL", "ROLE": "PLPGSQL"} {
		items = listingItems(t, newLanguageGrantsDataSource(), map[string]string{"grantee_type": kind}, client)
		assert.Equal(t, []string{language}, discoveryNames(items, "language_name"), "only the grants to %s identities", kind)
	}

	assertListingFails(t, newLanguageGrantsDataSource(), nil, nil)
}
