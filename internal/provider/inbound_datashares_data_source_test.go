package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newInboundDatasharesDataSource, collection: true, filters: []string{"name"}})

// TestInboundDatasharesList lists only inbound shares, filtered by name, with nulls for absent catalog values.
func TestInboundDatasharesList(t *testing.T) {
	for _, test := range []struct {
		name    string
		filters map[string]string
		count   int
	}{
		{"all", nil, 1},
		{"named", map[string]string{"name": "source"}, 1},
		{"outbound_name", map[string]string{"name": "producer"}, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			id, items := readListing(t, newInboundDatasharesDataSource(), test.filters, fullCatalog())
			assertLookupIdentity(t, id, test.filters)
			require.Len(t, items, test.count)

			if test.count == 1 {
				assert.Equal(t, map[string]attr.Value{
					"name": types.StringValue("source"), "share_id": types.Int64Value(200), "producer_account": types.StringValue("123456789012"),
					"producer_namespace": types.StringValue("11111111-2222-3333-4444-555555555555"), "created_at": types.StringNull(),
					"consumer_database": types.StringValue("analytics"), "managed_by": types.StringNull(),
				}, items[0])
			}
		})
	}
}

// TestInboundDatasharesListFailures reports catalog errors and undecodable rows instead of a partial listing.
func TestInboundDatasharesListFailures(t *testing.T) {
	for name, client := range map[string]sqlclient.Client{
		"catalog": failingCatalog,
		"share id": queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
			return []sqlclient.Row{{"share_name": "source", "share_id": "not-a-number"}}, nil
		}),
	} {
		t.Run(name, func(t *testing.T) {
			source := newInboundDatasharesDataSource()
			_, diagnostics := readSource(t, source, collectionConfig(t, source, nil), client)
			assert.True(t, diagnostics.HasError())
		})
	}
}
