package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
)

var _ = registerParity(parityCase{source: newColumnGrantsDataSource, resource: newColumnGrantResource, collection: true, filters: []string{"database_name", "schema_name", "object_name", "grantee", "grantee_type"}})

// TestColumnGrantsSQL pins the column privilege listing with and without each optional filter.
func TestColumnGrantsSQL(t *testing.T) {
	render := func(schema, object, grantee, granteeType string) func() (string, error) {
		return func() (string, error) {
			sql, _, err := listColumnGrantsQuery(schema, object, grantee, granteeType).Build()
			return sql, err
		}
	}
	checkSQL(t, "column_grants", []sqlCase{
		{"unfiltered", render("", "", "", "")},
		{"all_filters", render("serving", "events", "readers", "GROUP")},
		{"grantee_type_only", render("", "", "", "PUBLIC")},
	})
}

// TestColumnGrantsListing lists the fake's column privileges as one element per relation and grantee, filtered and
// unfiltered, in the selected database.
func TestColumnGrantsListing(t *testing.T) {
	c := fullCatalog()
	fake := fakeState[*columnGrantFake](c, "column_grant")
	fake.set(`GROUP "readers"`, map[string][]string{"SELECT": {"id"}})

	var database string

	client := queryFunc(func(ctx context.Context, connection sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		database = connection.Database
		return c.Query(ctx, connection, sql, parameters)
	})

	readers := columnGrantValue(columnGrantColumns{"SELECT": {"id"}})
	role := columnGrantValue(columnGrantColumns{"SELECT": {"id", "label"}, "UPDATE": {"label"}})

	for _, test := range []struct {
		name     string
		filters  map[string]string
		database string
		grantees []string
		values   []types.Map
	}{
		{"unfiltered", nil, "admin", []string{"GROUP:readers", "ROLE:example:readers"}, []types.Map{readers, role}},
		{"group", map[string]string{"database_name": "analytics", "schema_name": "serving", "object_name": "events", "grantee_type": "GROUP"}, "analytics", []string{"GROUP:readers"}, []types.Map{readers}},
		{"role", map[string]string{"grantee": "example:readers", "grantee_type": "ROLE"}, "admin", []string{"ROLE:example:readers"}, []types.Map{role}},
		{"empty", map[string]string{"object_name": "missing"}, "admin", nil, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			items := listingItems(t, newColumnGrantsDataSource(), test.filters, client)
			assert.Equal(t, test.database, database)

			var (
				grantees []string
				values   []types.Map
			)

			for _, item := range items {
				assert.Equal(t, map[string]attr.Value{"database_name": types.StringValue(test.database), "schema_name": types.StringValue("serving"), "object_name": types.StringValue("events")},
					map[string]attr.Value{"database_name": item["database_name"], "schema_name": item["schema_name"], "object_name": item["object_name"]})
				grantees = append(grantees, item["grantee_type"].(types.String).ValueString()+":"+item["grantee"].(types.String).ValueString())
				values = append(values, item["privileges"].(types.Map))
			}

			assert.Equal(t, test.grantees, grantees)
			assert.Equal(t, test.values, values, "privileges map each privilege to its columns, as the resource does")
		})
	}

	assertListingFails(t, newColumnGrantsDataSource(), nil, nil)
}
