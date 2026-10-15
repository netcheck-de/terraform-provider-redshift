package provider

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerLifecycleCase(lifecycleCase{name: "identity", new: newIdentityProviderResource, model: identityProviderModel{Name: types.StringValue("identity"), Namespace: types.StringValue("example"), ApplicationARN: types.StringValue("application"), IAMRoleARN: types.StringValue("role-one"), Enabled: types.BoolValue(true)}, absent: func(c *catalog) { c.identity = false }, dependents: func(c *catalog) { c.role = false }})

var _ = registerReplacementPolicy("redshift_identity_provider", map[string]replaceRule{
	"name":                             replaceAlways,
	"type":                             replaceAlways,
	"namespace":                        replaceNever,
	"application_arn":                  replaceAlways,
	"iam_role_arn":                     replaceNever,
	"issuer":                           replaceNever,
	"client_id":                        replaceNever,
	"audience":                         replaceNever,
	"client_secret_wo":                 replaceNever,
	"client_secret_wo_version":         replaceNever,
	"auto_create_roles":                replaceNever,
	"auto_create_roles_include_groups": replaceNever,
	"auto_create_roles_exclude_groups": replaceNever,
	"enabled":                          replaceNever,
})

var _ = registerValidateConfigCase("identity_provider", validateConfigCase{
	new:     newIdentityProviderResource,
	valid:   identityProviderAzureFixture("oauth_standard", "aad", "https://issuer.example/"),
	invalid: identityProviderModel{Name: types.StringValue("idp"), Type: types.StringValue(identityProviderAzure), Namespace: types.StringValue("aad"), ApplicationARN: types.StringValue("application"), Issuer: types.StringValue("https://issuer.example/"), ClientID: types.StringValue("client"), ClientSecretVersion: types.Int64Value(1), Enabled: types.BoolValue(true)},
	unknown: identityProviderModel{Name: types.StringValue("idp"), Type: types.StringValue(identityProviderAzure), Namespace: types.StringValue("aad"), ApplicationARN: types.StringUnknown(), Issuer: types.StringValue("https://issuer.example/"), ClientID: types.StringValue("client"), ClientSecretVersion: types.Int64Value(1), Enabled: types.BoolValue(true)},
})

// identityProviderAzureCatalog returns a fake holding an Azure provider created with the fixture's parameters.
func identityProviderAzureCatalog(existing bool) *catalog {
	c := &catalog{}
	if existing {
		c.identity, c.enabled = true, true
		family := fakeState[*identityProviderFakeFamily](c, "identity_provider")
		family.azure, family.namespace = true, "aad"
		family.parameters = `{"issuer":"https://sts.windows.net/tenant/", "client_id":"87f4aa26-78b7-410e-bf29-57b39929ef9a", "client_secret":, "audience":["https://analysis.windows.net/powerbi/connector/AmazonRedshift"]}`
	}
	return c
}

// identityProviderWithSecret returns the configuration of model, which carries the write-only secret.
func identityProviderWithSecret(model identityProviderModel, secret string) identityProviderModel {
	model.ClientSecret = types.StringValue(secret)
	return model
}

// TestIdentityProviderObservesMutableAttributes checks integration role, enabled-state, and catalog detail refresh.
func TestIdentityProviderObservesMutableAttributes(t *testing.T) {
	c := &catalog{identity: true, iamRole: "role-one", enabled: true}
	r := &identityProviderResource{testResourceClient(c)}
	data := identityProviderModel{Name: types.StringValue("identity")}
	found, err := r.read(context.Background(), &data)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "role-one", data.IAMRoleARN.ValueString())
	assert.True(t, data.Enabled.ValueBool())
	assert.Equal(t, identityProviderAWSIDC, data.Type.ValueString())
	assert.Equal(t, int64(126692), data.ProviderID.ValueInt64())
	assert.Equal(t, "application", data.InstanceID.ValueString())
	assert.Equal(t, "arn:aws:sso:::instance/ssoins-1234567890abcdef", data.IdentityCenterInstanceARN.ValueString())
	assert.True(t, data.Issuer.IsNull())
	c.iamRole, c.enabled = "role-two", false
	found, err = r.read(context.Background(), &data)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "role-two", data.IAMRoleARN.ValueString())
	assert.False(t, data.Enabled.ValueBool())
}

// TestIdentityProviderReadsAzureParameters decodes Azure parameters with the redacted secret in both printed forms.
func TestIdentityProviderReadsAzureParameters(t *testing.T) {
	for name, params := range map[string]string{
		"view": `{"issuer":"https://login.microsoftonline.com/t/v2.0", "client_id":"871c", "client_secret":, "audience":["b", "a"]}`,
		"desc": `{"issuer":"https://login.microsoftonline.com/t/v2.0", "client_id":"871c", "client_secret":'', "audience":["b", "a"]}`,
		"last": `{"issuer":"https://login.microsoftonline.com/t/v2.0", "client_id":"871c", "audience":["b", "a"], "client_secret": }`,
	} {
		t.Run(name, func(t *testing.T) {
			r := &identityProviderResource{testResourceClient(queryFunc(func(context.Context, dataapi.Connection, string, map[string]string) ([]dataapi.Row, error) {
				return []dataapi.Row{{"name": "azure_idp", "type": "azure", "instanceid": "e40d", "namespc": "aad", "params": params, "enabled": "t", "uid": "126692"}}, nil
			}))}
			data := identityProviderModel{Name: types.StringValue("azure_idp")}
			found, err := r.read(context.Background(), &data)
			require.NoError(t, err)
			require.True(t, found)
			assert.Equal(t, "AZURE", data.Type.ValueString(), "an import adopts the canonical uppercase type")
			data.Type = types.StringValue("azure")
			_, err = r.read(context.Background(), &data)
			require.NoError(t, err)
			assert.Equal(t, "azure", data.Type.ValueString(), "a refresh keeps the configured case")
			assert.Equal(t, "https://login.microsoftonline.com/t/v2.0", data.Issuer.ValueString())
			assert.Equal(t, "871c", data.ClientID.ValueString())
			assert.Equal(t, []string{"a", "b"}, data.Audience)
			assert.Equal(t, "e40d", data.InstanceID.ValueString())
			assert.True(t, data.ApplicationARN.IsNull())
			assert.True(t, data.IAMRoleARN.IsNull())
			assert.True(t, data.IdentityCenterInstanceARN.IsNull())
		})
	}
}

// TestIdentityProviderKeepsFederatedUsers prevents deleting integrations with unmanaged users.
func TestIdentityProviderKeepsFederatedUsers(t *testing.T) {
	writes := 0
	r := &identityProviderResource{testResourceClient(queryFunc(func(_ context.Context, _ dataapi.Connection, sql string, _ map[string]string) ([]dataapi.Row, error) {
		switch {
		case strings.HasPrefix(sql, "SELECT name"):
			return []dataapi.Row{{"name": "identity", "type": "awsidc", "namespc": "example", "instanceid": "application", "params": `{"iam_role":"role"}`, "enabled": "true"}}, nil
		case strings.HasPrefix(sql, "SELECT usename"):
			return []dataapi.Row{{"usename": "example:user"}}, nil
		default:
			writes++
			return nil, nil
		}
	}))}
	data := identityProviderModel{Name: types.StringValue("identity"), Namespace: types.StringValue("example"), ApplicationARN: types.StringValue("application"), IAMRoleARN: types.StringValue("role"), Enabled: types.BoolValue(true)}
	diagnostics := invoke(t, r, "delete", data, false)
	assert.True(t, diagnostics.HasError(), "%v", diagnostics)
	assert.Zero(t, writes, "federated provider must not be dropped")
}

// TestIdentityProviderRejectsInvalidCatalog checks catalog identity and option validation for both types.
func TestIdentityProviderRejectsInvalidCatalog(t *testing.T) {
	for name, change := range map[string]map[string]string{
		"provider type":       {"type": "okta"},
		"azure without param": {"type": "azure"},
		"parameters":          {"params": "invalid JSON"},
		"IAM role":            {"params": "{}"},
		"enabled flag":        {"enabled": "invalid"},
		"namespace":           {"namespc": ""},
		"application":         {"instanceid": ""},
		"uid":                 {"uid": "x"},
	} {
		t.Run(name, func(t *testing.T) {
			row := dataapi.Row{"name": "identity", "type": "awsidc", "namespc": "example", "instanceid": "application", "params": `{"iam_role":"role-one"}`, "enabled": "true", "uid": "1"}
			maps.Copy(row, change)
			r := &identityProviderResource{testResourceClient(queryFunc(func(context.Context, dataapi.Connection, string, map[string]string) ([]dataapi.Row, error) {
				return []dataapi.Row{row}, nil
			}))}
			data := identityProviderModel{Name: types.StringValue("identity")}
			_, err := r.read(context.Background(), &data)
			require.Error(t, err)
		})
	}
}

// TestIdentityProviderRejectsDuplicateRows prevents selecting an arbitrary integration binding.
func TestIdentityProviderRejectsDuplicateRows(t *testing.T) {
	row := dataapi.Row{"name": "identity", "type": "awsidc", "namespc": "example", "instanceid": "application", "params": `{"iam_role":"role-one"}`, "enabled": "true"}
	r := &identityProviderResource{testResourceClient(queryFunc(func(context.Context, dataapi.Connection, string, map[string]string) ([]dataapi.Row, error) {
		return []dataapi.Row{row, row}, nil
	}))}
	_, err := r.read(context.Background(), &identityProviderModel{Name: types.StringValue("identity")})
	require.ErrorContains(t, err, "ambiguous catalog metadata")
}

// TestIdentityProviderRefusesChangedIdentityOnDelete prevents cleanup of a rebound integration.
func TestIdentityProviderRefusesChangedIdentityOnDelete(t *testing.T) {
	for _, field := range []string{"namespace", "application"} {
		t.Run(field, func(t *testing.T) {
			c := &catalog{identity: true, enabled: true, iamRole: "role-one"}
			r := &identityProviderResource{testResourceClient(c)}
			data := identityProviderModel{Name: types.StringValue("identity"), Namespace: types.StringValue("example"), ApplicationARN: types.StringValue("application"), IAMRoleARN: types.StringValue("role-one"), Enabled: types.BoolValue(true)}
			if field == "namespace" {
				data.Namespace = types.StringValue("previous")
			} else {
				data.ApplicationARN = types.StringValue("previous")
			}
			diagnostics := invoke(t, r, "delete", data, false)
			assert.True(t, diagnostics.HasError())
			assert.Empty(t, c.writes)
		})
	}
}

// TestIdentityProviderCreatesDisabled verifies an explicitly disabled integration is created correctly.
func TestIdentityProviderCreatesDisabled(t *testing.T) {
	for _, failAt := range []int{0, 2} {
		t.Run(fmt.Sprint(failAt), func(t *testing.T) {
			c := &catalog{}
			calls := 0
			client := queryFunc(func(ctx context.Context, target dataapi.Connection, sql string, parameters map[string]string) ([]dataapi.Row, error) {
				calls++
				if calls == failAt {
					return nil, fmt.Errorf("disable failed")
				}
				return c.Query(ctx, target, sql, parameters)
			})
			r := &identityProviderResource{testResourceClient(client)}
			data := identityProviderModel{Name: types.StringValue("identity"), Namespace: types.StringValue("example"), ApplicationARN: types.StringValue("application"), IAMRoleARN: types.StringValue("role-one"), Enabled: types.BoolValue(false)}
			diagnostics := invoke(t, r, "create", data, false)
			assert.Equal(t, failAt != 0, diagnostics.HasError(), "%v", diagnostics)
			if failAt == 0 {
				assert.False(t, c.enabled)
			}
		})
	}
}

// TestIdentityProviderUpdateNamesFailedStatement keeps the role, option, and status failures apart, so a refused
// update says which setting Redshift rejected.
func TestIdentityProviderUpdateNamesFailedStatement(t *testing.T) {
	for suffix, summary := range map[string]string{"IAM_ROLE 'role-two'": "Update identity provider role", "NAMESPACE 'renamed'": "Update identity provider", `"identity" DISABLE`: "Update identity provider status"} {
		t.Run(summary, func(t *testing.T) {
			c := &catalog{identity: true, iamRole: "role-one", enabled: true}
			client := queryFunc(func(ctx context.Context, target dataapi.Connection, sql string, parameters map[string]string) ([]dataapi.Row, error) {
				if strings.HasSuffix(sql, suffix) {
					return nil, fmt.Errorf("ALTER denied")
				}
				return c.Query(ctx, target, sql, parameters)
			})
			r := newIdentityProviderResource()
			configureTestResource(t, r, client)
			prior := identityProviderAWSIDCFixture("identity", "example", "role-two", false)
			prior.ID = r.(*identityProviderResource).identity("admin", map[string]string{"name": "identity"})
			planned := prior
			planned.Namespace = types.StringValue("renamed")
			_, diagnostics := applyOperation(t, r, "update", prior, planned, nil)
			require.True(t, diagnostics.HasError())
			assert.Equal(t, summary, diagnostics.Errors()[0].Summary())
		})
	}
}

// TestIdentityProviderAzureLifecycle creates, re-parameterizes, rotates, and deletes an Azure provider, and refuses
// changes that would drop the secret.
func TestIdentityProviderAzureLifecycle(t *testing.T) {
	azure := identityProviderAzureFixture("oauth_standard", "aad", "https://sts.windows.net/tenant/")
	ctx := context.Background()

	c := identityProviderAzureCatalog(false)
	r := newIdentityProviderResource()
	configureTestResource(t, r, c)
	_, diagnostics := applyOperation(t, r, "create", nil, azure, nil)
	require.True(t, diagnostics.HasError(), "creation needs the secret from configuration")
	assert.Empty(t, c.writes)
	state, diagnostics := applyOperation(t, r, "create", nil, azure, identityProviderWithSecret(azure, "first"))
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	family := fakeState[*identityProviderFakeFamily](c, "identity_provider")
	assert.Equal(t, "first", family.secret)
	var created identityProviderModel
	require.False(t, state.Get(ctx, &created).HasError())
	assert.True(t, created.ClientSecret.IsNull(), "the secret never reaches state")
	assert.Equal(t, int64(126692), created.ProviderID.ValueInt64())

	rotated := created
	rotated.ClientSecretVersion = types.Int64Value(2)
	_, diagnostics = applyOperation(t, r, "update", created, rotated, nil)
	require.True(t, diagnostics.HasError(), "a rotation needs the secret from configuration")
	_, diagnostics = applyOperation(t, r, "update", created, rotated, identityProviderWithSecret(rotated, "second"))
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.Equal(t, "second", family.secret)

	reissued := rotated
	reissued.Issuer = types.StringValue("https://login.microsoftonline.com/tenant/v2.0")
	_, diagnostics = applyOperation(t, r, "update", rotated, reissued, identityProviderWithSecret(reissued, "second"))
	require.False(t, diagnostics.HasError(), "%v", diagnostics)

	diagnostics = invoke(t, r, "delete", reissued, false)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.False(t, c.identity)
}

// TestIdentityProviderUpdateVerifiesParameters fails when the catalog keeps the previous parameters.
func TestIdentityProviderUpdateVerifiesParameters(t *testing.T) {
	c := identityProviderAzureCatalog(true)
	client := queryFunc(func(ctx context.Context, target dataapi.Connection, sql string, parameters map[string]string) ([]dataapi.Row, error) {
		if strings.Contains(sql, " PARAMETERS ") {
			return nil, nil // An acknowledged change that did not converge.
		}
		return c.Query(ctx, target, sql, parameters)
	})
	r := newIdentityProviderResource()
	configureTestResource(t, r, client)
	prior := identityProviderAzureFixture("oauth_standard", "aad", "https://sts.windows.net/tenant/")
	planned := prior
	planned.ClientID = types.StringValue("other")
	_, diagnostics := applyOperation(t, r, "update", prior, planned, identityProviderWithSecret(planned, "secret"))
	require.True(t, diagnostics.HasError())
	assert.Equal(t, "Verify identity provider", diagnostics.Errors()[0].Summary())
}

// TestIdentityProviderRejectsInvalidPlan reports type-specific attribute mistakes before any SQL runs.
func TestIdentityProviderRejectsInvalidPlan(t *testing.T) {
	c := &catalog{identity: true, iamRole: "role-one", enabled: true}
	r := newIdentityProviderResource()
	configureTestResource(t, r, c)
	invalid := identityProviderAWSIDCFixture("identity", "example", "role-one", true)
	invalid.Issuer = types.StringValue("https://issuer.example/")
	_, diagnostics := applyOperation(t, r, "create", nil, invalid, nil)
	require.True(t, diagnostics.HasError())
	_, diagnostics = applyOperation(t, r, "update", identityProviderAWSIDCFixture("identity", "example", "role-one", true), invalid, nil)
	require.True(t, diagnostics.HasError())
	assert.Empty(t, c.writes)
}

// TestIdentityProviderAlterCoverage checks the in-place attributes: namespace and role creation have diff steps, while
// the IAM role and status are reapplied on every update and the Azure parameters are rendered together.
func TestIdentityProviderAlterCoverage(t *testing.T) {
	assertAlterCoverage(t, newIdentityProviderResource(), identityProviderAlterSteps,
		"iam_role_arn", "enabled", "issuer", "client_id", "audience", "client_secret_wo", "client_secret_wo_version",
		"auto_create_roles_include_groups", "auto_create_roles_exclude_groups")
}

// TestIdentityProviderTranscripts records the Azure and role-creation flows beyond the AWSIDC lifecycle case.
func TestIdentityProviderTranscripts(t *testing.T) {
	azure := identityProviderAzureFixture("oauth_standard", "aad", "https://sts.windows.net/tenant/")
	reissued := azure
	reissued.Issuer, reissued.ClientSecretVersion = types.StringValue("https://login.microsoftonline.com/tenant/v2.0"), types.Int64Value(2)
	renamed := azure
	renamed.Namespace, renamed.AutoCreateRoles, renamed.IncludeGroups = types.StringValue("entra"), types.BoolValue(true), types.StringValue("finance_%")
	fake := func(existing bool) func() dataapi.Client {
		return func() dataapi.Client { return identityProviderAzureCatalog(existing) }
	}
	runTranscripts(t, "lifecycle/identity_provider_azure", newIdentityProviderResource, []transcriptCase{
		{name: "create", operation: "create", catalog: fake(false), planned: azure, config: identityProviderWithSecret(azure, "secret")},
		{name: "read", operation: "read", catalog: fake(true), prior: azure},
		{name: "update_parameters", operation: "update", catalog: fake(true), prior: azure, planned: reissued, config: identityProviderWithSecret(reissued, `rotated'\secret`)},
		{name: "update_namespace_and_role_creation", operation: "update", catalog: fake(true), prior: azure, planned: renamed},
		{name: "delete", operation: "delete", catalog: fake(true), prior: azure},
		{name: "import", operation: "import", catalog: fake(false), planned: azure, config: identityProviderWithSecret(azure, "secret")},
	})
}

// TestIdentityProviderNamespaceChangeKeepsFederatedUsers refuses a namespace change while users carry the previous
// prefix, because Redshift keeps their names and Delete would then look for the new prefix only.
func TestIdentityProviderNamespaceChangeKeepsFederatedUsers(t *testing.T) {
	c := &catalog{identity: true, iamRole: "role-one", enabled: true}
	family := fakeState[*identityProviderFakeFamily](c, "identity_provider")
	family.federated = []string{"example:bob"}
	r := newIdentityProviderResource()
	configureTestResource(t, r, c)
	prior := identityProviderAWSIDCFixture("identity", "example", "role-one", true)
	prior.ID = r.(*identityProviderResource).identity("admin", map[string]string{"name": "identity"})
	planned := prior
	planned.Namespace = types.StringValue("entra")
	_, diagnostics := applyOperation(t, r, "update", prior, planned, nil)
	require.True(t, diagnostics.HasError())
	assert.Equal(t, "Update identity provider", diagnostics.Errors()[0].Summary())
	assert.Contains(t, diagnostics.Errors()[0].Detail(), `"example:"`)
	assert.Empty(t, c.writes)
	assert.Empty(t, family.namespace, "the namespace stays unchanged")

	// Users under another prefix, including the new one, do not block the change.
	family.federated = []string{"entra:bob", "examples:alice"}
	_, diagnostics = applyOperation(t, r, "update", prior, planned, nil)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.Equal(t, "entra", family.namespace)
}

// identityProviderValidateConfig runs ValidateConfig on the configuration of model after replacing the named
// attributes with unknown values, which the model cannot express for the audience set or the write-only secret.
func identityProviderValidateConfig(t *testing.T, model identityProviderModel, unknown ...string) diag.Diagnostics {
	t.Helper()
	r := newIdentityProviderResource()
	config := tfsdk.Config(testState(t, r, model))
	raw, err := tftypes.Transform(config.Raw, func(at *tftypes.AttributePath, value tftypes.Value) (tftypes.Value, error) {
		for _, name := range unknown {
			if at.Equal(tftypes.NewAttributePath().WithAttributeName(name)) {
				return tftypes.NewValue(value.Type(), tftypes.UnknownValue), nil
			}
		}
		return value, nil
	})
	require.NoError(t, err)
	config.Raw = raw
	var resp resource.ValidateConfigResponse
	r.(resource.ResourceWithValidateConfig).ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: config}, &resp)
	return resp.Diagnostics
}

// TestIdentityProviderValidateConfigWithUnknownValues checks that an unknown value defers only the checks that read it,
// and that the write-only secret and its version are rejected for awsidc at plan time.
func TestIdentityProviderValidateConfigWithUnknownValues(t *testing.T) {
	awsidc := identityProviderAWSIDCFixture("identity", "example", "role-one", true)
	azure := identityProviderAzureFixture("oauth_standard", "aad", "https://issuer.example/")
	with := func(model identityProviderModel, change func(*identityProviderModel)) identityProviderModel {
		change(&model)
		return model
	}
	for name, test := range map[string]struct {
		model   identityProviderModel
		unknown []string
		invalid string
	}{
		"awsidc from unknown ARNs":         {model: awsidc, unknown: []string{"application_arn", "iam_role_arn"}},
		"awsidc missing role":              {model: with(awsidc, func(m *identityProviderModel) { m.IAMRoleARN = types.StringNull() }), unknown: []string{"application_arn"}, invalid: "requires iam_role_arn"},
		"awsidc with secret":               {model: with(awsidc, func(m *identityProviderModel) { m.ClientSecret = types.StringValue("secret") }), unknown: []string{"application_arn"}, invalid: "client_secret_wo does not apply"},
		"awsidc with secret version":       {model: with(awsidc, func(m *identityProviderModel) { m.ClientSecretVersion = types.Int64Value(1) }), invalid: "client_secret_wo_version does not apply"},
		"awsidc with unknown audience":     {model: with(awsidc, func(m *identityProviderModel) { m.Audience = []string{"aud"} }), unknown: []string{"audience"}},
		"awsidc filter, unknown switch":    {model: with(awsidc, func(m *identityProviderModel) { m.IncludeGroups = types.StringValue("x") }), unknown: []string{"auto_create_roles"}},
		"awsidc filter without switch":     {model: with(awsidc, func(m *identityProviderModel) { m.IncludeGroups = types.StringValue("x") }), unknown: []string{"application_arn"}, invalid: "require auto_create_roles = true"},
		"azure from unknown secret":        {model: identityProviderWithSecret(azure, "secret"), unknown: []string{"client_secret_wo", "audience"}},
		"azure with application, unknown":  {model: with(azure, func(m *identityProviderModel) { m.ApplicationARN = types.StringValue("application") }), unknown: []string{"client_secret_wo", "audience"}, invalid: "application_arn does not apply"},
		"azure without secret version":     {model: with(azure, func(m *identityProviderModel) { m.ClientSecretVersion = types.Int64Null() }), unknown: []string{"audience"}, invalid: "requires client_secret_wo_version"},
		"unknown type defers type checks":  {model: with(awsidc, func(m *identityProviderModel) { m.Issuer = types.StringValue("https://issuer.example/") }), unknown: []string{"type"}},
		"unknown type keeps filter checks": {model: with(awsidc, func(m *identityProviderModel) { m.ExcludeGroups = types.StringValue("x") }), unknown: []string{"type"}, invalid: "require auto_create_roles = true"},
	} {
		t.Run(name, func(t *testing.T) {
			diagnostics := identityProviderValidateConfig(t, test.model, test.unknown...)
			if test.invalid == "" {
				assert.False(t, diagnostics.HasError(), "%v", diagnostics)
				return
			}
			require.True(t, diagnostics.HasError())
			assert.Contains(t, diagnostics.Errors()[0].Detail(), test.invalid)
		})
	}
}

// TestIdentityProviderInstanceModifier keeps the instance identifiers through an update unless the issuer changes.
func TestIdentityProviderInstanceModifier(t *testing.T) {
	r := newIdentityProviderResource()
	prior := identityProviderAzureFixture("oauth_standard", "aad", "https://sts.windows.net/tenant/")
	prior.InstanceID = types.StringValue("tenant")
	for name, test := range map[string]struct {
		issuer string
		want   types.String
	}{
		"same issuer":    {issuer: "https://sts.windows.net/tenant/", want: types.StringValue("tenant")},
		"changed issuer": {issuer: "https://sts.windows.net/other/", want: types.StringUnknown()},
	} {
		t.Run(name, func(t *testing.T) {
			planned := prior
			planned.Issuer, planned.Enabled = types.StringValue(test.issuer), types.BoolValue(false)
			plan := testState(t, r, planned)
			require.False(t, plan.SetAttribute(context.Background(), path.Root("instance_id"), types.StringUnknown()).HasError())
			req := planmodifier.StringRequest{
				State: testState(t, r, prior), Plan: tfsdk.Plan(plan), StateValue: prior.InstanceID,
				PlanValue: types.StringUnknown(), ConfigValue: types.StringNull(),
			}
			resp := planmodifier.StringResponse{PlanValue: req.PlanValue}
			identityProviderInstanceModifier{}.PlanModifyString(context.Background(), req, &resp)
			require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
			assert.Equal(t, test.want, resp.PlanValue)
		})
	}
	resp := planmodifier.StringResponse{PlanValue: types.StringUnknown()}
	identityProviderInstanceModifier{}.PlanModifyString(context.Background(), planmodifier.StringRequest{
		State: tfsdk.State{Raw: tftypes.NewValue(testState(t, r, prior).Raw.Type(), nil)}, PlanValue: types.StringUnknown(),
	}, &resp)
	assert.True(t, resp.PlanValue.IsUnknown(), "creation leaves the value unknown")
}
