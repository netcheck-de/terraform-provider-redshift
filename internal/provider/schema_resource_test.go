package provider

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
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

// TestDroppedParentDatabaseIsNotFound removes database-local objects whose database was dropped outside Terraform.
func TestDroppedParentDatabaseIsNotFound(t *testing.T) {
	client := testResourceClient(queryFunc(func(_ context.Context, target dataapi.Connection, sql string, _ map[string]string) ([]dataapi.Row, error) {
		if target.Database == "admin" && strings.HasPrefix(sql, "SELECT database_name FROM svv_redshift_databases") {
			return nil, nil
		}
		return nil, fmt.Errorf("database %q does not exist", target.Database)
	}))
	database := types.StringValue("analytics")
	reads := map[string]func() (bool, error){
		"schema": func() (bool, error) {
			return (&schemaResource{client}).read(context.Background(), &schemaModel{Database: database, Name: types.StringValue("serving")})
		},
		"external_schema": func() (bool, error) {
			return (&externalSchemaResource{client}).read(context.Background(), &externalSchemaModel{Database: database, Name: types.StringValue("lake")})
		},
		"datashare": func() (bool, error) {
			return (&datashareResource{client}).read(context.Background(), &datashareModel{Database: database, Name: types.StringValue("share")})
		},
		"datashare_schema": func() (bool, error) {
			return (&datashareSchemaResource{client}).read(context.Background(), &datashareSchemaModel{Database: database, Datashare: types.StringValue("share"), Schema: types.StringValue("serving")})
		},
		"datashare_table": func() (bool, error) {
			return (&datashareTableResource{client}).read(context.Background(), datashareTableModel{Database: database, Datashare: types.StringValue("share"), Schema: types.StringValue("serving"), Table: types.StringValue("t")})
		},
		"datashare_grant": func() (bool, error) {
			return (&datashareGrantResource{client}).read(context.Background(), datashareGrantModel{Database: database, Datashare: types.StringValue("share"), AccountID: types.StringValue("123456789012")})
		},
	}
	for name, read := range reads {
		t.Run(name, func(t *testing.T) {
			found, err := read()
			require.NoError(t, err)
			assert.False(t, found)
		})
	}
}

// creationOnlyClient accepts CREATE statements and then fails or misses every catalog readback.
func creationOnlyClient(unavailable bool) resourceClient {
	return testResourceClient(queryFunc(func(_ context.Context, _ dataapi.Connection, sql string, _ map[string]string) ([]dataapi.Row, error) {
		if unavailable && !strings.HasPrefix(sql, "CREATE ") {
			return nil, fmt.Errorf("catalog unavailable")
		}
		return nil, nil
	}))
}

// TestSchemaCreationErrorsRetainKnownState keeps a known identity when readback fails after a successful CREATE.
func TestSchemaCreationErrorsRetainKnownState(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		t.Run(fmt.Sprintf("unavailable=%t", unavailable), func(t *testing.T) {
			r := &schemaResource{creationOnlyClient(unavailable)}
			state := testState(t, r, schemaModel{Database: types.StringValue("admin"), Name: types.StringValue("serving"), Owner: types.StringUnknown()})
			resp := resource.CreateResponse{State: tfsdk.State{Schema: state.Schema}}
			r.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan(state)}, &resp)
			require.True(t, resp.Diagnostics.HasError())
			var observed schemaModel
			require.False(t, resp.State.Get(context.Background(), &observed).HasError())
			assert.False(t, observed.ID.IsNull())
			assert.True(t, observed.Owner.IsNull())
			assert.True(t, resp.State.Raw.IsFullyKnown())
		})
	}
}
