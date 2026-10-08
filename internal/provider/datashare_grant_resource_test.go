package provider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
