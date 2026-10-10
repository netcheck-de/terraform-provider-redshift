package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newDatasharesDataSource, collection: true, filters: []string{"share_type", "name"}})

// TestDatasharesSharedElementTypes keeps the attributes the listing shares with redshift_datashare in step with it.
func TestDatasharesSharedElementTypes(t *testing.T) {
	assertCollectionParity(t, parityCase{
		source: func() datasource.DataSource {
			element := collectionElement(newDatashareResource, "name", "publicly_accessible", "owner", "share_id", "producer_account", "producer_namespace", "created_at")
			return newCollectionDataSource(collectionSpec{name: "datashares", element: element})
		},
		resource: newDatashareResource, collection: true,
	})
}

// TestDatasharesList checks unfiltered and filtered listings, inbound nulls, identities, and read-only execution.
func TestDatasharesList(t *testing.T) {
	for _, test := range []struct {
		name    string
		filters map[string]string
		shares  []string
	}{
		{"all", nil, []string{"producer", "source"}},
		{"outbound", map[string]string{"share_type": "OUTBOUND"}, []string{"producer"}},
		{"inbound_named", map[string]string{"share_type": "INBOUND", "name": "source"}, []string{"source"}},
		{"none", map[string]string{"name": "missing"}, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := fullCatalog()
			source := newDatasharesDataSource()
			state, diagnostics := readSource(t, source, collectionConfig(t, source, test.filters), c)
			require.False(t, diagnostics.HasError(), "%v", diagnostics)
			assert.Empty(t, c.writes)
			var observed types.Object
			require.False(t, state.Get(context.Background(), &observed).HasError())
			identity := map[string]string{}
			for key, value := range test.filters {
				identity[key] = value
			}
			assertLookupIdentity(t, observed.Attributes()["id"].(types.String), "admin", identity)
			items := observed.Attributes()[collectionItems].(types.List).Elements()
			require.Len(t, items, len(test.shares))
			for index, name := range test.shares {
				item := items[index].(types.Object).Attributes()
				assert.Equal(t, types.StringValue(name), item["name"])
				if name == "producer" {
					assert.Equal(t, types.StringValue("OUTBOUND"), item["share_type"])
					assert.Equal(t, types.StringValue("admin"), item["database"])
					assert.Equal(t, types.StringValue("admin"), item["owner"])
					assert.Equal(t, types.Int64Value(100), item["share_id"])
					assert.Equal(t, types.StringValue("2026-01-02 03:04:05"), item["created_at"])
					assert.True(t, item["consumer_database"].IsNull())
				} else {
					assert.Equal(t, types.StringValue("INBOUND"), item["share_type"])
					assert.Equal(t, types.StringValue("analytics"), item["consumer_database"])
					// SVV_DATASHARES documents share_id for every row, so inbound IDs pass through as well.
					assert.Equal(t, types.Int64Value(200), item["share_id"])
					for _, empty := range []string{"database", "owner", "created_at", "managed_by"} {
						assert.True(t, item[empty].IsNull(), empty)
					}
				}
				assert.Equal(t, types.StringValue("123456789012"), item["producer_account"])
				assert.Equal(t, types.BoolValue(false), item["publicly_accessible"])
			}
		})
	}
}

// TestDatasharesListFailures reports catalog errors and undecodable rows instead of a partial listing.
func TestDatasharesListFailures(t *testing.T) {
	for name, rows := range map[string][]sqlclient.Row{
		"share id":   {{"share_name": "producer", "share_id": "not-a-number"}},
		"accessible": {{"share_name": "producer", "is_publicaccessible": "maybe"}},
	} {
		t.Run(name, func(t *testing.T) {
			source := newDatasharesDataSource()
			_, diagnostics := readSource(t, source, collectionConfig(t, source, nil), queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
				return rows, nil
			}))
			assert.True(t, diagnostics.HasError())
		})
	}
	source := newDatasharesDataSource()
	_, diagnostics := readSource(t, source, collectionConfig(t, source, nil), queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
		return nil, errors.New("catalog unavailable")
	}))
	assert.True(t, diagnostics.HasError())
}
