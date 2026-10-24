package provider

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/netcheck-de/terraform-provider-redshift/internal/redshiftconn"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These models never invoke Bedrock. They exercise Terraform's import, adoption apply, refresh and subsequent plan.
func TestAccExternalModelImportAdoption(t *testing.T) {
	region, profile, workgroup, database := testAccWorkgroup(t, "REDSHIFT_ACC_MODEL_ROLE")
	for _, scenario := range []struct {
		name, response string
		nonowner       bool
	}{{"owner_default", "VARCHAR", false}, {"owner_super", "SUPER", false}, {"nonowner_super", "SUPER", true}} {
		t.Run(scenario.name, func(t *testing.T) {
			f := newLiveValidationFixture(t, "REDSHIFT_ACC_MODEL_ROLE")
			model := externalModelModel{
				Database: types.StringValue(database), Schema: types.StringValue(f.schema), Name: types.StringValue("imported_model"),
				Function: types.StringValue("infer_model"), IAMRole: types.StringValue(os.Getenv("REDSHIFT_ACC_MODEL_ROLE")),
				ModelID: types.StringValue("amazon.nova-lite-v1:0"), RequestType: types.StringValue("UNIFIED"), ResponseType: types.StringValue(scenario.response),
			}
			statement, err := createExternalModelStatement(model)
			require.NoError(t, err)
			_, err = f.client.Query(context.Background(), f.conn, statement, nil)
			require.NoError(t, err, "CREATE EXTERNAL MODEL requires only fixture permissions; no inference is attempted")
			t.Cleanup(func() {
				_, err := f.client.Query(context.Background(), f.conn, sqlclient.Stmt("DROP MODEL IF EXISTS").Qualified(f.schema, "imported_model").String(), nil)
				assert.NoError(t, err, "clean up imported model")
			})

			provider := fmt.Sprintf("provider \"redshift\" {\n region = %q\n profile = %q\n workgroup_name = %q\n database = %q\n}\n", region, profile, workgroup, database)
			identity := fmt.Sprintf(`{"workgroup_name":%q,"database":%q,"schema":%q,"name":"imported_model"}`, workgroup, database, f.schema)
			observer := f
			grantee := ""

			if scenario.nonowner {
				testAccPreCheck(t, "REDSHIFT_ACC_DIRECT")
				user, password := f.user(t)
				grantee = user
				f.exec(t, sqlclient.Stmt("GRANT USAGE ON SCHEMA").Ident(f.schema).Kw("TO").Ident(user))
				t.Cleanup(func() {
					_, err := f.client.Query(context.Background(), f.conn, sqlclient.Stmt("REVOKE EXECUTE ON MODEL").Qualified(f.schema, "imported_model").Kw("FROM").Ident(user).String(), nil)
					assert.NoError(t, err)
					_, err = f.client.Query(context.Background(), f.conn, sqlclient.Stmt("REVOKE USAGE ON SCHEMA").Ident(f.schema).Kw("FROM").Ident(user).String(), nil)
					assert.NoError(t, err)
					_, err = f.client.Query(context.Background(), f.conn, sqlclient.Stmt("DROP MODEL IF EXISTS").Qualified(f.schema, "imported_model").String(), nil)
					assert.NoError(t, err, "remove model-generated function privileges before dropping user")
				})
				f.exec(t, sqlclient.Stmt("GRANT EXECUTE ON MODEL").Qualified(f.schema, "imported_model").Kw("TO").Ident(user))
				observer = f.directUser(t, user, password)
				credentials := observer.client.(*redshiftconn.Client).Credentials
				provider = fmt.Sprintf("provider \"redshift\" {\n database = %q\n direct_connection {\n host = %q\n port = %d\n username = %q\n password = %q\n %s\n }\n}\n", database, credentials.Host, credentials.Port, user, password, testAccSSLModeHCL())
				identity = fmt.Sprintf(`{"endpoint":%q,"database":%q,"schema":%q,"name":"imported_model"}`, fmt.Sprintf("%s:%d", credentials.Host, credentials.Port), database, f.schema)
			}

			catalogClient := resourceClient{client: observer.client, database: types.StringValue(database), warehouse: warehouseBinding{field: "workgroup_name", value: types.StringValue(workgroup)}}
			settings, found, err := readExternalModelCatalog(context.Background(), &catalogClient, database, f.schema, "imported_model")
			require.NoError(t, err)
			require.True(t, found, "EXECUTE on the model must make it catalog-visible")
			t.Logf("%s: SHOW MODEL response_type=%q iam_role_visible=%t", scenario.name, settings["response_type"], settings["iam_role"] != "")

			if scenario.nonowner {
				assert.Empty(t, settings["iam_role"], "non-owner catalog must not reveal the IAM role")
			} else {
				assert.NotEmpty(t, settings["iam_role"], "owner must see the role")
			}

			configuration := func(response string) string {
				text := provider + fmt.Sprintf(`
resource "redshift_external_model" "imported" {
 database = %q
 schema = %q
 name = "imported_model"
 function = "infer_model"
 iam_role = %q
 model_id = "amazon.nova-lite-v1:0"
 request_type = "UNIFIED"
 response_type = %q
}
data "redshift_external_model" "observed" {
 database = redshift_external_model.imported.database
 schema = redshift_external_model.imported.schema
 name = redshift_external_model.imported.name
}
data "redshift_models" "observed" {
 database = redshift_external_model.imported.database
 schema = redshift_external_model.imported.schema
}
`, database, f.schema, os.Getenv("REDSHIFT_ACC_MODEL_ROLE"), response)
				if grantee != "" {
					text += fmt.Sprintf(`
data "redshift_object_grant" "model" {
 database_name = %q
 schema_name = %q
 object_name = "imported_model"
 object_type = "MODEL"
 grantee = %q
 grantee_type = "USER"
}
`, database, f.schema, grantee)
				}

				return text
			}
			address := "redshift_external_model.imported"

			checks := []resource.TestCheckFunc{
				resource.TestCheckResourceAttr(address, "response_type", scenario.response),
				resource.TestCheckResourceAttr(address, "iam_role", os.Getenv("REDSHIFT_ACC_MODEL_ROLE")),
				resource.TestCheckResourceAttr("data.redshift_models.observed", "models.#", "1"),
				resource.TestCheckResourceAttr("data.redshift_external_model.observed", "model_id", "amazon.nova-lite-v1:0"),
			}
			if scenario.nonowner {
				checks = append(checks, resource.TestCheckNoResourceAttr("data.redshift_external_model.observed", "iam_role"), resource.TestCheckResourceAttr("data.redshift_object_grant.model", "privileges.0", "EXECUTE"))
			}

			steps := []resource.TestStep{
				{Config: configuration(scenario.response), ResourceName: address, ImportState: true, ImportStatePersist: true, ImportStateId: identity},
				{Config: configuration(scenario.response), Check: resource.ComposeAggregateTestCheckFunc(checks...), ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate)}}},
				{Config: configuration(scenario.response), PlanOnly: true},
			}
			if !scenario.nonowner {
				changed := "SUPER"
				if scenario.response == changed {
					changed = "VARCHAR"
				}

				steps = append(steps, resource.TestStep{Config: configuration(changed), ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionDestroyBeforeCreate)}}})
			} else {
				// EXECUTE permits visibility, not DROP MODEL. Only the administrator cleanup drops this fixture.
				steps = append(steps, resource.TestStep{Config: provider + "\nremoved {\n from = redshift_external_model.imported\n lifecycle { destroy = false }\n}\n"})
			}

			resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(New("test")())}, Steps: steps})
		})
	}
}
