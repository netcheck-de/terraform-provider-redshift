package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	testresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerLifecycleCase(lifecycleCase{
	name: "external_model", new: newExternalModelResource, model: testExternalModelModel(),
	absent: func(c *catalog) { c.family("external_model").(*externalModelFamily).remove(fakeModelName) },
})

var _ = registerReplacementPolicy("redshift_external_model", map[string]replaceRule{
	"database":      replaceAlways,
	"schema":        replaceAlways,
	"name":          replaceAlways,
	"function":      replaceAlways,
	"iam_role":      replaceAlways,
	"model_id":      replaceAlways,
	"prompt":        replaceAlways,
	"suffix":        replaceAlways,
	"request_type":  replaceAlways,
	"response_type": replaceAlways,
})

var _ = registerValidateConfigCase("external_model", validateConfigCase{
	new:   newExternalModelResource,
	valid: testExternalModelModel(),
	invalid: func() externalModelModel {
		data := testExternalModelModel()
		data.RequestType = types.StringValue("RAW")
		return data
	}(),
	unknown: func() externalModelModel {
		data := testExternalModelModel()
		data.ModelID = types.StringUnknown()
		return data
	}(),
})

// externalModelFake returns a full fake catalog and its model family.
func externalModelFake() (*catalog, *externalModelFamily) {
	c := fullCatalog()
	return c, fakeState[*externalModelFamily](c, "external_model")
}

// decodeExternalModelState reads a model from Terraform state.
func decodeExternalModelState(t *testing.T, state tfsdk.State) externalModelModel {
	t.Helper()
	var data externalModelModel
	require.False(t, state.Get(context.Background(), &data).HasError())
	return data
}

// TestExternalModelCreateRecordsCatalog creates a RAW model with a role ARN and records the catalog owner.
func TestExternalModelCreateRecordsCatalog(t *testing.T) {
	c, models := externalModelFake()
	models.remove(fakeModelName)
	r := newExternalModelResource()
	configureTestResource(t, r, c)
	planned := testExternalModelModel()
	planned.Prompt, planned.RequestType, planned.ResponseType = types.StringNull(), types.StringValue("RAW"), types.StringValue("SUPER")
	planned.IAMRole, planned.Owner = types.StringValue("arn:aws:iam::123456789012:role/redshift-bedrock"), types.StringUnknown()
	state, diagnostics := applyOperation(t, r, "create", nil, planned, nil)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	created := decodeExternalModelState(t, state)
	assert.Equal(t, "admin", created.Owner.ValueString())
	assert.JSONEq(t, `{"workgroup_name":"warehouse","database":"admin","schema":"serving","name":"summarizer"}`, created.ID.ValueString())
	assert.Equal(t, "raw", models.get(fakeModelName).settings["Request Type"])
	assert.Equal(t, "arn:aws:iam::123456789012:role/redshift-bedrock", models.get(fakeModelName).settings["IAM Role"])

	state, diagnostics = applyOperation(t, r, "read", created, nil, nil)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.Equal(t, created, decodeExternalModelState(t, state), "a refresh without outside changes is stable")
}

// TestExternalModelDrift surfaces settings changed outside Terraform where SHOW MODEL reports them, and keeps state
// where it does not.
func TestExternalModelDrift(t *testing.T) {
	c, models := externalModelFake()
	r := newExternalModelResource()
	configureTestResource(t, r, c)
	models.get(fakeModelName).settings["Model ID"] = "amazon.nova-pro-v1:0"
	state, diagnostics := applyOperation(t, r, "read", testExternalModelModel(), nil, nil)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.Equal(t, "amazon.nova-pro-v1:0", decodeExternalModelState(t, state).ModelID.ValueString())

	models.hideSettings = true
	state, diagnostics = applyOperation(t, r, "read", testExternalModelModel(), nil, nil)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.Equal(t, testExternalModelModel(), decodeExternalModelState(t, state), "unreported settings keep their state")
	assert.Empty(t, c.writes)
}

// TestExternalModelImport fills the reported settings from SHOW MODEL without mutating.
func TestExternalModelImport(t *testing.T) {
	c, _ := externalModelFake()
	r := newExternalModelResource()
	configureTestResource(t, r, c)
	imported := resource.ImportStateResponse{State: emptyState(t, r)}
	id := `{"workgroup_name":"warehouse","database":"admin","schema":"serving","name":"summarizer"}`
	r.(resource.ResourceWithImportState).ImportState(context.Background(), resource.ImportStateRequest{ID: id}, &imported)
	require.False(t, imported.Diagnostics.HasError(), "%v", imported.Diagnostics)
	resp := resource.ReadResponse{State: imported.State}
	r.Read(context.Background(), resource.ReadRequest{State: imported.State}, &resp)
	require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
	data := decodeExternalModelState(t, resp.State)
	expected := testExternalModelModel()
	expected.ID = types.StringValue(id)
	assert.Equal(t, expected, data)
	assert.Empty(t, c.writes, "import never mutates")
}

// Even a default response needs an adoption update after import. Otherwise a no-op apply could leave the private
// exemption active and incorrectly accept a later SUPER change without replacing the model.
func TestExternalModelImportAdoptsDefaultResponse(t *testing.T) {
	c, models := externalModelFake()
	configuration := func(requestType, modelID string) string {
		return fmt.Sprintf(`
provider "redshift" {
  region = "eu-central-1"
  workgroup_name = "warehouse"
  database = "admin"
}
resource "redshift_external_model" "summarizer" {
  database = "admin"
  schema = %q
  name = %q
  function = %q
  iam_role = "default"
  model_id = %q
  prompt = %q
  request_type = %q
}`, fakeModelSchema, fakeModelName, fakeModelFunction, modelID, fakeModelPrompt, requestType)
	}
	address := "redshift_external_model.summarizer"
	testresource.UnitTest(t, testresource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(&redshiftProvider{version: "test", client: c})},
		Steps: []testresource.TestStep{
			{
				Config: configuration("UNIFIED", fakeModelID), ResourceName: address, ImportState: true, ImportStatePersist: true,
				ImportStateId: fmt.Sprintf(`{"workgroup_name":"warehouse","database":"admin","schema":%q,"name":%q}`, fakeModelSchema, fakeModelName),
			},
			{
				Config:           configuration("UNIFIED", fakeModelID),
				ConfigPlanChecks: testresource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate)}},
				Check: func(*terraform.State) error {
					assert.Empty(t, c.writes, "default-response adoption must not issue SQL")
					return nil
				},
			},
			{
				Config:           configuration("unified", fakeModelID),
				ConfigPlanChecks: testresource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate)}},
			},
			{
				Config:           configuration("unified", fakeModelID) + "\n",
				ConfigPlanChecks: testresource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionNoop)}},
			},
			{
				Config:           strings.TrimSuffix(configuration("unified", fakeModelID), "}") + "\n response_type = \"SUPER\"\n}",
				ConfigPlanChecks: testresource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionDestroyBeforeCreate)}},
			},
			{
				Config:           configuration("unified", "amazon.nova-pro-v1:0"),
				ConfigPlanChecks: testresource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionDestroyBeforeCreate)}},
			},
			{Config: configuration("unified", "amazon.nova-pro-v1:0"), PlanOnly: true},
		},
	})
	assert.Nil(t, models.get(fakeModelName), "destroy drops the model")
}

// TestExternalModelRoleReplacement replaces the model for every role change except DEFAULT in another case.
func TestExternalModelRoleReplacement(t *testing.T) {
	arn := "arn:aws:iam::123456789012:role/redshift-bedrock"
	for _, test := range []struct {
		before, after string
		replace       bool
	}{
		{"DEFAULT", "default", false},
		{"DEFAULT", "DEFAULT", false},
		{arn, arn, false},
		{"DEFAULT", arn, true},
		{arn, "DEFAULT", true},
		{arn, strings.ToUpper(arn), true},
	} {
		request := planmodifier.StringRequest{StateValue: types.StringValue(test.before), PlanValue: types.StringValue(test.after)}
		var response stringplanmodifier.RequiresReplaceIfFuncResponse
		externalModelRoleChanged(context.Background(), request, &response)
		assert.Equal(t, test.replace, response.RequiresReplace, "%s -> %s", test.before, test.after)
	}
	var response stringplanmodifier.RequiresReplaceIfFuncResponse
	externalModelRoleChanged(context.Background(), planmodifier.StringRequest{StateValue: types.StringValue("DEFAULT"), PlanValue: types.StringUnknown()}, &response)
	assert.True(t, response.RequiresReplace, "an unknown role may differ")
}

// The catalog omits the response type and hides the IAM role from non-owners. Import must not recreate the model
// merely to adopt these values, but their next change must still replace it.
func TestExternalModelImportAdoptsUnreadSettings(t *testing.T) {
	c, models := externalModelFake()
	delete(models.get(fakeModelName).settings, "IAM Role")
	configuration := func(role string) string {
		return fmt.Sprintf(`
provider "redshift" {
  region = "eu-central-1"
  workgroup_name = "warehouse"
  database = "admin"
}

resource "redshift_external_model" "summarizer" {
  database = "admin"
  schema = %q
  name = %q
  function = %q
  iam_role = %q
  model_id = %q
  prompt = %q
  response_type = "SUPER"
}`, fakeModelSchema, fakeModelName, fakeModelFunction, role, fakeModelID, fakeModelPrompt)
	}
	address := "redshift_external_model.summarizer"
	testresource.UnitTest(t, testresource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(&redshiftProvider{version: "test", client: c})},
		Steps: []testresource.TestStep{
			{
				Config: configuration("DEFAULT"), ResourceName: address, ImportState: true, ImportStatePersist: true,
				ImportStateId: fmt.Sprintf(`{"workgroup_name":"warehouse","database":"admin","schema":%q,"name":%q}`, fakeModelSchema, fakeModelName),
			},
			{
				Config:           configuration("DEFAULT"),
				ConfigPlanChecks: testresource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate)}},
				Check: func(*terraform.State) error {
					assert.Empty(t, c.writes, "adopting unread settings must not issue SQL")
					return nil
				},
			},
			{
				Config:           configuration("arn:aws:iam::123456789012:role/replacement"),
				ConfigPlanChecks: testresource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionDestroyBeforeCreate)}},
			},
			{
				Config:           strings.ReplaceAll(configuration("arn:aws:iam::123456789012:role/replacement"), `response_type = "SUPER"`, `response_type = "VARCHAR"`),
				ConfigPlanChecks: testresource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionDestroyBeforeCreate)}},
			},
		},
	})
}

// A future catalog that reports RESPONSE_TYPE must end the exemption on observation, without requiring an update.
func TestExternalModelImportObservedResponseReplaces(t *testing.T) {
	c, models := externalModelFake()
	models.get(fakeModelName).settings["Response Type"] = "SUPER"
	configuration := fmt.Sprintf(`
provider "redshift" {
 region = "eu-central-1"
 workgroup_name = "warehouse"
 database = "admin"
}
resource "redshift_external_model" "summarizer" {
 database = "admin"
 schema = %q
 name = %q
 function = %q
 iam_role = "DEFAULT"
 model_id = %q
 prompt = %q
 response_type = "SUPER"
}`, fakeModelSchema, fakeModelName, fakeModelFunction, fakeModelID, fakeModelPrompt)
	address := "redshift_external_model.summarizer"
	testresource.UnitTest(t, testresource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(&redshiftProvider{version: "test", client: c})},
		Steps: []testresource.TestStep{
			{Config: configuration, ResourceName: address, ImportState: true, ImportStatePersist: true,
				ImportStateId: fmt.Sprintf(`{"workgroup_name":"warehouse","database":"admin","schema":%q,"name":%q}`, fakeModelSchema, fakeModelName)},
			{Config: configuration, ConfigPlanChecks: testresource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionNoop)}}},
			{Config: strings.ReplaceAll(configuration, `response_type = "SUPER"`, `response_type = "VARCHAR"`),
				ConfigPlanChecks: testresource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionDestroyBeforeCreate)}}},
		},
	})
}

// TestExternalModelCreateRejectsInvalidSettingsBeforeSQL validates the settings before any SQL or state write.
func TestExternalModelCreateRejectsInvalidSettingsBeforeSQL(t *testing.T) {
	r := newExternalModelResource()
	configureTestResource(t, r, queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
		return nil, errors.New("unexpected SQL " + sql)
	}))
	data := testExternalModelModel()
	data.RequestType, data.ResponseType = types.StringValue("RAW"), types.StringValue("SUPER")
	state, diagnostics := applyOperation(t, r, "create", nil, data, nil)
	require.True(t, diagnostics.HasError())
	assert.Contains(t, diagnostics.Errors()[0].Detail(), "prompt and suffix")
	assert.True(t, state.Raw.IsNull(), "a rejected model must not be recorded in state")
}

// TestExternalModelConvergenceFailure reports a setting the catalog does not reflect after apply, while Create still
// records the created model.
func TestExternalModelConvergenceFailure(t *testing.T) {
	c, models := externalModelFake()
	models.remove(fakeModelName)
	r := newExternalModelResource()
	configureTestResource(t, r, queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		rows, err := c.Query(ctx, target, sql, parameters)
		if strings.HasPrefix(sql, "CREATE EXTERNAL MODEL") {
			models.get(fakeModelName).settings["Model ID"] = "amazon.titan-text-express-v1"
		}
		return rows, err
	}))
	state, diagnostics := applyOperation(t, r, "create", nil, testExternalModelModel(), nil)
	require.True(t, diagnostics.HasError())
	assert.Contains(t, diagnostics.Errors()[0].Detail(), "model_id")
	assert.Equal(t, fakeModelName, decodeExternalModelState(t, state).Name.ValueString(), "the created model stays in state")
}

// TestExternalModelReadRejectsIncompleteCatalogRows reports a SHOW MODEL ALL row without a schema or name.
func TestExternalModelReadRejectsIncompleteCatalogRows(t *testing.T) {
	r := &externalModelResource{testResourceClient(queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
		return []sqlclient.Row{{"Schema Name": "serving"}}, nil
	}))}
	data := testExternalModelModel()
	_, err := r.read(context.Background(), &data)
	require.Error(t, err)
}

// TestExternalModelTranscripts records a RAW creation with a role ARN beyond the lifecycle case.
func TestExternalModelTranscripts(t *testing.T) {
	raw := testExternalModelModel()
	raw.Prompt, raw.RequestType, raw.ResponseType = types.StringNull(), types.StringValue("RAW"), types.StringValue("SUPER")
	raw.IAMRole, raw.Owner = types.StringValue("arn:aws:iam::123456789012:role/redshift-bedrock"), types.StringUnknown()
	runTranscripts(t, "external_model_transcript", newExternalModelResource, []transcriptCase{
		{name: "create_raw", operation: "create", catalog: catalogWith(func(c *catalog) { c.family("external_model").(*externalModelFamily).remove(fakeModelName) }), planned: raw},
	})
}

// TestExternalModelImportResponseDefault records the Redshift default response type after an import, because SHOW
// MODEL never prints it, and keeps a value state already holds.
func TestExternalModelImportResponseDefault(t *testing.T) {
	for _, tc := range []struct{ request, state, want types.String }{
		{types.StringValue("UNIFIED"), types.StringNull(), types.StringValue("VARCHAR")},
		{types.StringValue("RAW"), types.StringNull(), types.StringValue("SUPER")},
		{types.StringValue("UNIFIED"), types.StringValue("SUPER"), types.StringValue("SUPER")},
	} {
		data := externalModelModel{RequestType: tc.request, ResponseType: tc.state}
		defaultExternalModelResponse(&data)
		assert.Equal(t, tc.want, data.ResponseType, "%s/%s", tc.request, tc.state)
	}
}
