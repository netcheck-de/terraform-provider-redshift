package provider

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCoverageDatabaseIntegrationReads(t *testing.T) {
	ctx := context.Background()
	client := testResourceClient(queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
		return []sqlclient.Row{{}, {}}, nil
	}))
	_, found, err := client.databaseIntegration(ctx, "replica")
	require.ErrorContains(t, err, "2 integrations")
	assert.False(t, found)
	observed := integrationState()
	observed.DatabaseType = types.StringValue(databaseTypeShared)
	require.ErrorContains(t, client.databaseReadIntegration(ctx, observed.Integration, &observed), "shared database")
	assert.Empty(t, alterDatabaseIntegrationStatements(databaseModel{}, databaseModel{}))
}

func TestCoverageDatabaseIntegrationWaitFailures(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(map[bool]string{false: "read error", true: "vanished"}[missing], func(t *testing.T) {
			failure := errors.New("catalog denied")
			client := queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
				if missing {
					return nil, nil
				}
				return nil, failure
			})
			r := &databaseResource{testResourceClient(client)}
			expected := integrationState()
			observed := integrationState()
			integration := *observed.Integration
			observed.Integration = &integration
			observed.Integration.RefreshInterval = types.Int64Value(1)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			err := r.waitDatabaseIntegration(ctx, expected, &observed)
			if missing {
				require.ErrorIs(t, err, errDatabaseVanished)
			} else {
				require.ErrorIs(t, err, failure)
			}
		})
	}
}

func TestCoverageDatabaseUnknownCollation(t *testing.T) {
	c := fullCatalog()
	c.localDB = true
	r := &databaseResource{testResourceClient(c)}
	data := databaseLocalOptionsModel("etl", 25, "CASE_INSENSITIVE", "SERIALIZABLE")
	data.ID = r.identity("admin", map[string]string{"name": "warehouse"})
	data.Collation = types.StringUnknown()
	found, err := r.read(context.Background(), &data)
	require.NoError(t, err)
	require.True(t, found)
	assert.True(t, data.Collation.IsNull())
	r = &databaseResource{testResourceClient(queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		if strings.Contains(strings.ToUpper(sql), "DB_COLLATION()") {
			return nil, errors.New("connection limit")
		}
		return c.Query(ctx, target, sql, parameters)
	}))}
	data.Collation = types.StringUnknown()
	data.DatabaseType = types.StringValue(databaseTypeLocal)
	var diagnostics diag.Diagnostics
	r.databaseReadCollation(context.Background(), &data, &diagnostics)
	assert.True(t, data.Collation.IsNull())
	assert.NotEmpty(t, diagnostics.Warnings())
}

func TestCoverageDatabaseMalformedValidation(t *testing.T) {
	r := newDatabaseResource()
	state := testState(t, r, databaseLocalOptionsModel("etl", 25, "CASE_INSENSITIVE", "SERIALIZABLE"))
	var response resource.ValidateConfigResponse
	r.(resource.ResourceWithValidateConfig).ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config{Schema: state.Schema, Raw: tftypes.NewValue(tftypes.Object{AttributeTypes: map[string]tftypes.Type{"connection_limit": tftypes.String}}, map[string]tftypes.Value{"connection_limit": tftypes.NewValue(tftypes.String, "invalid")})}}, &response)
	require.True(t, response.Diagnostics.HasError())
}

func TestCoverageDatabaseDisappearsDuringUpdate(t *testing.T) {
	c := fullCatalog()
	c.localDB = true
	r := &databaseResource{testResourceClient(queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		rows, err := c.Query(ctx, target, sql, parameters)
		if strings.HasPrefix(sql, "ALTER DATABASE") {
			c.localDB = false
		}
		return rows, err
	}))}
	prior := databaseLocalOptionsModel("admin", -1, "CASE_SENSITIVE", "SNAPSHOT")
	prior.ID = r.identity("admin", map[string]string{"name": "warehouse"})
	planned := prior
	planned.Owner = types.StringValue("etl")
	_, diagnostics := applyOperation(t, r, "update", prior, planned, nil)
	require.True(t, diagnostics.HasError())
	assert.Contains(t, diagnostics.Errors()[0].Detail(), errDatabaseVanished.Error())
}
