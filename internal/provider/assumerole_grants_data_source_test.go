package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
)

var _ = registerParity(parityCase{source: newAssumeroleGrantsDataSource, resource: newAssumeroleGrantResource, collection: true, filters: []string{"iam_role_arn", "grantee"}})

// TestAssumeroleGrantsSQL pins the IAM privilege listing with and without each filter.
func TestAssumeroleGrantsSQL(t *testing.T) {
	render := func(role, grantee string) func() (string, error) {
		return func() (string, error) {
			sql, _, err := listAssumeroleGrantsQuery(role, grantee).Build()
			return sql, err
		}
	}
	checkSQL(t, "assumerole_grants", []sqlCase{{"unfiltered", render("", "")}, {"all_filters", render(assumeroleGrantDefaultRole, "readers")}})
}

// TestAssumeroleGrantsListing groups commands per IAM role and grantee, maps the catalog's role markers to the
// resource's selectors, and translates the iam_role_arn filter to the catalog name.
func TestAssumeroleGrantsListing(t *testing.T) {
	const arn = "arn:aws:iam::123456789012:role/loader"

	var parameters map[string]string

	client := queryFunc(func(_ context.Context, _ sqlclient.Connection, _ string, params map[string]string) ([]sqlclient.Row, error) {
		parameters = params

		return []sqlclient.Row{
			{"iam_arn": arn, "privilege_type": "COPY", "identity_name": "loader", "identity_type": "user"},
			{"iam_arn": assumeroleGrantAllRoles, "privilege_type": "UNLOAD", "identity_name": "operators", "identity_type": "role"},
			{"iam_arn": assumeroleGrantDefaultRole, "privilege_type": "COPY", "identity_name": "readers", "identity_type": "role"},
			{"iam_arn": assumeroleGrantDefaultRole, "privilege_type": "EXFUNC", "identity_name": "readers", "identity_type": "role"},
		}, nil
	})

	items := listingItems(t, newAssumeroleGrantsDataSource(), nil, client)
	if assert.Len(t, items, 3) {
		assert.Equal(t, types.StringValue("ALL"), items[0]["iam_role_arn"])
		assert.Equal(t, types.StringValue("DEFAULT"), items[1]["iam_role_arn"])
		assert.Equal(t, types.StringValue("ROLE"), items[1]["grantee_type"])
		assert.Equal(t, stringSet("COPY", "EXTERNAL FUNCTION"), items[1]["privileges"])
		assert.Equal(t, types.StringValue(arn), items[2]["iam_role_arn"])
		assert.Equal(t, types.StringValue("USER"), items[2]["grantee_type"])
	}

	for selector, catalogRole := range map[string]string{"default": assumeroleGrantDefaultRole, "ALL": assumeroleGrantAllRoles, arn: arn} {
		listingItems(t, newAssumeroleGrantsDataSource(), map[string]string{"iam_role_arn": selector, "grantee": "readers"}, client)
		assert.Equal(t, map[string]string{"arn": catalogRole, "grantee": "readers"}, parameters)
	}

	parameters = nil

	assertListingFails(t, newAssumeroleGrantsDataSource(), map[string]string{"iam_role_arn": "not-an-arn"}, client)
	assert.Nil(t, parameters, "an invalid role selector never reaches SQL")
	assertListingFails(t, newAssumeroleGrantsDataSource(), nil, nil)
}
