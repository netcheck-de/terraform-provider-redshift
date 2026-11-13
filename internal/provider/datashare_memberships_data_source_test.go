package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newDatashareMembershipsDataSource, resource: newDatashareMembershipResource, collection: true, filters: []string{"database", "datashare", "schema"}})

// datashareMembershipItem is one expected membership element in the admin database.
func datashareMembershipItem(schema, kind string, all bool) map[string]attr.Value {
	return map[string]attr.Value{
		"database": types.StringValue("admin"), "datashare": types.StringValue("producer"), "schema": types.StringValue(schema),
		"kind": types.StringValue(kind), "all": types.BoolValue(all),
	}
}

// membershipCatalog shares one of two SQL overloads and the only table of the serving schema, consistently for the
// member listing and the lookup's member query.
func membershipCatalog() *catalog {
	c := fullCatalog()
	fake := fakeState[*fakeDatashares](c, "datashare")
	fake.objects = []sqlclient.Row{
		{"object_name": "odd.schema", "object_type": "schema", "include_new": "false"},
		{"object_name": "serving", "object_type": "schema", "include_new": "false"},
		{"object_name": "serving.f_shared", "object_type": "function"},
		{"object_name": "serving.table", "object_type": "table"},
	}
	fake.bulkMembers["FUNCTIONS"] = []string{"f_shared"}

	return c
}

// TestDatashareMembershipsList derives coverage per share, schema, and kind as the lookup does, omits a schema whose
// name contains a period, and agrees with the lookup for every listed tuple.
func TestDatashareMembershipsList(t *testing.T) {
	expected := []map[string]attr.Value{datashareMembershipItem("serving", "FUNCTIONS", false), datashareMembershipItem("serving", "TABLES", true)}

	for _, filters := range []map[string]string{nil, {"datashare": "producer", "schema": "serving"}} {
		c := membershipCatalog()

		var queries int

		client := queryFunc(func(ctx context.Context, connection sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
			queries++
			return c.Query(ctx, connection, sql, parameters)
		})
		_, items := readListing(t, newDatashareMembershipsDataSource(), filters, client)
		assert.Equal(t, expected, items)
		assert.Equal(t, 3, queries, "members and both candidate kinds are read once each")

		for _, item := range items {
			lookup := map[string]string{"database": "admin", "datashare": "producer", "schema": "serving", "kind": item["kind"].(types.String).ValueString()}
			exerciseCatalogLookup(t, newDatashareMembershipDataSource, lookup, map[string]attr.Value{"all": item["all"], "exists": types.BoolValue(true)}, membershipCatalog())
		}
	}

	for _, filters := range []map[string]string{{"schema": "odd.schema"}, {"datashare": "missing"}} {
		_, items := readListing(t, newDatashareMembershipsDataSource(), filters, membershipCatalog())
		assert.Empty(t, items)
	}
}

// TestDatashareMembershipsListAmbiguousSource omits a kind whose source object names contain a period, keeping the
// other kind of the same schema.
func TestDatashareMembershipsListAmbiguousSource(t *testing.T) {
	c := membershipCatalog()
	fakeState[*fakeDatashares](c, "datashare").bulkCandidates["TABLES"] = []string{"odd.name"}
	_, items := readListing(t, newDatashareMembershipsDataSource(), nil, c)
	assert.Equal(t, []map[string]attr.Value{datashareMembershipItem("serving", "FUNCTIONS", false)}, items)
}

// TestDatashareMembershipsListFailures reports an error from each catalog query instead of a partial listing.
func TestDatashareMembershipsListFailures(t *testing.T) {
	for _, failing := range []string{"svv_datashare_objects", "svv_all_tables", "pg_proc_info"} {
		t.Run(failing, func(t *testing.T) {
			c := fullCatalog()
			source := newDatashareMembershipsDataSource()
			_, diagnostics := readSource(t, source, collectionConfig(t, source, nil), queryFunc(func(ctx context.Context, connection sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
				if strings.Contains(sql, failing) {
					return nil, assert.AnError
				}

				return c.Query(ctx, connection, sql, parameters)
			}))
			require.True(t, diagnostics.HasError())
		})
	}
}
