package provider

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	testresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/stretchr/testify/assert"
)

// Even a default response needs an adoption update after import. Otherwise a no-op apply could leave the private
// exemption active and incorrectly accept a later SUPER change without replacing the model.
func TestContractExternalModelImportAdoptsDefaultResponse(t *testing.T) {
	t.Parallel()

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
				ImportStateId: fmt.Sprintf(`{"database":"admin","schema":%q,"name":%q}`, fakeModelSchema, fakeModelName),
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
		},
	})
	assert.Nil(t, models.get(fakeModelName), "destroy drops the model")
}

// The catalog omits the response type and hides the IAM role from non-owners. Import must not recreate the model
// merely to adopt these values, but their next change must still replace it.
func TestContractExternalModelImportAdoptsUnreadSettings(t *testing.T) {
	t.Parallel()

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
				ImportStateId: fmt.Sprintf(`{"database":"admin","schema":%q,"name":%q}`, fakeModelSchema, fakeModelName),
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
func TestContractExternalModelImportObservedResponseReplaces(t *testing.T) {
	t.Parallel()

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
			{
				Config: configuration, ResourceName: address, ImportState: true, ImportStatePersist: true,
				ImportStateId: fmt.Sprintf(`{"database":"admin","schema":%q,"name":%q}`, fakeModelSchema, fakeModelName),
			},
			{Config: configuration, ConfigPlanChecks: testresource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionNoop)}}},
			{
				Config:           strings.ReplaceAll(configuration, `response_type = "SUPER"`, `response_type = "VARCHAR"`),
				ConfigPlanChecks: testresource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionDestroyBeforeCreate)}},
			},
		},
	})
}
