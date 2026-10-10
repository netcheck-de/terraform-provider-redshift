package provider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerLifecycleCase(lifecycleCase{name: "share grant", new: newDatashareGrantResource, model: datashareGrantModel{Database: types.StringValue("admin"), Datashare: types.StringValue("producer"), AccountID: types.StringValue("123456789012")}, absent: func(c *catalog) { c.shareGrant = false }})

var _ = registerReplacementPolicy("redshift_datashare_grant", map[string]replaceRule{
	"database":         replaceAlways,
	"datashare":        replaceAlways,
	"account_id":       replaceAlways,
	"namespace_id":     replaceAlways,
	"via_data_catalog": replaceConditional("TestDatashareGrantViaDataCatalogReplacement"),
})

// datashareCatalogGrant is an account grant to a Lake Formation account.
func datashareCatalogGrant() datashareGrantModel {
	return datashareGrantModel{Database: types.StringValue("admin"), Datashare: types.StringValue("producer"), AccountID: types.StringValue("123456789012"), NamespaceID: types.StringNull(), ViaDataCatalog: types.BoolValue(true)}
}

// TestDatashareGrantViaDataCatalogReplacement replaces the grant only when the effective form changes: null, recorded
// before the default existed, and false are the same plain account grant, while a value unknown until apply replaces
// because Update cannot switch forms.
func TestDatashareGrantViaDataCatalogReplacement(t *testing.T) {
	var response resource.SchemaResponse
	(&datashareGrantResource{}).Schema(context.Background(), resource.SchemaRequest{}, &response)
	attribute := response.Schema.Attributes["via_data_catalog"].(schema.BoolAttribute)
	for _, test := range []struct {
		state, plan types.Bool
		replace     bool
	}{
		{types.BoolNull(), types.BoolValue(false), false},
		{types.BoolValue(false), types.BoolNull(), false},
		{types.BoolNull(), types.BoolValue(true), true},
		{types.BoolValue(false), types.BoolValue(true), true},
		{types.BoolValue(true), types.BoolNull(), true},
		{types.BoolValue(true), types.BoolValue(false), true},
		{types.BoolNull(), types.BoolUnknown(), true},
		{types.BoolValue(false), types.BoolUnknown(), true},
		{types.BoolValue(true), types.BoolUnknown(), true},
	} {
		state := testState(t, &datashareGrantResource{}, datashareGrantModel{ViaDataCatalog: test.state})
		plan := testState(t, &datashareGrantResource{}, datashareGrantModel{ViaDataCatalog: test.plan})
		request := planmodifier.BoolRequest{Path: path.Root("via_data_catalog"), StateValue: test.state, PlanValue: test.plan, ConfigValue: test.plan, State: state, Plan: tfsdk.Plan(plan), Config: tfsdk.Config(plan)}
		replace := false
		for _, modifier := range attribute.PlanModifiers {
			var result planmodifier.BoolResponse
			modifier.PlanModifyBool(context.Background(), request, &result)
			require.False(t, result.Diagnostics.HasError(), "%v", result.Diagnostics)
			replace = replace || result.RequiresReplace
		}
		assert.Equal(t, test.replace, replace, "%v -> %v", test.state, test.plan)
	}
}

// TestDatashareGrantViaDataCatalogValidation accepts the Data Catalog form only for accounts, during planning and
// before Create records state.
func TestDatashareGrantViaDataCatalogValidation(t *testing.T) {
	r := &datashareGrantResource{testResourceClient(queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
		t.Fatalf("invalid consumer reached SQL: %s", sql)
		return nil, nil
	}))}
	for name, test := range map[string]struct {
		data    datashareGrantModel
		invalid bool
	}{
		"account":           {datashareCatalogGrant(), false},
		"namespace":         {datashareGrantModel{Database: types.StringValue("admin"), Datashare: types.StringValue("producer"), AccountID: types.StringNull(), NamespaceID: types.StringValue("12345678-1234-1234-1234-123456789abc"), ViaDataCatalog: types.BoolValue(true)}, true},
		"unknown":           {datashareGrantModel{Database: types.StringValue("admin"), Datashare: types.StringValue("producer"), AccountID: types.StringValue("123456789012"), NamespaceID: types.StringNull(), ViaDataCatalog: types.BoolUnknown()}, false},
		"unknown namespace": {datashareGrantModel{Database: types.StringValue("admin"), Datashare: types.StringValue("producer"), AccountID: types.StringNull(), NamespaceID: types.StringUnknown(), ViaDataCatalog: types.BoolValue(true)}, false},
		"neither":           {datashareGrantModel{Database: types.StringValue("admin"), Datashare: types.StringValue("producer"), AccountID: types.StringNull(), NamespaceID: types.StringNull(), ViaDataCatalog: types.BoolNull()}, true},
	} {
		t.Run(name, func(t *testing.T) {
			config := testState(t, r, test.data)
			var validated resource.ValidateConfigResponse
			r.ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config(config)}, &validated)
			assert.Equal(t, test.invalid, validated.Diagnostics.HasError(), "%v", validated.Diagnostics)
			if test.invalid {
				resp := resource.CreateResponse{State: tfsdk.State{Schema: config.Schema, Raw: tftypes.NewValue(config.Raw.Type(), nil)}}
				r.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan(config)}, &resp)
				require.True(t, resp.Diagnostics.HasError())
				assert.True(t, resp.State.Raw.IsNull(), "an invalid consumer must not be recorded in state")
			}
		})
	}
	var validated resource.ValidateConfigResponse
	r.ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config{Schema: testState(t, r, datashareCatalogGrant()).Schema, Raw: tftypes.NewValue(tftypes.String, "invalid")}}, &validated)
	assert.True(t, validated.Diagnostics.HasError())
}

// TestDatashareGrantViaDataCatalogLifecycle grants and revokes the Data Catalog form and records it in the identity.
func TestDatashareGrantViaDataCatalogLifecycle(t *testing.T) {
	c := &catalog{}
	var statements []string
	r := &datashareGrantResource{testResourceClient(queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		if !strings.HasPrefix(sql, "SELECT") {
			statements = append(statements, sql)
		}
		return c.Query(ctx, target, sql, parameters)
	}))}
	data := datashareCatalogGrant()
	state, diagnostics := applyOperation(t, r, "create", nil, data, nil)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	var created datashareGrantModel
	require.False(t, state.Get(context.Background(), &created).HasError())
	assert.Equal(t, r.identity("admin", map[string]string{"datashare": "producer", "account_id": "123456789012", "via_data_catalog": "true"}), created.ID)
	require.False(t, invoke(t, r, "delete", created, false).HasError())
	assert.Equal(t, []string{
		`GRANT USAGE ON DATASHARE "producer" TO ACCOUNT '123456789012' VIA DATA CATALOG`,
		`REVOKE USAGE ON DATASHARE "producer" FROM ACCOUNT '123456789012' VIA DATA CATALOG`,
	}, statements)
	assert.False(t, c.shareGrant)
}

// TestDatashareGrantViaDataCatalogImport restores the flag from the JSON identity and rejects other values.
func TestDatashareGrantViaDataCatalogImport(t *testing.T) {
	r := &datashareGrantResource{}
	for selector, valid := range map[string]bool{
		`"account_id":"123456789012","via_data_catalog":"true"`:                           true,
		`"account_id":"123456789012","via_data_catalog":"false"`:                          false,
		`"account_id":"123456789012","via_data_catalog":""`:                               false,
		`"namespace_id":"12345678-1234-1234-1234-123456789abc","via_data_catalog":"true"`: false,
	} {
		id := `{"workgroup_name":"warehouse","database":"admin","datashare":"producer",` + selector + `}`
		resp := resource.ImportStateResponse{State: testState(t, r, datashareGrantModel{})}
		r.ImportState(context.Background(), resource.ImportStateRequest{ID: id}, &resp)
		require.Equal(t, !valid, resp.Diagnostics.HasError(), "%s: %v", selector, resp.Diagnostics)
		if valid {
			var data datashareGrantModel
			require.False(t, resp.State.Get(context.Background(), &data).HasError())
			assert.Equal(t, types.BoolValue(true), data.ViaDataCatalog)
			assert.Equal(t, id, data.ID.ValueString())
		}
	}
}

// TestDatashareGrantUpdateRejectsFormSwitch refuses to record a switch between the account forms, which the
// catalog cannot verify, and accepts null state as the plain form.
func TestDatashareGrantUpdateRejectsFormSwitch(t *testing.T) {
	plain := datashareCatalogGrant()
	plain.ViaDataCatalog = types.BoolValue(false)
	legacy := plain
	legacy.ViaDataCatalog = types.BoolNull()
	for name, test := range map[string]struct {
		prior, planned datashareGrantModel
		invalid        bool
	}{
		"to data catalog": {plain, datashareCatalogGrant(), true},
		"to plain":        {datashareCatalogGrant(), plain, true},
		"null to false":   {legacy, plain, false},
	} {
		t.Run(name, func(t *testing.T) {
			r := &datashareGrantResource{testResourceClient(catalogWith()())}
			state, diagnostics := applyOperation(t, r, "update", test.prior, test.planned, test.planned)
			assert.Equal(t, test.invalid, diagnostics.HasError(), "%v", diagnostics)
			if !test.invalid {
				var updated datashareGrantModel
				require.False(t, state.Get(context.Background(), &updated).HasError())
				assert.Equal(t, types.BoolValue(false), updated.ViaDataCatalog)
			}
		})
	}
}

// TestDatashareGrantViaDataCatalogTranscripts records the Data Catalog form beside the lifecycle transcripts.
func TestDatashareGrantViaDataCatalogTranscripts(t *testing.T) {
	data := datashareCatalogGrant()
	runTranscripts(t, "lifecycle/datashare_grant_via_data_catalog", newDatashareGrantResource, []transcriptCase{
		{name: "create", operation: "create", catalog: catalogWith(func(c *catalog) { c.shareGrant = false }), planned: data},
		{name: "read", operation: "read", catalog: catalogWith(), prior: data},
		{name: "delete", operation: "delete", catalog: catalogWith(), prior: data},
		{name: "import", operation: "import", catalog: catalogWith(func(c *catalog) { c.shareGrant = false }), planned: data},
	})
}

// TestDatashareAccountGrant checks explicit SQL usage for a consumer account.
func TestDatashareAccountGrant(t *testing.T) {
	c := &catalog{shareGrant: true}
	r := &datashareGrantResource{testResourceClient(c)}
	data := datashareGrantModel{Database: types.StringValue("admin"), Datashare: types.StringValue("producer"), AccountID: types.StringValue("123456789012")}
	found, err := r.read(context.Background(), data)
	require.NoError(t, err)
	assert.True(t, found)
	c.shareGrant = false
	found, err = r.read(context.Background(), data)
	require.NoError(t, err)
	assert.False(t, found)
}

// TestDatashareGrantSelectorValidation checks exclusivity and deferred unknown namespace validation.
func TestDatashareGrantSelectorValidation(t *testing.T) {
	r := &datashareGrantResource{}
	var response resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &response)
	for _, test := range []struct {
		// name identifies the validation case.
		name string
		// account is the configured AWS account selector.
		account types.String
		// namespace is the configured Redshift namespace selector.
		namespace types.String
		// invalid records whether configuration must be rejected.
		invalid bool
	}{
		{"account", types.StringValue("123456789012"), types.StringNull(), false},
		{"namespace", types.StringNull(), types.StringValue("12345678-1234-1234-1234-123456789abc"), false},
		{"unknown_namespace", types.StringNull(), types.StringUnknown(), false},
		{"neither", types.StringNull(), types.StringNull(), true},
		{"both", types.StringValue("123456789012"), types.StringValue("12345678-1234-1234-1234-123456789abc"), true},
		{"invalid_account", types.StringValue("123"), types.StringNull(), true},
		{"invalid_namespace", types.StringNull(), types.StringValue("arn:aws:redshift:namespace"), true},
		{"empty_account", types.StringValue(""), types.StringNull(), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := testState(t, r, datashareGrantModel{AccountID: test.account, NamespaceID: test.namespace})
			config := tfsdk.Config{Schema: response.Schema, Raw: state.Raw}
			invalid := false
			for field, value := range map[string]types.String{"account_id": test.account, "namespace_id": test.namespace} {
				attribute := response.Schema.Attributes[field].(schema.StringAttribute)
				assert.True(t, attribute.Optional)
				assert.NotEmpty(t, attribute.PlanModifiers)
				for _, validation := range attribute.Validators {
					var result validator.StringResponse
					validation.ValidateString(context.Background(), validator.StringRequest{Path: path.Root(field), PathExpression: path.MatchRoot(field), Config: config, ConfigValue: value}, &result)
					invalid = invalid || result.Diagnostics.HasError()
				}
			}
			assert.Equal(t, test.invalid, invalid)
		})
	}
}

// TestDatashareGrantReadSeparatesConsumerScopes checks a different explicit scope never satisfies the selector.
func TestDatashareGrantReadSeparatesConsumerScopes(t *testing.T) {
	for _, namespace := range []bool{false, true} {
		data := datashareGrantModel{Database: types.StringValue("admin"), Datashare: types.StringValue("producer")}
		if namespace {
			data.NamespaceID = types.StringValue("12345678-1234-1234-1234-123456789abc")
		} else {
			data.AccountID = types.StringValue("123456789012")
		}
		r := &datashareGrantResource{testResourceClient(&catalog{shareGrant: namespace, shareNamespaceGrant: !namespace})}
		found, err := r.read(context.Background(), data)
		require.NoError(t, err)
		assert.False(t, found)
	}
}

// TestDatashareGrantLifecycle checks SQL targets, identity stability, refresh, and independent deletion.
func TestDatashareGrantLifecycle(t *testing.T) {
	for _, namespace := range []bool{false, true} {
		t.Run(map[bool]string{false: "account", true: "namespace"}[namespace], func(t *testing.T) {
			c := &catalog{shareGrant: namespace, shareNamespaceGrant: !namespace}
			data := datashareGrantModel{Database: types.StringValue("admin"), Datashare: types.StringValue("producer"), AccountID: types.StringNull(), NamespaceID: types.StringNull()}
			kind, field, value := "ACCOUNT", "account_id", "123456789012"
			if namespace {
				kind, field, value = "NAMESPACE", "namespace_id", "12345678-1234-1234-1234-123456789abc"
				data.NamespaceID = types.StringValue(value)
			} else {
				data.AccountID = types.StringValue(value)
			}
			r := &datashareGrantResource{testResourceClient(queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
				if strings.HasPrefix(sql, "GRANT ") {
					assert.Equal(t, `GRANT USAGE ON DATASHARE "producer" TO `+kind+" '"+value+"'", sql)
				}
				if strings.HasPrefix(sql, "REVOKE ") {
					assert.Equal(t, `REVOKE USAGE ON DATASHARE "producer" FROM `+kind+" '"+value+"'", sql)
				}
				return c.Query(ctx, target, sql, parameters)
			}))}
			require.False(t, invoke(t, r, "create", data, false).HasError())
			data.ID = r.identity("admin", map[string]string{"datashare": "producer", field: value})
			require.False(t, invoke(t, r, "read", data, false).HasError())
			require.False(t, invoke(t, r, "update", data, false).HasError())
			require.False(t, invoke(t, r, "delete", data, false).HasError())
			assert.Equal(t, namespace, c.shareGrant)
			assert.Equal(t, !namespace, c.shareNamespaceGrant)
			require.False(t, invoke(t, r, "read", data, false).HasError())
			assert.True(t, invoke(t, r, "update", data, false).HasError())
			require.False(t, invoke(t, r, "delete", data, false).HasError())
		})
	}
}

// TestDatashareGrantImport checks both JSON identities and invalid selectors.
func TestDatashareGrantImport(t *testing.T) {
	r := &datashareGrantResource{}
	var schema resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &schema)
	for _, selector := range []string{`"account_id":"123456789012"`, `"namespace_id":"12345678-1234-1234-1234-123456789abc"`, ``, `"account_id":"123456789012","namespace_id":"12345678-1234-1234-1234-123456789abc"`, `"account_id":"bad"`, `"namespace_id":"bad"`, `"namespace_id":""`} {
		id := `{"workgroup_name":"warehouse","database":"admin","datashare":"producer"`
		if selector != "" {
			id += "," + selector
		}
		id += "}"
		resp := resource.ImportStateResponse{State: testState(t, r, datashareGrantModel{})}
		r.ImportState(context.Background(), resource.ImportStateRequest{ID: id}, &resp)
		valid := selector == `"account_id":"123456789012"` || selector == `"namespace_id":"12345678-1234-1234-1234-123456789abc"`
		require.Equal(t, !valid, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
		if valid {
			var data datashareGrantModel
			require.False(t, resp.State.Get(context.Background(), &data).HasError())
			assert.Equal(t, id, data.ID.ValueString())
			// The default records false, so a configuration writing false plans no change after import.
			assert.Equal(t, types.BoolValue(false), data.ViaDataCatalog)
			_, _, _, err := data.consumer()
			require.NoError(t, err)
		}
	}
	resp := resource.ImportStateResponse{State: tfsdk.State{Schema: schema.Schema}}
	r.ImportState(context.Background(), resource.ImportStateRequest{ID: "invalid"}, &resp)
	assert.True(t, resp.Diagnostics.HasError())
}

// TestDatashareGrantFailures checks transport failures and failed mutation verification.
func TestDatashareGrantFailures(t *testing.T) {
	data := datashareGrantModel{Database: types.StringValue("admin"), Datashare: types.StringValue("producer"), AccountID: types.StringValue("123456789012")}
	for _, operation := range []string{"create", "read", "update", "delete"} {
		r := &datashareGrantResource{testResourceClient(queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
			return nil, errors.New("API unavailable")
		}))}
		assert.True(t, invoke(t, r, operation, data, false).HasError(), operation)
	}
	for _, failure := range []string{"absent", "verify", "revoke", "remains"} {
		reads := 0
		r := &datashareGrantResource{testResourceClient(queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
			if strings.HasPrefix(sql, "SELECT") {
				reads++
				if failure == "absent" {
					return nil, nil
				}
				if failure == "verify" || (failure == "revoke" && reads > 1) {
					return nil, errors.New("verification failed")
				}
				return []sqlclient.Row{{"consumer_account": "123456789012"}}, nil
			}
			if failure == "revoke" {
				return nil, errors.New("revoke failed")
			}
			return nil, nil
		}))}
		operation := "delete"
		if failure == "absent" || failure == "verify" {
			operation = "create"
		}
		assert.True(t, invoke(t, r, operation, data, false).HasError(), failure)
	}
}

// TestDatashareGrantRejectsInvalidConsumers ensures invalid and unresolved identities never execute SQL.
func TestDatashareGrantRejectsInvalidConsumers(t *testing.T) {
	for _, data := range []datashareGrantModel{
		{}, {AccountID: types.StringValue("123456789012"), NamespaceID: types.StringValue("12345678-1234-1234-1234-123456789abc")},
		{AccountID: types.StringValue("bad")}, {NamespaceID: types.StringValue("bad")}, {AccountID: types.StringUnknown()}, {NamespaceID: types.StringUnknown()},
	} {
		r := &datashareGrantResource{testResourceClient(queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
			t.Fatal("invalid consumer reached SQL")
			return nil, nil
		}))}
		assert.True(t, invoke(t, r, "create", data, false).HasError())
		_, err := r.read(context.Background(), data)
		require.Error(t, err)
	}
}
