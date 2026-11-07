package provider

import (
	"context"
	"maps"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newDatashareSchemasDataSource, resource: newDatashareSchemaResource, collection: true, filters: []string{"database", "datashare", "schema"}})

// TestDatashareSchemasList lists the shared schemas of the provider database, a named share, or one schema, with
// include_new.
func TestDatashareSchemasList(t *testing.T) {
	for _, test := range []struct {
		name    string
		filters map[string]string
		count   int
	}{
		{"defaults", nil, 2},
		{"share", map[string]string{"database": "admin", "datashare": "producer"}, 2},
		{"missing_share", map[string]string{"datashare": "missing"}, 0},
		{"schema", map[string]string{"schema": "serving"}, 1},
		{"missing_schema", map[string]string{"schema": "missing"}, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			id, items := readListing(t, newDatashareSchemasDataSource(), test.filters, fullCatalog())
			expected := map[string]string{"database": "admin"}
			maps.Copy(expected, test.filters)
			assertLookupIdentity(t, id, expected)
			require.Len(t, items, test.count)

			if test.count == 1 {
				assert.Equal(t, []map[string]attr.Value{
					{"database": types.StringValue("admin"), "datashare": types.StringValue("producer"), "schema": types.StringValue("serving"), "include_new": types.BoolValue(true)},
				}, items)
			}

			if test.count == 2 {
				assert.Equal(t, []map[string]attr.Value{
					{"database": types.StringValue("admin"), "datashare": types.StringValue("producer"), "schema": types.StringValue("odd.schema"), "include_new": types.BoolValue(false)},
					{"database": types.StringValue("admin"), "datashare": types.StringValue("producer"), "schema": types.StringValue("serving"), "include_new": types.BoolValue(true)},
				}, items)
			}
		})
	}
}

// TestDatashareSchemasListMissingDatabase returns an empty listing for a missing database without reading its members.
func TestDatashareSchemasListMissingDatabase(t *testing.T) {
	id, items := readListing(t, newDatashareSchemasDataSource(), map[string]string{"database": "missing"}, queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
		assert.Contains(t, sql, "svv_redshift_databases", "an absent database must not be queried for members")
		return nil, nil
	}))
	assert.Empty(t, items)
	assertLookupIdentity(t, id, map[string]string{"database": "missing"})
}

// TestDatashareSchemasListFailures reports catalog errors and undecodable include_new values.
func TestDatashareSchemasListFailures(t *testing.T) {
	for name, client := range map[string]sqlclient.Client{
		"catalog": failingCatalog,
		"include_new": queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
			return []sqlclient.Row{{"share_name": "producer", "object_type": "schema", "object_name": "serving", "include_new": "maybe"}}, nil
		}),
	} {
		t.Run(name, func(t *testing.T) {
			source := newDatashareSchemasDataSource()
			_, diagnostics := readSource(t, source, collectionConfig(t, source, nil), client)
			assert.True(t, diagnostics.HasError())
		})
	}
}

// TestDatashareObjectListSQL pins the member listings of the schema, table, and membership lists, which share one
// query shape with different object types.
func TestDatashareObjectListSQL(t *testing.T) {
	built := func(query sqlclient.Query) func() (string, error) {
		return func() (string, error) {
			sql, _, err := query.Build()
			return sql, err
		}
	}
	checkSQL(t, "datashare_object_lists", []sqlCase{
		{"schemas", built(listDatashareObjectsQuery("admin", "", datashareSchemaTypes))},
		{"schemas_share", built(listDatashareObjectsQuery("admin", `Odd"Share`, datashareSchemaTypes))},
		{"tables", built(listDatashareObjectsQuery("admin", "producer", datashareRelationRows))},
		{"memberships", built(listDatashareObjectsQuery("admin", "", datashareMembershipObjectTypes))},
		{"empty_database", built(listDatashareObjectsQuery("", "producer", datashareSchemaTypes))},
		{"table_candidates", built(listDatashareMembershipCandidatesQuery("TABLES", "admin", ""))},
		{"table_candidates_schema", built(listDatashareMembershipCandidatesQuery("TABLES", "admin", "serving"))},
		{"function_candidates", built(listDatashareMembershipCandidatesQuery("FUNCTIONS", "admin", ""))},
		{"function_candidates_schema", built(listDatashareMembershipCandidatesQuery("FUNCTIONS", "admin", "serving"))},
	})
}
