package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDatashareObservesAccessibility checks refresh of the public-access setting.
func TestDatashareObservesAccessibility(t *testing.T) {
	c := &catalog{share: true}
	r := &datashareResource{testResourceClient(c)}
	data := datashareModel{Database: types.StringValue("admin"), Name: types.StringValue("producer")}
	found, err := r.read(context.Background(), &data)
	require.NoError(t, err)
	assert.True(t, found)
	assert.False(t, data.PublicAccessible.ValueBool())
	c.public = true
	found, err = r.read(context.Background(), &data)
	require.NoError(t, err)
	assert.True(t, found)
	assert.True(t, data.PublicAccessible.ValueBool())
}

// TestDatashareRejectsDifferentOwnerAndInvalidCatalog prevents adoption of unrelated shares.
func TestDatashareRejectsDifferentOwnerAndInvalidCatalog(t *testing.T) {
	for _, changed := range []dataapi.Row{
		{"source_database": "another"},
		{"managed_by": "ADX"},
		{"is_publicaccessible": "invalid"},
	} {
		row := dataapi.Row{"share_name": "producer", "source_database": "admin", "managed_by": "", "is_publicaccessible": "false"}
		for key, value := range changed {
			row[key] = value
		}
		r := &datashareResource{testResourceClient(queryFunc(func(context.Context, dataapi.Connection, string, map[string]string) ([]dataapi.Row, error) {
			return []dataapi.Row{row}, nil
		}))}
		_, err := r.read(context.Background(), &datashareModel{Database: types.StringValue("admin"), Name: types.StringValue("producer")})
		require.Error(t, err)
	}
}

// TestDatashareRequiresKnownTarget verifies unknown warehouses cannot receive SQL.
func TestDatashareRequiresKnownTarget(t *testing.T) {
	r := &datashareResource{resourceClient{database: types.StringValue("admin"), warehouse: warehouseBinding{field: "workgroup_name", value: types.StringUnknown()}}}
	data := datashareModel{Database: types.StringValue("admin")}
	_, err := r.read(context.Background(), &data)
	require.ErrorContains(t, err, "provider workgroup_name and database must be known")
}
