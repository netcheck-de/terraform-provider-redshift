package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSchemaCatalogOwnership checks observed schema ownership.
func TestSchemaCatalogOwnership(t *testing.T) {
	r := &schemaResource{testResourceClient(queryFunc(func(context.Context, dataapi.Connection, string, map[string]string) ([]dataapi.Row, error) {
		return []dataapi.Row{{"schema_name": "serving", "owner": "warehouse_admin"}}, nil
	}))}
	data := schemaModel{Database: types.StringValue("admin"), Name: types.StringValue("serving")}
	found, err := r.read(context.Background(), &data)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "warehouse_admin", data.Owner.ValueString())
}

// TestSchemaRejectsIncompleteMetadata rejects missing or ambiguous catalog fields.
func TestSchemaRejectsIncompleteMetadata(t *testing.T) {
	for _, rows := range [][]dataapi.Row{
		{{"schema_name": "serving"}},
		{{"schema_name": "serving", "owner": "admin"}, {"schema_name": "serving", "owner": "admin"}},
	} {
		r := &schemaResource{testResourceClient(queryFunc(func(context.Context, dataapi.Connection, string, map[string]string) ([]dataapi.Row, error) {
			return rows, nil
		}))}
		_, err := r.read(context.Background(), &schemaModel{Database: types.StringValue("admin"), Name: types.StringValue("serving")})
		require.Error(t, err)
	}
}
