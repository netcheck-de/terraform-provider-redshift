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

// TestAccGrantsUserOptionsAndScopes checks against a live warehouse what the offline fakes assume: SHOW GRANTS ON …
// FOR a user reports admin_option, the LANGUAGES, COPY JOBS, and TEMPLATES scopes read back under their scope names,
// a schema snapshot reads the privileges common to its tables, and the implicit PUBLIC EXECUTE on new functions is
// reported until revoked. The scoped grant and default privilege listings report the same tuples. Every object is
// uniquely named and dropped afterwards.
func TestAccGrantsUserOptionsAndScopes(t *testing.T) {
	region, profile, workgroup, database := testAccWorkgroup(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	configuration := func(options string, publicExecute string) string {
		return fmt.Sprintf(`
provider "redshift" {
  region         = %q
  profile        = %q
  workgroup_name = %q
  database       = %q
}
# The revoked PUBLIC default stays in the database and depends on its owner, so the database is dropped before the
# user: DROP USER fails while the entry exists.
resource "redshift_database" "local" {
  name       = "acc_grants_%[5]s"
  depends_on = [redshift_user.analyst]
}
resource "redshift_schema" "local" {
  database = redshift_database.local.name
  name     = "acc_grants_%[5]s"
}
resource "redshift_role" "reader" { name = "acc_grants_role_%[5]s" }
resource "redshift_user" "analyst" {
  name        = "acc_grants_user_%[5]s"
  password_wo = "Acc1Grants%[5]s"
}
resource "redshift_grant" "analyst_tables" {
  database_name           = redshift_schema.local.database
  schema_name             = redshift_schema.local.name
  user                    = redshift_user.analyst.name
  scope                   = "TABLES"
  privileges              = ["INSERT", "SELECT"]
  grant_option_privileges = [%[6]s]
}
resource "redshift_grant" "languages" {
  database_name = redshift_database.local.name
  role          = redshift_role.reader.name
  scope         = "LANGUAGES"
  privileges    = ["USAGE"]
}
resource "redshift_grant" "copy_jobs" {
  database_name = redshift_database.local.name
  user          = redshift_user.analyst.name
  scope         = "COPY JOBS"
  privileges    = ["CREATE"]
}
resource "redshift_grant" "templates" {
  database_name = redshift_schema.local.database
  schema_name   = redshift_schema.local.name
  role          = redshift_role.reader.name
  scope         = "TEMPLATES"
  privileges    = ["USAGE"]
}
resource "redshift_default_privilege" "public_functions" {
  database_name = redshift_database.local.name
  owner         = redshift_user.analyst.name
  object_type   = "FUNCTIONS"
  grantee       = "public"
  grantee_type  = "PUBLIC"
  privileges    = [%[7]s]
}
data "redshift_grants" "database" {
  database_name = redshift_database.local.name
  depends_on    = [redshift_grant.languages, redshift_grant.copy_jobs]
}
data "redshift_grants" "analyst_schema" {
  database_name = redshift_schema.local.database
  schema_name   = redshift_schema.local.name
  user          = redshift_user.analyst.name
  depends_on    = [redshift_grant.analyst_tables]
}
data "redshift_default_privileges" "analyst" {
  database_name = redshift_database.local.name
  owner         = redshift_user.analyst.name
  depends_on    = [redshift_default_privilege.public_functions]
}
`, region, profile, workgroup, database, suffix, options, publicExecute)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(New("test")())},
		Steps: []resource.TestStep{
			{Config: configuration(`"SELECT"`, `"EXECUTE"`), Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("redshift_grant.analyst_tables", "grant_option_privileges.#", "1"),
				resource.TestCheckResourceAttr("redshift_default_privilege.public_functions", "privileges.#", "1"),
				resource.TestCheckTypeSetElemNestedAttrs("data.redshift_grants.database", "grants.*", map[string]string{"scope": "LANGUAGES", "role": "acc_grants_role_" + suffix, "privileges.#": "1"}),
				resource.TestCheckTypeSetElemNestedAttrs("data.redshift_grants.database", "grants.*", map[string]string{"scope": "COPY JOBS", "user": "acc_grants_user_" + suffix, "privileges.#": "1"}),
				resource.TestCheckResourceAttr("data.redshift_grants.analyst_schema", "grants.#", "1"),
				resource.TestCheckResourceAttr("data.redshift_grants.analyst_schema", "grants.0.scope", "TABLES"),
				resource.TestCheckResourceAttr("data.redshift_grants.analyst_schema", "grants.0.privileges.#", "2"),
				resource.TestCheckTypeSetElemAttr("data.redshift_grants.analyst_schema", "grants.0.grant_option_privileges.*", "SELECT"),
				resource.TestCheckTypeSetElemNestedAttrs("data.redshift_default_privileges.analyst", "default_privileges.*", map[string]string{"object_type": "FUNCTIONS", "grantee_type": "PUBLIC", "privileges.#": "1"}),
			)},
			{Config: configuration(`"SELECT"`, `"EXECUTE"`), PlanOnly: true},
			{ResourceName: "redshift_grant.analyst_tables", ImportState: true, ImportStateVerify: true},
			{ResourceName: "redshift_grant.copy_jobs", ImportState: true, ImportStateVerify: true},
			{Config: configuration(``, ``), Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("redshift_grant.analyst_tables", "grant_option_privileges.#", "0"),
				resource.TestCheckResourceAttr("redshift_default_privilege.public_functions", "privileges.#", "0"),
				resource.TestCheckResourceAttr("data.redshift_grants.analyst_schema", "grants.0.grant_option_privileges.#", "0"),
				resource.TestCheckResourceAttr("data.redshift_default_privileges.analyst", "default_privileges.#", "0"),
			)},
			{Config: configuration(``, ``), PlanOnly: true},
		},
	})
}

// TestAccObjectGrantRoutinesAndSnapshots grants EXECUTE on one function overload and SELECT on all tables of a
// schema. REDSHIFT_ACC_ROUTINE_SCHEMA names an existing schema of the admin database holding the function
// f_acc_grant(integer) and at least one table.
func TestAccObjectGrantRoutinesAndSnapshots(t *testing.T) {
	region, profile, workgroup, database := testAccWorkgroup(t, "REDSHIFT_ACC_ROUTINE_SCHEMA")
	schema := os.Getenv("REDSHIFT_ACC_ROUTINE_SCHEMA")
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	configuration := fmt.Sprintf(`
provider "redshift" {
  region         = %q
  profile        = %q
  workgroup_name = %q
  database       = %q
}
resource "redshift_user" "analyst" {
  name        = "acc_routine_user_%[5]s"
  password_wo = "Acc1Routine%[5]s"
}
resource "redshift_object_grant" "function" {
  database_name           = %[4]q
  schema_name             = %[6]q
  object_name             = "f_acc_grant"
  object_type             = "FUNCTION"
  arguments               = "int4"
  grantee                 = redshift_user.analyst.name
  grantee_type            = "USER"
  privileges              = ["EXECUTE"]
  grant_option_privileges = ["EXECUTE"]
}
resource "redshift_object_grant" "tables" {
  database_name = %[4]q
  schema_name   = %[6]q
  object_type   = "ALL TABLES"
  grantee       = redshift_user.analyst.name
  grantee_type  = "USER"
  privileges    = ["SELECT"]
}
`, region, profile, workgroup, database, suffix, schema)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(New("test")())},
		Steps: []resource.TestStep{
			{Config: configuration, Check: resource.TestCheckResourceAttr("redshift_object_grant.function", "grant_option_privileges.#", "1")},
			{Config: configuration, PlanOnly: true},
			{ResourceName: "redshift_object_grant.function", ImportState: true, ImportStateVerify: true},
		},
	})
}

// TestAccAssumeroleGrantAllRoles checks that ON ALL grants read back through the default role's catalog entry, which
// the listing reports as ALL. It requires a warehouse whose PUBLIC ASSUMEROLE default is already revoked.
func TestAccAssumeroleGrantAllRoles(t *testing.T) {
	region, profile, workgroup, database := testAccWorkgroup(t, "REDSHIFT_ACC_ASSUMEROLE")
	name := "acc_iam_all_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	configuration := fmt.Sprintf(`
provider "redshift" {
  region         = %q
  profile        = %q
  workgroup_name = %q
  database       = %q
}
resource "redshift_role" "loader" { name = %q }
resource "redshift_assumerole_grant" "all" {
  iam_role_arn = "ALL"
  grantee      = redshift_role.loader.name
  grantee_type = "ROLE"
  privileges   = ["COPY", "UNLOAD"]
}
data "redshift_assumerole_grants" "all" {
  iam_role_arn = "all"
  grantee      = redshift_assumerole_grant.all.grantee
}
`, region, profile, workgroup, database, name)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(New("test")())},
		Steps: []resource.TestStep{
			{Config: configuration, Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("redshift_assumerole_grant.all", "privileges.#", "2"),
				resource.TestCheckResourceAttr("data.redshift_assumerole_grants.all", "assumerole_grants.#", "1"),
				resource.TestCheckResourceAttr("data.redshift_assumerole_grants.all", "assumerole_grants.0.iam_role_arn", "ALL"),
				resource.TestCheckResourceAttr("data.redshift_assumerole_grants.all", "assumerole_grants.0.grantee_type", "ROLE"),
				resource.TestCheckResourceAttr("data.redshift_assumerole_grants.all", "assumerole_grants.0.privileges.#", "2"),
			)},
			{Config: configuration, PlanOnly: true},
		},
	})
}
