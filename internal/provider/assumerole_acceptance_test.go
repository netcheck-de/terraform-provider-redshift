package provider

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccAssumeroleGrantLifecycle requires a warehouse with ASSUMEROLE access control already enabled.
func TestAccAssumeroleGrantLifecycle(t *testing.T) {
	t.Parallel()

	region, workgroup, database := testAccWorkgroup(t, "REDSHIFT_ACC_ASSUMEROLE")
	name := "acc_iam_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	configuration := func(command string) string {
		return fmt.Sprintf(`
provider "redshift" {
  region = %q
  workgroup_name = %q
  database = %q
}
resource "redshift_role" "reader" { name = %q }
resource "redshift_assumerole_grant" "reader" {
  iam_role_arn = "DEFAULT"
  grantee = redshift_role.reader.name
  grantee_type = "ROLE"
  privileges = [%q]
}`, region, workgroup, database, name, command)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": testAccProviderServer},
		Steps: []resource.TestStep{
			{Config: configuration("COPY"), Check: resource.TestCheckResourceAttr("redshift_assumerole_grant.reader", "privileges.#", "1")},
			{ResourceName: "redshift_assumerole_grant.reader", ImportState: true, ImportStateVerify: true},
			{Config: configuration("UNLOAD")},
		},
	})
}
