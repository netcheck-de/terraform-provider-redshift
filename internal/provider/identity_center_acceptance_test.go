package provider

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The application and IAM role are separately provisioned disposable fixtures. No directory user, membership,
// interactive login or token exchange is involved. Optional existing directory group/user names enable namespaced
// SQL roles and a temporary password-disabled SQL user; directory objects and memberships are never changed.
func TestAccIdentityCenterLifecycle(t *testing.T) {
	region, profile, workgroup, database := testAccWorkgroup(t, "REDSHIFT_ACC_IDC_APPLICATION_ARN", "REDSHIFT_ACC_IDC_ROLE_ARN")
	f := newLiveValidationFixture(t, "REDSHIFT_ACC_IDC_APPLICATION_ARN", "REDSHIFT_ACC_IDC_ROLE_ARN")
	name, namespace := f.schema+"_idp", f.schema
	group := os.Getenv("REDSHIFT_ACC_IDC_GROUP_NAME")
	provider := fmt.Sprintf("provider \"redshift\" {\n region = %q\n profile = %q\n workgroup_name = %q\n database = %q\n}\n", region, profile, workgroup, database)
	configuration := func(name, namespace string, enabled bool) string {
		text := provider + fmt.Sprintf(`
resource "redshift_identity_provider" "test" {
  name = %q
  namespace = %q
  application_arn = %q
  iam_role_arn = %q
  enabled = %t
  auto_create_roles = false
}
data "redshift_identity_provider" "test" {
  name = redshift_identity_provider.test.name
}
`, name, namespace, os.Getenv("REDSHIFT_ACC_IDC_APPLICATION_ARN"), os.Getenv("REDSHIFT_ACC_IDC_ROLE_ARN"), enabled)
		if group != "" {
			text += fmt.Sprintf("resource \"redshift_role\" \"group\" {\n name = %q\n}\n", "${redshift_identity_provider.test.namespace}:"+group)
		}
		return text
	}
	address := "redshift_identity_provider.test"
	initial := configuration(name, namespace, true)
	renamed := configuration(name+"_replacement", namespace+"_new", true)
	steps := []resource.TestStep{
		{Config: initial, Check: resource.ComposeAggregateTestCheckFunc(
			resource.TestCheckResourceAttr(address, "enabled", "true"),
			resource.TestCheckResourceAttr("data.redshift_identity_provider.test", "application_arn", os.Getenv("REDSHIFT_ACC_IDC_APPLICATION_ARN")),
		)},
		{ResourceName: address, ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"auto_create_roles"}},
		{Config: configuration(name, namespace, false), Check: resource.TestCheckResourceAttr(address, "enabled", "false")},
		{Config: initial, PreConfig: func() {
			f.exec(t, sqlclient.Stmt("ALTER IDENTITY PROVIDER").Ident(name).Kw("ENABLE"))
		}, Check: resource.TestCheckResourceAttr(address, "enabled", "true")},
		{Config: initial, PreConfig: func() {
			f.exec(t, sqlclient.Stmt("ALTER IDENTITY PROVIDER").Ident(name).Kw("DISABLE"))
		}, ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate)}}},
	}
	if group != "" {
		steps = append(steps,
			resource.TestStep{ResourceName: "redshift_role.group", ImportState: true, ImportStateVerify: true},
			resource.TestStep{Config: initial, PreConfig: func() {
				f.exec(t, sqlclient.Stmt("DROP ROLE").Ident(namespace+":"+group))
			}, ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("redshift_role.group", plancheck.ResourceActionCreate)}}},
		)
	}
	steps = append(steps, resource.TestStep{Config: renamed, ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionDestroyBeforeCreate)}}})
	if username := os.Getenv("REDSHIFT_ACC_IDC_USER_NAME"); username != "" {
		user := namespace + "_new:" + username
		created := false
		t.Cleanup(func() {
			if created {
				_, err := f.client.Query(context.Background(), f.conn, sqlclient.Stmt("DROP USER").Ident(user).String(), nil)
				assert.NoError(t, err, "clean up only the SQL user created by this test")
			}
		})
		steps = append(steps,
			resource.TestStep{Config: renamed, PreConfig: func() {
				rows := f.query(t, sqlclient.Select("usename").From("pg_user").Where("usename = :name", sqlclient.Bind("name", user)))
				require.Empty(t, rows, "never adopt or delete a preexisting SQL user")
				f.exec(t, sqlclient.Stmt("CREATE USER").Ident(user).Kw("PASSWORD DISABLE"))
				created = true
			}, Destroy: true, ExpectError: regexp.MustCompile("Federated users with the namespace prefix")},
			resource.TestStep{Config: configuration(name+"_replacement", namespace+"_blocked", true), ExpectError: regexp.MustCompile("Federated users with the namespace prefix")},
			resource.TestStep{Config: renamed, PreConfig: func() {
				f.exec(t, sqlclient.Stmt("DROP USER").Ident(user))
				created = false
			}},
		)
	}
	final := resource.TestStep{Config: configuration(name+"_replacement", namespace+"_final", true), Check: resource.TestCheckResourceAttr(address, "namespace", namespace+"_final")}
	if group != "" {
		final.Check = resource.ComposeAggregateTestCheckFunc(final.Check,
			resource.TestCheckResourceAttr("redshift_role.group", "name", namespace+"_final:"+group),
			resource.TestCheckResourceAttrSet("redshift_role.group", "owner"),
		)
		final.ConfigPlanChecks = resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("redshift_role.group", plancheck.ResourceActionDestroyBeforeCreate)}}
	}
	steps = append(steps, final)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(New("test")())},
		Steps:                    steps,
		CheckDestroy: func(_ *terraform.State) error {
			for _, query := range []sqlclient.Query{
				sqlclient.Select("name").From("svv_identity_providers").Where("name = :name OR name = :replacement", sqlclient.Bind("name", name), sqlclient.Bind("replacement", name+"_replacement")),
				sqlclient.Select("role_name").From("svv_roles").Where("LEFT(role_name, LENGTH(:prefix)) = :prefix", sqlclient.Bind("prefix", namespace)),
				sqlclient.Select("usename").From("pg_user").Where("LEFT(usename, LENGTH(:prefix)) = :prefix", sqlclient.Bind("prefix", namespace)),
			} {
				if rows := f.query(t, query); len(rows) != 0 {
					return fmt.Errorf("identity-center SQL fixture remains after destroy: %v", rows)
				}
			}
			return nil
		},
	})
}
