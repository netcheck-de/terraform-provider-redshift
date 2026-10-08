package provider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDatashareSchemaMembership checks catalog membership and include_new refresh.
func TestDatashareSchemaMembership(t *testing.T) {
	c := &catalog{shareSchema: true}
	r := &datashareSchemaResource{testResourceClient(c)}
	data := datashareSchemaModel{Database: types.StringValue("admin"), Datashare: types.StringValue("producer"), Schema: types.StringValue("serving")}
	found, err := r.read(context.Background(), &data)
	require.NoError(t, err)
	assert.True(t, found)
	c.shareSchema = false
	found, err = r.read(context.Background(), &data)
	require.NoError(t, err)
	assert.False(t, found)
}

// TestDatashareSchemaRejectsInvalidCatalog rejects ambiguous or malformed schema members.
func TestDatashareSchemaRejectsInvalidCatalog(t *testing.T) {
	for _, rows := range [][]dataapi.Row{
		{{"object_name": "serving", "include_new": "invalid"}},
		{{"object_name": "serving", "include_new": "false"}, {"object_name": "serving", "include_new": "false"}},
	} {
		r := &datashareSchemaResource{testResourceClient(queryFunc(func(context.Context, dataapi.Connection, string, map[string]string) ([]dataapi.Row, error) {
			return rows, nil
		}))}
		data := datashareSchemaModel{Database: types.StringValue("admin"), Datashare: types.StringValue("producer"), Schema: types.StringValue("serving")}
		_, err := r.read(context.Background(), &data)
		require.Error(t, err)
	}
}

// TestDatashareSchemaReportsIncludeNewFailure checks errors after membership creation.
func TestDatashareSchemaReportsIncludeNewFailure(t *testing.T) {
	c := &catalog{}
	r := &datashareSchemaResource{testResourceClient(queryFunc(func(ctx context.Context, target dataapi.Connection, sql string, parameters map[string]string) ([]dataapi.Row, error) {
		if c.shareSchema && strings.HasPrefix(sql, "ALTER DATASHARE") && strings.Contains(sql, " SET INCLUDENEW ") {
			return nil, errors.New("cannot enable include_new")
		}
		return c.Query(ctx, target, sql, parameters)
	}))}
	data := datashareSchemaModel{Database: types.StringValue("admin"), Datashare: types.StringValue("producer"), Schema: types.StringValue("serving"), IncludeNew: types.BoolValue(true)}
	assert.True(t, invoke(t, r, "create", data, false).HasError())
	assert.True(t, c.shareSchema)
}
