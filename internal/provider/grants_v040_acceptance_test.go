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

// grantAcceptanceProvider renders the provider block for grant acceptance tests, adding secret_arn only when one is
// configured.
func grantAcceptanceProvider(region, profile, workgroup, database string) string {
	secret := ""
	if arn := os.Getenv("REDSHIFT_ACC_SECRET_ARN"); arn != "" {
		secret = fmt.Sprintf("secret_arn = %q", arn)
	}

	return fmt.Sprintf(`provider "redshift" {
  region = %q
  profile = %q
  workgroup_name = %q
  database = %q
  %s
}
`, region, profile, workgroup, database, secret)
}

// TestAccGrantRestoreDefaults checks that removing lockdown resources grants the documented defaults back while the
// isolated SQL database stays alive. Both languages and the connecting user's function defaults are observed afterward.
func TestAccGrantRestoreDefaults(t *testing.T) {
	region, profile, workgroup, database := testAccWorkgroup(t)
	name := "acc_restore_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	base := grantAcceptanceProvider(region, profile, workgroup, database) + fmt.Sprintf(`
resource "redshift_database" "local" { name = %q }
`, name)
	restricted := base + `
resource "redshift_language_grant" "public_sql" {
  database_name = redshift_database.local.name
  language_name = "SQL"
  grantee = "public"
  grantee_type = "PUBLIC"
  privileges = []
  restore_default_on_destroy = true
}
resource "redshift_language_grant" "public_plpgsql" {
  database_name = redshift_database.local.name
  language_name = "PLPGSQL"
  grantee = "public"
  grantee_type = "PUBLIC"
  privileges = []
  restore_default_on_destroy = true
}
resource "redshift_default_privileges" "public" {
  database_name = redshift_database.local.name
  object_type = "FUNCTIONS"
  grantee = "public"
  grantee_type = "PUBLIC"
  privileges = []
  restore_default_on_destroy = true
}
`
	restored := base + `
data "redshift_language_grant" "public_sql" {
  database_name = redshift_database.local.name
  language_name = "SQL"
  grantee = "public"
  grantee_type = "PUBLIC"
}
data "redshift_language_grant" "public_plpgsql" {
  database_name = redshift_database.local.name
  language_name = "PLPGSQL"
  grantee = "public"
  grantee_type = "PUBLIC"
}
data "redshift_default_privileges" "public" {
  database_name = redshift_database.local.name
  object_type = "FUNCTIONS"
  grantee = "public"
  grantee_type = "PUBLIC"
}
`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(New("test")())},
		Steps: []resource.TestStep{
			{Config: restricted, Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("redshift_language_grant.public_sql", "privileges.#", "0"),
				resource.TestCheckResourceAttr("redshift_default_privileges.public", "privileges.#", "0"),
			)},
			{Config: restricted, PlanOnly: true},
			{ResourceName: "redshift_default_privileges.public", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"restore_default_on_destroy"}},
			{Config: base},
			{Config: restored, Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.redshift_language_grant.public_sql", "privileges.#", "1"),
				resource.TestCheckResourceAttr("data.redshift_language_grant.public_plpgsql", "privileges.#", "1"),
				resource.TestCheckResourceAttr("data.redshift_default_privileges.public", "privileges.#", "1"),
			)},
		},
	})
}

// TestAccObjectGrantSharedDatabase consumes an existing WITH PERMISSIONS fixture without owning the datashare or
// database. Only its uniquely named SQL role and explicit grants are created and removed by the test.
func TestAccObjectGrantSharedDatabase(t *testing.T) {
	region, profile, workgroup, database := testAccWorkgroup(t, "REDSHIFT_ACC_SHARED_DATABASE", "REDSHIFT_ACC_SHARED_SCHEMA", "REDSHIFT_ACC_SHARED_TABLE")
	name := "acc_shared_grant_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	configuration := func(privileges string) string {
		return grantAcceptanceProvider(region, profile, workgroup, database) + fmt.Sprintf(`
resource "redshift_role" "reader" { name = %[1]q }
resource "redshift_object_grant" "database" {
  database_name = %[2]q
  database_type = "shared"
  object_type = "DATABASE"
  grantee = redshift_role.reader.name
  grantee_type = "ROLE"
  privileges = ["USAGE"]
}
resource "redshift_object_grant" "schema" {
  database_name = %[2]q
  database_type = "shared"
  schema_name = %[3]q
  object_type = "SCHEMA"
  grantee = redshift_role.reader.name
  grantee_type = "ROLE"
  privileges = ["USAGE"]
}
resource "redshift_object_grant" "table" {
  database_name = %[2]q
  database_type = "shared"
  schema_name = %[3]q
  object_name = %[4]q
  object_type = "TABLE"
  grantee = redshift_role.reader.name
  grantee_type = "ROLE"
  privileges = [%[5]s]
}
data "redshift_object_grant" "table" {
  database_name = redshift_object_grant.table.database_name
  database_type = redshift_object_grant.table.database_type
  schema_name = redshift_object_grant.table.schema_name
  object_name = redshift_object_grant.table.object_name
  object_type = "TABLE"
  grantee = redshift_role.reader.name
  grantee_type = "ROLE"
  depends_on = [redshift_object_grant.table]
}
`, name, os.Getenv("REDSHIFT_ACC_SHARED_DATABASE"), os.Getenv("REDSHIFT_ACC_SHARED_SCHEMA"), os.Getenv("REDSHIFT_ACC_SHARED_TABLE"), privileges)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(New("test")())},
		Steps: []resource.TestStep{
			{Config: configuration(`"SELECT"`), Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("redshift_object_grant.database", "privileges.#", "1"),
				resource.TestCheckResourceAttr("redshift_object_grant.schema", "privileges.#", "1"),
				resource.TestCheckResourceAttr("data.redshift_object_grant.table", "privileges.#", "1"),
			)},
			{Config: configuration(`"SELECT"`), PlanOnly: true},
			{ResourceName: "redshift_object_grant.table", ImportState: true, ImportStateVerify: true},
			{Config: configuration(``), Check: resource.TestCheckResourceAttr("data.redshift_object_grant.table", "privileges.#", "0")},
		},
	})
}
