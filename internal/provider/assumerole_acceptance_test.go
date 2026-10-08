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
	"github.com/stretchr/testify/require"
)

// TestAccAssumeroleGrantLifecycle requires a warehouse with ASSUMEROLE access control already enabled.
func TestAccAssumeroleGrantLifecycle(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" || os.Getenv("REDSHIFT_ACC_ASSUMEROLE") != "1" {
		t.Skip("set TF_ACC=1 and REDSHIFT_ACC_ASSUMEROLE=1 on a suitably configured test warehouse")
	}
	region, profile := os.Getenv("REDSHIFT_ACC_REGION"), os.Getenv("REDSHIFT_ACC_PROFILE")
	workgroup, database := os.Getenv("REDSHIFT_ACC_WORKGROUP"), os.Getenv("REDSHIFT_ACC_DATABASE")
	require.NotEmpty(t, region)
	require.NotEmpty(t, profile)
	require.NotEmpty(t, workgroup)
	require.NotEmpty(t, database)
	name := "acc_iam_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	configuration := func(command string) string {
		return fmt.Sprintf(`
provider "redshift" {
  region = %q
  profile = %q
  workgroup_name = %q
  database = %q
}
resource "redshift_role" "reader" { name = %q }
resource "redshift_assumerole_grant" "reader" {
  iam_role_arn = "default"
  grantee = redshift_role.reader.name
  grantee_type = "ROLE"
  privileges = [%q]
}`, region, profile, workgroup, database, name, command)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(New("test")())},
		Steps: []resource.TestStep{
			{Config: configuration("COPY"), Check: resource.TestCheckResourceAttr("redshift_assumerole_grant.reader", "privileges.#", "1")},
			{Config: configuration("COPY"), PlanOnly: true},
			{ResourceName: "redshift_assumerole_grant.reader", ImportState: true, ImportStateVerify: true},
			{Config: configuration("UNLOAD")},
			{Config: configuration("UNLOAD"), PlanOnly: true},
		},
	})
}
