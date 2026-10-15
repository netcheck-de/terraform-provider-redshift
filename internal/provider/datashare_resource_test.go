package provider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerLifecycleCase(lifecycleCase{name: "datashare", new: newDatashareResource, model: datashareModel{Database: types.StringValue("admin"), Name: types.StringValue("producer"), PublicAccessible: types.BoolValue(false)}, absent: func(c *catalog) { c.share = false }, dependents: func(c *catalog) { c.shareSchema, c.shareGrant = false, false }})

var _ = registerReplacementPolicy("redshift_datashare", map[string]replaceRule{
	"database":            replaceAlways,
	"name":                replaceAlways,
	"publicly_accessible": replaceNever,
})

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

// TestDatashareObservesCatalogMetadata checks owner, ID, producer identity, and creation time, and that an empty
// catalog value becomes null rather than an empty string.
func TestDatashareObservesCatalogMetadata(t *testing.T) {
	c := fullCatalog()
	r := &datashareResource{testResourceClient(c)}
	data := datashareModel{Database: types.StringValue("admin"), Name: types.StringValue("producer")}
	found, err := r.read(context.Background(), &data)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, types.StringValue("admin"), data.Owner)
	assert.Equal(t, types.Int64Value(100), data.ShareID)
	assert.Equal(t, types.StringValue("123456789012"), data.ProducerAccount)
	assert.Equal(t, types.StringValue("11111111-2222-3333-4444-555555555555"), data.ProducerNamespace)
	assert.Equal(t, types.StringValue("2026-01-02 03:04:05"), data.CreatedAt)
	fakeState[*fakeDatashares](c, "datashare").owner = ""
	found, err = r.read(context.Background(), &data)
	require.NoError(t, err)
	require.True(t, found)
	assert.True(t, data.Owner.IsNull())
}

// TestDatashareCreateFailureLeavesKnownState keeps state written before verification free of unknown values, so a
// failed verification still records the created share.
func TestDatashareCreateFailureLeavesKnownState(t *testing.T) {
	r := &datashareResource{testResourceClient(queryFunc(func(_ context.Context, _ dataapi.Connection, sql string, _ map[string]string) ([]dataapi.Row, error) {
		if strings.HasPrefix(sql, "SELECT") {
			return nil, errors.New("catalog unavailable")
		}
		return nil, nil
	}))}
	var schema resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &schema)
	plan := testState(t, r, datashareModel{Database: types.StringValue("admin"), Name: types.StringValue("producer"), PublicAccessible: types.BoolValue(false), Owner: types.StringUnknown(), ShareID: types.Int64Unknown(), ProducerAccount: types.StringUnknown(), ProducerNamespace: types.StringUnknown(), CreatedAt: types.StringUnknown()})
	resp := resource.CreateResponse{State: tfsdk.State{Schema: schema.Schema}}
	r.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan(plan)}, &resp)
	require.True(t, resp.Diagnostics.HasError())
	assert.True(t, resp.State.Raw.IsFullyKnown(), "state after a failed verification must be fully known")
	var data datashareModel
	require.False(t, resp.State.Get(context.Background(), &data).HasError())
	assert.True(t, data.Owner.IsNull())
	assert.False(t, data.ID.IsNull())
}

// TestDatashareRejectsDifferentOwnerAndInvalidCatalog prevents adoption of unrelated shares.
func TestDatashareRejectsDifferentOwnerAndInvalidCatalog(t *testing.T) {
	for _, changed := range []dataapi.Row{
		{"source_database": "another"},
		{"managed_by": "ADX"},
		{"is_publicaccessible": "invalid"},
		{"share_id": "invalid"},
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
