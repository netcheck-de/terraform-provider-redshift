package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newDatasharesDataSource, resource: newDatashareResource, collection: true, filters: []string{"name"}})

// readListing reads a listing without writes and returns its identity and elements.
func readListing(t *testing.T, source datasource.DataSource, filters map[string]string, client sqlclient.Client) (types.String, []map[string]attr.Value) {
	t.Helper()

	state, diagnostics := readSource(t, source, collectionConfig(t, source, filters), client)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)

	if c, ok := client.(*catalog); ok {
		assert.Empty(t, c.writes)
	}

	var observed types.Object
	require.False(t, state.Get(context.Background(), &observed).HasError())

	var items []map[string]attr.Value
	for _, item := range collectionResult(source, observed).Elements() {
		items = append(items, item.(types.Object).Attributes())
	}

	return observed.Attributes()["id"].(types.String), items
}

// TestDatasharesList lists only outbound shares, filtered by name, with the resource's observed attributes.
func TestDatasharesList(t *testing.T) {
	for _, test := range []struct {
		name    string
		filters map[string]string
		count   int
	}{
		{"all", nil, 1},
		{"named", map[string]string{"name": "producer"}, 1},
		{"inbound_name", map[string]string{"name": "source"}, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			id, items := readListing(t, newDatasharesDataSource(), test.filters, fullCatalog())
			assertLookupIdentity(t, id, test.filters)
			require.Len(t, items, test.count)

			if test.count == 1 {
				assert.Equal(t, map[string]attr.Value{
					"database": types.StringValue("admin"), "name": types.StringValue("producer"), "publicly_accessible": types.BoolValue(false),
					"owner": types.StringValue("admin"), "share_id": types.Int64Value(100), "producer_account": types.StringValue("123456789012"),
					"producer_namespace": types.StringValue("11111111-2222-3333-4444-555555555555"), "created_at": types.StringValue("2026-01-02 03:04:05"),
				}, items[0])
			}
		})
	}
}

// TestDatasharesListFailures reports catalog errors and undecodable rows instead of a partial listing.
func TestDatasharesListFailures(t *testing.T) {
	for name, client := range map[string]sqlclient.Client{
		"catalog": failingCatalog,
		"share id": queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
			return []sqlclient.Row{{"share_name": "producer", "share_id": "not-a-number"}}, nil
		}),
		"accessible": queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
			return []sqlclient.Row{{"share_name": "producer", "is_publicaccessible": "maybe"}}, nil
		}),
	} {
		t.Run(name, func(t *testing.T) {
			source := newDatasharesDataSource()
			_, diagnostics := readSource(t, source, collectionConfig(t, source, nil), client)
			assert.True(t, diagnostics.HasError())
		})
	}
}
