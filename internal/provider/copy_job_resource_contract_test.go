package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	testresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

// TestContractCopyJobPlan drives real plans: the required table block, an in-place AUTO change, and a replacing source change.
func TestContractCopyJobPlan(t *testing.T) {
	t.Parallel()
	c := &catalog{identity: true, privileges: map[string]bool{}}
	configuration := func(source, auto, table string) string {
		return fmt.Sprintf(`
provider "redshift" {
  region = "eu-central-1"
  workgroup_name = "warehouse"
  database = "admin"
}
resource "redshift_copy_job" "events" {
  database = "admin"
  name = "events_load"
  source = %q
  iam_role = %q
  format = "CSV"
  parameters = { IGNOREHEADER = "1" }
  auto = %s
%s
}`, source, copyJobFakeRole, auto, table)
	}
	table := `  table {
    schema = "serving"
    name = "events"
    columns = ["id", "label"]
  }`
	first := configuration("s3://example-bucket/events/", "false", table)
	automatic := configuration("s3://example-bucket/events/", "true", table)
	moved := configuration("s3://example-bucket/moved/", "true", table)
	testresource.UnitTest(t, testresource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(&redshiftProvider{version: "test", client: c})},
		Steps: []testresource.TestStep{
			{Config: configuration("s3://example-bucket/events/", "false", ""), ExpectError: regexp.MustCompile(`Block table must have a configuration value`), PlanOnly: true},
			{Config: first, Check: testresource.TestCheckResourceAttr("redshift_copy_job.events", "auto", "false")},
			{
				Config: automatic,
				ConfigPlanChecks: testresource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("redshift_copy_job.events", plancheck.ResourceActionUpdate),
				}},
				Check: testresource.TestCheckResourceAttr("redshift_copy_job.events", "auto", "true"),
			},
			{
				Config: moved,
				ConfigPlanChecks: testresource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("redshift_copy_job.events", plancheck.ResourceActionDestroyBeforeCreate),
				}},
			},
		},
	})
}
