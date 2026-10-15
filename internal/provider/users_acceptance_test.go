package provider

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccUserOptionsLifecycle verifies user options, stored session defaults, the disabled-password form, and the
// group lookup's members against a live warehouse, including no-change plans that prove the catalog reads match
// the configuration.
func TestAccUserOptionsLifecycle(t *testing.T) {
	region, profile, workgroup, database := testAccWorkgroup(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	configuration := func(options string) string {
		return fmt.Sprintf(`
provider "redshift" {
  region = %q
  profile = %q
  workgroup_name = %q
  database = %q
}
resource "redshift_user" "iam" {
  name = "acc_iam_%[5]s"
  password_disabled = true
  %[6]s
}
resource "redshift_group" "readers" { name = "acc_group_%[5]s" }
resource "redshift_group_membership" "iam" {
  group = redshift_group.readers.name
  user = redshift_user.iam.name
}
data "redshift_user" "iam" { name = redshift_user.iam.name }
data "redshift_group" "readers" {
  name = redshift_group_membership.iam.group
}`, region, profile, workgroup, database, suffix, options)
	}
	options := `
  valid_until = "2037-12-31T00:00:00Z"
  connection_limit = 5
  session_timeout = 600
  syslog_access = "UNRESTRICTED"
  search_path = ["$user", "public", "Odd\"Schema"]
  session_defaults = { timezone = "Europe/Berlin", statement_timeout = "300000", datestyle = "ISO, MDY" }`
	cleared := `
  connection_limit = -1
  session_timeout = 0
  syslog_access = "RESTRICTED"`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(New("test")())},
		Steps: []resource.TestStep{
			{Config: configuration(options), Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.redshift_user.iam", "search_path.2", `Odd"Schema`),
				resource.TestCheckResourceAttr("data.redshift_user.iam", "session_defaults.timezone", "Europe/Berlin"),
				resource.TestCheckResourceAttr("data.redshift_user.iam", "session_timeout", "600"),
				resource.TestCheckResourceAttr("data.redshift_group.readers", "members.#", "1"),
			)},
			{Config: configuration(options), PlanOnly: true},
			// Import cannot observe a disabled password and records the default instead.
			{ResourceName: "redshift_user.iam", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"password_disabled"}},
			{Config: configuration(cleared), Check: resource.TestCheckNoResourceAttr("data.redshift_user.iam", "search_path.#")},
			{Config: configuration(cleared), PlanOnly: true},
		},
	})
}
