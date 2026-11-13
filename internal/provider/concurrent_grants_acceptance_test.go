package provider

import (
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/stretchr/testify/require"
)

// TestAccConcurrentSharedGrants exercises six independent scoped grants at Terraform's default parallelism.
// Only uniquely named fixture roles and their grants are managed; the existing shared database is retained.
func TestAccConcurrentSharedGrants(t *testing.T) {
	testAccPreCheck(t, "REDSHIFT_ACC_SHARED_DATABASE")

	region := os.Getenv("REDSHIFT_ACC_REGION")
	workgroup, database := os.Getenv("REDSHIFT_ACC_WORKGROUP"), os.Getenv("REDSHIFT_ACC_DATABASE")

	shared, secret := os.Getenv("REDSHIFT_ACC_SHARED_DATABASE"), os.Getenv("REDSHIFT_ACC_SECRET_ARN")
	for _, value := range []string{region, workgroup, database, secret} {
		require.NotEmpty(t, value)
	}

	prefix := "acc_concurrent_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	configuration := fmt.Sprintf(`
provider "redshift" {
  region = %q
  workgroup_name = %q
  database = %q
  secret_arn = %q
}
resource "redshift_role" "fixture" {
  count = 2
  name = format("%%s%%d", %q, count.index)
}
resource "redshift_grant" "fixture" {
  count = 6
  database_name = %q
  role = redshift_role.fixture[floor(count.index / 3)].name
  scope = ["DATABASE", "SCHEMAS", "TABLES"][count.index %% 3]
  privileges = count.index %% 3 == 2 ? ["SELECT"] : ["USAGE"]
}
`, region, workgroup, database, secret, prefix, shared)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){
			"redshift": testAccProviderServer,
		},
		Steps: []resource.TestStep{
			{Config: configuration, Check: func(state *terraform.State) error {
				grants := 0

				for address, observed := range state.RootModule().Resources {
					if observed.Type != "redshift_grant" {
						continue
					}

					grants++

					if observed.Primary.Attributes["privileges.#"] != "1" {
						return fmt.Errorf("missing scoped grant %s", address)
					}
				}

				if grants != 6 {
					return fmt.Errorf("expected six scoped grants, got %d", grants)
				}

				return nil
			}},
		},
	})
}
