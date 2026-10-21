package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// The destination remains externally owned so the test cannot drop an active integration's database.
func TestAccDatabaseIntegrationMySQLLookup(t *testing.T) {
	region, profile, workgroup, database := testAccWorkgroup(t, "REDSHIFT_ACC_MYSQL_DATABASE", "REDSHIFT_ACC_MYSQL_INTEGRATION_ID", "REDSHIFT_ACC_SECRET_ARN")
	configuration := fmt.Sprintf(`
provider "redshift" {
  region         = %q
  profile        = %q
  workgroup_name = %q
  database       = %q
  secret_arn     = %q
}
data "redshift_database" "destination" {
  name = %q
}
`, region, profile, workgroup, database, os.Getenv("REDSHIFT_ACC_SECRET_ARN"), os.Getenv("REDSHIFT_ACC_MYSQL_DATABASE"))
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){
			"redshift": providerserver.NewProtocol6WithError(New("test")()),
		},
		Steps: []resource.TestStep{{
			Config: configuration,
			Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttr("data.redshift_database.destination", "database_type", "LOCAL"),
				resource.TestCheckResourceAttr("data.redshift_database.destination", "integration.integration_id", os.Getenv("REDSHIFT_ACC_MYSQL_INTEGRATION_ID")),
				resource.TestCheckNoResourceAttr("data.redshift_database.destination", "integration.source_database"),
			),
		}},
	})
}
