package provider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newViewsDataSource, resource: newViewResource, collection: true, filters: []string{"database", "schema"}})

// viewsListingFake adds a late-binding view in a second schema to the representative view and materialized view.
func viewsListingFake() (*catalog, *viewFamily) {
	c, views := viewFake()
	views.relations[fakeViewKey("staging", "late_labels")] = &fakeViewRelation{schema: "staging", name: "late_labels", query: fakeViewQuery, owner: "etl", lateBinding: true}

	return c, views
}

// listingTranscript reads a listing against client and pins the SQL conversation in group.
func listingTranscript(t *testing.T, group, name string, client sqlclient.Client, read func(sqlclient.Client) ([]map[string]attr.Value, map[string]string, error)) []map[string]attr.Value {
	t.Helper()

	recorder := &recordingClient{client: client}
	items, _, err := read(recorder)
	require.NoError(t, err)
	checkTranscript(t, group, name, recorder.take())

	return items
}

// TestViewsListingTranscript lists ordinary and late-binding views with the lookup's values, leaves out materialized views,
// and filters by schema.
func TestViewsListingTranscript(t *testing.T) {
	c, views := viewsListingFake()
	read := func(filters map[string]string) func(sqlclient.Client) ([]map[string]attr.Value, map[string]string, error) {
		return func(client sqlclient.Client) ([]map[string]attr.Value, map[string]string, error) {
			items, identity, diagnostics := discoveryRead(t, newViewsDataSource, filters, client)
			if diagnostics.HasError() {
				return nil, nil, errors.New(diagnostics.Errors()[0].Detail())
			}

			return items, identity, nil
		}
	}

	items := listingTranscript(t, "views", "database", c, read(nil))
	assert.Equal(t, []string{fakeViewName, "late_labels"}, discoveryNames(items, "name"), "materialized views are not listed")
	definition := views.get(fakeViewName).definition()
	assert.Equal(t, map[string]attr.Value{
		"database": types.StringValue("admin"), "schema": types.StringValue(fakeViewSchema), "name": types.StringValue(fakeViewName),
		"query": types.StringValue(definition), "late_binding": types.BoolValue(false), "owner": types.StringValue(fakeViewOwner),
		"definition_fingerprint": types.StringValue(definitionFingerprint(definition)),
	}, items[0])
	assert.Equal(t, types.BoolValue(true), items[1]["late_binding"])

	items = listingTranscript(t, "views", "schema", c, read(map[string]string{"database": "admin", "schema": "staging"}))
	assert.Equal(t, []string{"late_labels"}, discoveryNames(items, "name"))

	items = listingTranscript(t, "views", "missing_database", c, read(map[string]string{"database": "missing"}))
	assert.Empty(t, items, "a database that is not local lists no views")

	discoveryFailures(t, newViewsDataSource, nil, sqlclient.Row{"schemaname": "serving", "viewname": "orphan", "viewowner": "", "definition": "SELECT 1;"},
		sqlclient.Row{"schemaname": "serving", "viewname": "", "viewowner": "admin", "definition": "SELECT 1;"})
}

// TestViewsSQL pins the view listing and its schema filter.
func TestViewsSQL(t *testing.T) {
	checkSQL(t, "views_sql", []sqlCase{
		{"database", discoverySQL(listViewsQuery(""))},
		{"schema", discoverySQL(listViewsQuery(`Odd"Schema`))},
	})
}

// TestViewsListingRunsInListedDatabase reads pg_views in the listed database, which is the only one it covers.
func TestViewsListingRunsInListedDatabase(t *testing.T) {
	var connections []string

	_, _, diagnostics := discoveryRead(t, newViewsDataSource, map[string]string{"database": "analytics"}, queryFunc(func(_ context.Context, connection sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
		if strings.Contains(sql, "svv_redshift_databases") {
			return []sqlclient.Row{{"database_name": "analytics"}}, nil
		}

		connections = append(connections, connection.Database)

		return nil, nil
	}))
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.Equal(t, []string{"analytics"}, connections)
}
