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

var _ = registerParity(parityCase{source: newCommentsDataSource, resource: newCommentResource, collection: true, filters: []string{"database_name", "object_type", "schema_name"}})

// commentListingCatalog answers each object type's listing query with one annotated object and records the
// queries, their database, and their parameters.
type commentListingCatalog struct {
	// queries are the listing queries in the order they ran.
	queries []string
	// databases are the connection databases of the queries.
	databases []string
	// parameters are the bound parameters of the queries.
	parameters []map[string]string
	// fail makes the query of this object type fail.
	fail string
}

// commentListingSources recognize each object type's query by a source fragment, most specific first.
var commentListingSources = []struct{ kind, fragment string }{
	{"CONSTRAINT", "pg_constraint k"},
	{"COLUMN", "pg_attribute a"},
	{"VIEW", string(commentViewKind)},
	{"TABLE", "o.relkind IN"},
	{"SCHEMA", "pg_namespace o"},
	{"DATABASE", "pg_database o"},
}

// Query returns the annotated object of the recognized type.
func (c *commentListingCatalog) Query(_ context.Context, connection sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
	if local, _, _ := localDatabaseQuery(parameters["database"]).Build(); sql == local {
		// Every database of this catalog is local.
		return []sqlclient.Row{{"database_name": parameters["database"]}}, nil
	}

	kind := ""

	for _, source := range commentListingSources {
		if strings.Contains(sql, source.fragment) {
			kind = source.kind
			break
		}
	}

	c.queries, c.databases, c.parameters = append(c.queries, kind), append(c.databases, connection.Database), append(c.parameters, parameters)
	if kind == c.fail {
		return nil, errors.New("catalog unavailable")
	}

	return []sqlclient.Row{map[string]sqlclient.Row{
		"DATABASE":   {"object_name": connection.Database, "text": "Warehouse."},
		"SCHEMA":     {"object_name": "serving", "text": "Serving layer."},
		"TABLE":      {"schema_name": "serving", "object_name": "orders", "text": "Order facts."},
		"VIEW":       {"schema_name": "serving", "object_name": "order_labels", "text": "Labels."},
		"COLUMN":     {"schema_name": "serving", "object_name": "orders", "column_name": "id", "text": "Order key."},
		"CONSTRAINT": {"schema_name": "serving", "object_name": "orders", "constraint_name": "orders_pkey", "text": "It's the key."},
	}[kind]}, nil
}

// TestCommentsDataSource lists every object type with one query each, and only the selected type or schema when
// filtered.
func TestCommentsDataSource(t *testing.T) {
	c := &commentListingCatalog{}
	items, identity, diagnostics := discoveryRead(t, newCommentsDataSource, nil, c)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.Equal(t, commentListKinds, c.queries, "one query per supported object type")
	assert.Equal(t, []string{"admin", "admin", "admin", "admin", "admin", "admin"}, c.databases)
	assert.Equal(t, map[string]string{"database_name": "admin"}, identity)
	require.Len(t, items, 6)
	assert.Equal(t, commentListKinds, discoveryNames(items, "object_type"))

	null := types.StringNull()
	assert.Equal(t, map[string]attr.Value{
		"database_name": types.StringValue("admin"), "object_type": types.StringValue("DATABASE"), "schema_name": null,
		"object_name": types.StringValue("admin"), "column_name": null, "constraint_name": null, "text": types.StringValue("Warehouse."),
	}, items[0])
	assert.True(t, items[1]["schema_name"].IsNull(), "a schema comment has no schema_name")
	assert.Equal(t, types.StringValue("serving"), items[2]["schema_name"])
	assert.True(t, items[2]["column_name"].IsNull())
	assert.Equal(t, types.StringValue("id"), items[4]["column_name"])
	assert.True(t, items[4]["constraint_name"].IsNull())
	assert.Equal(t, types.StringValue("orders_pkey"), items[5]["constraint_name"])
	assert.Equal(t, types.StringValue("It's the key."), items[5]["text"])

	c = &commentListingCatalog{}
	items, identity, diagnostics = discoveryRead(t, newCommentsDataSource, map[string]string{"database_name": "analytics", "object_type": "COLUMN"}, c)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.Equal(t, []string{"COLUMN"}, c.queries)
	assert.Equal(t, []string{"analytics"}, c.databases)
	assert.Equal(t, map[string]string{"database_name": "analytics", "object_type": "COLUMN"}, identity)
	require.Len(t, items, 1)
	assert.Equal(t, types.StringValue("analytics"), items[0]["database_name"])

	c = &commentListingCatalog{}
	items, _, diagnostics = discoveryRead(t, newCommentsDataSource, map[string]string{"schema_name": "serving"}, c)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.Equal(t, []string{"TABLE", "VIEW", "COLUMN", "CONSTRAINT"}, c.queries, "database and schema comments have no schema")
	assert.Len(t, items, 4)

	for _, parameters := range c.parameters {
		assert.Equal(t, map[string]string{"schema": "serving"}, parameters)
	}

	c = &commentListingCatalog{}
	_, _, diagnostics = discoveryRead(t, newCommentsDataSource, map[string]string{"object_type": "SCHEMA", "schema_name": "serving"}, c)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.Empty(t, c.queries, "no schema comment has a schema_name")

	_, _, diagnostics = discoveryRead(t, newCommentsDataSource, nil, &commentListingCatalog{fail: "COLUMN"})
	assert.True(t, diagnostics.HasError(), "a failing query is an error, not a partial listing")
}
