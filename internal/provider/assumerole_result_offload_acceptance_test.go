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
)

// TestAccAssumeroleResultOffload verifies independent ARN/ALL grants and exact-set removal without changing PUBLIC.
func TestAccAssumeroleResultOffload(t *testing.T) {
	region, profile, workgroup, database := testAccWorkgroup(t, "REDSHIFT_ACC_ASSUMEROLE", "REDSHIFT_ACC_SPECTRUM_ROLE_ARN")
	name := "acc_offload_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	configuration := func(selector, privileges string) string {
		return grantAcceptanceProvider(region, profile, workgroup, database) + fmt.Sprintf(`
resource "redshift_role" "reader" { name = %q }
resource "redshift_assumerole_grant" "offload" {
  iam_role_arn = %q
  grantee = redshift_role.reader.name
  grantee_type = "ROLE"
  privileges = [%s]
}
data "redshift_assumerole_grant" "offload" {
  iam_role_arn = redshift_assumerole_grant.offload.iam_role_arn
  grantee = redshift_assumerole_grant.offload.grantee
  grantee_type = "ROLE"
  depends_on = [redshift_assumerole_grant.offload]
}
`, name, selector, privileges)
	}
	arn := os.Getenv("REDSHIFT_ACC_SPECTRUM_ROLE_ARN")
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(New("test")())},
		Steps: []resource.TestStep{
			{Config: configuration(arn, `"RESULT_OFFLOAD"`), Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("redshift_assumerole_grant.offload", "privileges.#", "1"),
				resource.TestCheckTypeSetElemAttr("data.redshift_assumerole_grant.offload", "privileges.*", "RESULT_OFFLOAD"),
			)},
			{Config: configuration(arn, `"RESULT_OFFLOAD"`), PlanOnly: true},
			{ResourceName: "redshift_assumerole_grant.offload", ImportState: true, ImportStateVerify: true},
			{Config: configuration(arn, `"COPY", "RESULT_OFFLOAD"`), Check: resource.TestCheckResourceAttr("data.redshift_assumerole_grant.offload", "privileges.#", "2")},
			{Config: configuration(arn, `"COPY"`), Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.redshift_assumerole_grant.offload", "privileges.#", "1"),
				resource.TestCheckTypeSetElemAttr("data.redshift_assumerole_grant.offload", "privileges.*", "COPY"),
			)},
			{Config: configuration(arn, ``), Check: resource.TestCheckResourceAttr("data.redshift_assumerole_grant.offload", "privileges.#", "0")},
			{Config: configuration("ALL", `"RESULT_OFFLOAD"`), Check: resource.TestCheckTypeSetElemAttr("data.redshift_assumerole_grant.offload", "privileges.*", "RESULT_OFFLOAD")},
			{ResourceName: "redshift_assumerole_grant.offload", ImportState: true, ImportStateVerify: true},
		},
	})
}
