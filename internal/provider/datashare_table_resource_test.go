package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDatashareTableMembership checks explicit table membership refresh.
func TestDatashareTableMembership(t *testing.T) {
	c := &catalog{shareTable: true}
	r := &datashareTableResource{testResourceClient(c)}
	data := datashareTableModel{Database: types.StringValue("admin"), Datashare: types.StringValue("producer"), Schema: types.StringValue("serving"), Table: types.StringValue("table")}
	found, err := r.read(context.Background(), data)
	require.NoError(t, err)
	assert.True(t, found)
	c.shareTable = false
	found, err = r.read(context.Background(), data)
	require.NoError(t, err)
	assert.False(t, found)
}
