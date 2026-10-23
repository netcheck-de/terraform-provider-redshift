package provider

import (
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

func TestAccDatabaseCollationLifecycle(t *testing.T) {
	testAccDatabaseCollation(t, false)
}

func TestAccClusterDatabaseCollationLifecycle(t *testing.T) {
	testAccPreCheck(t, "REDSHIFT_ACC_CLUSTER", "REDSHIFT_ACC_SECRET_ARN")
	testAccDatabaseCollation(t, true)
}

// An empty, independently owned database can switch collation both ways; resource.Test owns and removes only it.
func testAccDatabaseCollation(t *testing.T, cluster bool) {
	t.Helper()
	region, profile, workgroup, database := testAccWorkgroup(t)
	target := fmt.Sprintf("workgroup_name = %q", workgroup)
	if cluster {
		target = fmt.Sprintf("cluster_identifier = %q", os.Getenv("REDSHIFT_ACC_CLUSTER"))
	}
	name := "acc_collation_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	configuration := func(collation string) string {
		return fmt.Sprintf(`
provider "redshift" {
  region     = %q
  profile    = %q
  database   = %q
  secret_arn = %q
  %s
}
resource "redshift_database" "probe" {
  name      = %q
  collation = %q
}
data "redshift_database" "probe" {
  name = redshift_database.probe.name
}
`, region, profile, database, os.Getenv("REDSHIFT_ACC_SECRET_ARN"), target, name, collation)
	}
	step := func(collation string, update bool) resource.TestStep {
		result := resource.TestStep{
			Config: configuration(collation),
			Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttr("redshift_database.probe", "collation", collation),
				resource.TestCheckResourceAttr("data.redshift_database.probe", "collation", collation),
			),
		}
		if update {
			result.ConfigPlanChecks.PreApply = []plancheck.PlanCheck{plancheck.ExpectResourceAction("redshift_database.probe", plancheck.ResourceActionUpdate)}
		}
		return result
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){
			"redshift": providerserver.NewProtocol6WithError(New("test")()),
		},
		Steps: []resource.TestStep{step("CASE_SENSITIVE", false), step("CASE_INSENSITIVE", true), step("CASE_SENSITIVE", true)},
	})
}
