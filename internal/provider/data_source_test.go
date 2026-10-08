package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readSource configures a data source and reads a typed model through a fake SQL client.
func readSource(t *testing.T, source datasource.DataSource, model any, client dataapi.Client, lookups ...func(context.Context, shareSource) (string, error)) (tfsdk.State, diag.Diagnostics) {
	t.Helper()
	ctx := context.Background()
	var schema datasource.SchemaResponse
	source.Schema(ctx, datasource.SchemaRequest{}, &schema)
	config := tfsdk.State{Schema: schema.Schema}
	require.False(t, config.Set(ctx, model).HasError())
	var configured datasource.ConfigureResponse
	lookup := testDatashareARN
	if len(lookups) > 0 {
		lookup = lookups[0]
	}
	source.(datasource.DataSourceWithConfigure).Configure(ctx, datasource.ConfigureRequest{ProviderData: providerData{client: client, warehouse: warehouseBinding{field: "workgroup_name", value: types.StringValue("warehouse")}, database: types.StringValue("admin"), datashareARN: lookup}}, &configured)
	require.False(t, configured.Diagnostics.HasError())
	resp := datasource.ReadResponse{State: tfsdk.State{Schema: schema.Schema}}
	source.Read(ctx, datasource.ReadRequest{Config: tfsdk.Config{Schema: schema.Schema, Raw: config.Raw}}, &resp)
	return resp.State, resp.Diagnostics
}

// TestDataSourceClientConfiguration checks missing and incorrectly typed provider data.
func TestDataSourceClientConfiguration(t *testing.T) {
	d := &dataSourceClient{}
	var resp datasource.ConfigureResponse
	d.Configure(context.Background(), datasource.ConfigureRequest{}, &resp)
	assert.False(t, resp.Diagnostics.HasError())
	d.Configure(context.Background(), datasource.ConfigureRequest{ProviderData: 42}, &resp)
	assert.True(t, resp.Diagnostics.HasError())
}

// TestObjectLookupsReportMissingAndAPIError checks absent objects and SQL failures for all lookups.
func TestObjectLookupsReportMissingAndAPIError(t *testing.T) {
	for _, test := range []struct {
		name  string
		new   func() datasource.DataSource
		model any
	}{
		{"role", newRoleDataSource, roleData{Name: types.StringValue("missing")}},
		{"group", newGroupDataSource, groupData{Name: types.StringValue("missing")}},
		{"user", newUserDataSource, userData{Name: types.StringValue("missing")}},
		{"identity", newIdentityProviderDataSource, identityProviderData{Name: types.StringValue("missing")}},
		{"datashare", newDatashareDataSource, datashareData{Name: types.StringValue("missing"), Database: types.StringValue("admin")}},
		{"database", newDatabaseDataSource, databaseData{Name: types.StringValue("missing")}},
		{"schema", newSchemaDataSource, schemaData{Database: types.StringValue("admin"), Name: types.StringValue("missing")}},
		{"external schema", newExternalSchemaDataSource, externalSchemaData{Database: types.StringValue("admin"), Name: types.StringValue("missing")}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, diagnostics := readSource(t, test.new(), test.model, queryFunc(func(context.Context, dataapi.Connection, string, map[string]string) ([]dataapi.Row, error) {
				return nil, nil
			}))
			assert.True(t, diagnostics.HasError(), "missing object must be reported")
			_, diagnostics = readSource(t, test.new(), test.model, queryFunc(func(context.Context, dataapi.Connection, string, map[string]string) ([]dataapi.Row, error) {
				return nil, errors.New("API unavailable")
			}))
			assert.True(t, diagnostics.HasError(), "catalog failure must be reported")
		})
	}
}

// TestObjectLookupsRejectInvalidConfiguration rejects malformed Terraform models before SQL execution.
func TestObjectLookupsRejectInvalidConfiguration(t *testing.T) {
	for _, source := range []datasource.DataSource{newGroupDataSource(), newRoleDataSource(), newUserDataSource(), newIdentityProviderDataSource(), newDatashareDataSource(), newDatabaseDataSource(), newSchemaDataSource(), newExternalSchemaDataSource()} {
		var schema datasource.SchemaResponse
		source.Schema(context.Background(), datasource.SchemaRequest{}, &schema)
		resp := datasource.ReadResponse{State: tfsdk.State{Schema: schema.Schema}}
		source.Read(context.Background(), datasource.ReadRequest{Config: tfsdk.Config{Schema: schema.Schema, Raw: tftypes.NewValue(tftypes.String, "bad config")}}, &resp)
		assert.True(t, resp.Diagnostics.HasError())
	}
}
