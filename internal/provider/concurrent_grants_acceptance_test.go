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
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/stretchr/testify/require"
)

// TestAccConcurrentSharedGrants exercises six independent scoped grants at Terraform's default parallelism.
// Only uniquely named fixture roles and their grants are managed; the existing shared database is retained.
func TestAccConcurrentSharedGrants(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" || os.Getenv("REDSHIFT_ACC_SHARED_DATABASE") == "" {
		t.Skip("set TF_ACC=1 and REDSHIFT_ACC_SHARED_DATABASE for a consumer warehouse")
	}
	region, profile := os.Getenv("REDSHIFT_ACC_REGION"), os.Getenv("REDSHIFT_ACC_PROFILE")
	workgroup, database := os.Getenv("REDSHIFT_ACC_WORKGROUP"), os.Getenv("REDSHIFT_ACC_DATABASE")
	shared, secret := os.Getenv("REDSHIFT_ACC_SHARED_DATABASE"), os.Getenv("REDSHIFT_ACC_SECRET_ARN")
	for _, value := range []string{region, profile, workgroup, database, secret} {
		require.NotEmpty(t, value)
	}
	prefix := "acc_concurrent_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	configuration := fmt.Sprintf(`
provider "redshift" {
  region = %q
  profile = %q
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
`, region, profile, workgroup, database, secret, prefix, shared)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){
			"redshift": providerserver.NewProtocol6WithError(New("test")()),
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
			{Config: configuration, PlanOnly: true},
		},
	})
}
