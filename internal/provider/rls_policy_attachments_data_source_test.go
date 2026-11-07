package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newRlsPolicyAttachmentsDataSource, resource: newRlsPolicyAttachmentResource, collection: true, filters: []string{"database", "policy", "schema", "relation"}})

// rlsAttachmentListing reads a listing and returns its elements' attributes.
func rlsAttachmentListing(t *testing.T, filters map[string]string, client sqlclient.Client) []map[string]string {
	t.Helper()

	source := newRlsPolicyAttachmentsDataSource()
	state, diagnostics := readSource(t, source, collectionConfig(t, source, filters), client)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)

	var data types.Object
	require.False(t, state.Get(context.Background(), &data).HasError())

	var items []map[string]string

	for _, element := range collectionResult(source, data).Elements() {
		item := map[string]string{}
		for _, name := range []string{"database", "policy", "schema", "relation", "grantee", "grantee_type"} {
			item[name] = objectString(element.(types.Object), name)
		}

		items = append(items, item)
	}

	return items
}

// TestRlsPolicyAttachmentsListing lists the attachments of the provider database in the resource's terms, narrows
// them by policy, schema, and relation, and reports catalog failures.
func TestRlsPolicyAttachmentsListing(t *testing.T) {
	attached := map[string]string{"database": "admin", "policy": "region_filter", "schema": "public", "relation": "events", "grantee": "analysts", "grantee_type": "ROLE"}

	for _, filters := range []map[string]string{nil, {"database": "admin", "policy": "region_filter", "schema": "public", "relation": "events"}} {
		assert.Equal(t, []map[string]string{attached}, rlsAttachmentListing(t, filters, fullCatalog()))
	}

	for _, filters := range []map[string]string{{"policy": "other"}, {"schema": "other"}, {"relation": "other"}} {
		assert.Empty(t, rlsAttachmentListing(t, filters, fullCatalog()), "%v", filters)
	}

	var (
		target     sqlclient.Connection
		parameters map[string]string
	)

	client := queryFunc(func(_ context.Context, connection sqlclient.Connection, sql string, params map[string]string) ([]sqlclient.Row, error) {
		require.Contains(t, sql, "FROM svv_rls_attached_policy")

		target, parameters = connection, params

		return []sqlclient.Row{
			{"polname": "region_filter", "relschema": "sales", "relname": "orders", "grantee": "public", "granteekind": "role"},
			{"polname": "region_filter", "relschema": "sales", "relname": "orders", "grantee": "loader", "granteekind": "user"},
		}, nil
	})
	items := rlsAttachmentListing(t, map[string]string{"database": "analytics", "schema": "sales"}, client)
	assert.Equal(t, "analytics", target.Database, "the view covers only the connected database")
	assert.Equal(t, map[string]string{"schema": "sales"}, parameters)
	require.Len(t, items, 2)
	assert.Equal(t, map[string]string{"database": "analytics", "policy": "region_filter", "schema": "sales", "relation": "orders", "grantee": "public", "grantee_type": "PUBLIC"}, items[0])
	assert.Equal(t, "USER", items[1]["grantee_type"])

	source := newRlsPolicyAttachmentsDataSource()
	_, diagnostics := readSource(t, source, collectionConfig(t, source, nil), queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
		return nil, errors.New("catalog unavailable")
	}))
	assert.True(t, diagnostics.HasError())
}
