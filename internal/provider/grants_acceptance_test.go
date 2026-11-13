package provider

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccGrantsUserOptionsAndScopes checks against a live warehouse what the offline fakes assume: SHOW GRANTS ON …
// FOR a user reports admin_option, the LANGUAGES, COPY JOBS, and TEMPLATES scopes read back under their scope names,
// a schema snapshot reads the privileges common to its tables, and the implicit PUBLIC EXECUTE on new functions is
// reported until revoked. The scoped grant and default privilege listings report the same tuples. Every object is
// uniquely named and dropped afterwards.
func TestAccGrantsUserOptionsAndScopes(t *testing.T) {
	t.Parallel()

	region, workgroup, database := testAccWorkgroup(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	configuration := func(options string, publicExecute string) string {
		return fmt.Sprintf(`
provider "redshift" {
  region         = %q
  workgroup_name = %q
  database       = %q
}
# The revoked PUBLIC default stays in the database and depends on its owner, so the database is dropped before the
# user: DROP USER fails while the entry exists.
resource "redshift_database" "local" {
  name       = "acc_grants_%[4]s"
  depends_on = [redshift_user.analyst]
}
resource "redshift_schema" "local" {
  database = redshift_database.local.name
  name     = "acc_grants_%[4]s"
}
resource "redshift_role" "reader" { name = "acc_grants_role_%[4]s" }
resource "redshift_user" "analyst" {
  name        = "acc_grants_user_%[4]s"
  password_wo = "Acc1Grants%[4]s"
}
resource "redshift_grant" "analyst_tables" {
  database_name           = redshift_schema.local.database
  schema_name             = redshift_schema.local.name
  user                    = redshift_user.analyst.name
  scope                   = "TABLES"
  privileges              = ["INSERT", "SELECT"]
  grant_option_privileges = [%[5]s]
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
  privileges    = [%[6]s]
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
  object_type   = "FUNCTIONS"
  grantee_type  = "PUBLIC"
  depends_on    = [redshift_default_privilege.public_functions]
}
`, region, workgroup, database, suffix, options, publicExecute)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": testAccProviderServer},
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
			{ResourceName: "redshift_grant.analyst_tables", ImportState: true, ImportStateVerify: true},
			{ResourceName: "redshift_grant.copy_jobs", ImportState: true, ImportStateVerify: true},
			{Config: configuration(``, ``), Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("redshift_grant.analyst_tables", "grant_option_privileges.#", "0"),
				resource.TestCheckResourceAttr("redshift_default_privilege.public_functions", "privileges.#", "0"),
				resource.TestCheckResourceAttr("data.redshift_grants.analyst_schema", "grants.0.grant_option_privileges.#", "0"),
				resource.TestCheckResourceAttr("data.redshift_default_privileges.analyst", "default_privileges.#", "0"),
			)},
		},
	})
}

// TestAccObjectGrantRoutinesAndSnapshots grants EXECUTE on one function overload and SELECT on all tables of a
// schema. The routines and source table belong to this test, not an externally provisioned SQL fixture.
func TestAccObjectGrantRoutinesAndSnapshots(t *testing.T) {
	t.Parallel()

	region, workgroup, database := testAccWorkgroup(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	configuration := fmt.Sprintf(`
provider "redshift" {
  region         = %q
  workgroup_name = %q
  database       = %q
}
resource "redshift_user" "analyst" {
  name        = "acc_routine_user_%[4]s"
  password_wo = "Acc1Routine%[4]s"
}
resource "redshift_schema" "fixture" {
  database = %[3]q
  name = "acc_routine_schema_%[4]s"
}
resource "redshift_function" "fixture" {
  database = redshift_schema.fixture.database
  schema = redshift_schema.fixture.name
  name = "f_acc_grant"
  arguments = ["INTEGER"]
  return_type = "INTEGER"
  body = "SELECT $1"
}
resource "redshift_table" "fixture" {
  database = redshift_schema.fixture.database
  schema = redshift_schema.fixture.name
  name = "events"
  column {
    name = "id"
    type = "INTEGER"
  }
}
resource "redshift_object_grant" "function" {
  database_name           = redshift_function.fixture.database
  schema_name             = redshift_function.fixture.schema
  object_name             = redshift_function.fixture.name
  object_type             = "FUNCTION"
  arguments               = "int4"
  grantee                 = redshift_user.analyst.name
  grantee_type            = "USER"
  privileges              = ["EXECUTE"]
  grant_option_privileges = ["EXECUTE"]
}
resource "redshift_object_grant" "tables" {
  database_name = redshift_table.fixture.database
  schema_name   = redshift_table.fixture.schema
  object_type   = "ALL TABLES"
  grantee       = redshift_user.analyst.name
  grantee_type  = "USER"
  privileges    = ["SELECT"]
}
`, region, workgroup, database, suffix)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": testAccProviderServer},
		Steps: []resource.TestStep{
			{Config: configuration, Check: resource.TestCheckResourceAttr("redshift_object_grant.function", "grant_option_privileges.#", "1")},
			{ResourceName: "redshift_object_grant.function", ImportState: true, ImportStateVerify: true},
		},
	})
}

// TestAccAssumeroleGrantAllRoles checks that ON ALL grants read back through the default role's catalog entry, which
// the listing reports as ALL. It requires a warehouse whose PUBLIC ASSUMEROLE default is already revoked.
func TestAccAssumeroleGrantAllRoles(t *testing.T) {
	t.Parallel()

	region, workgroup, database := testAccWorkgroup(t, "REDSHIFT_ACC_ASSUMEROLE")
	name := "acc_iam_all_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	configuration := fmt.Sprintf(`
provider "redshift" {
  region         = %q
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
  grantee_type = "ROLE"
}
data "redshift_assumerole_grants" "wrong_type" {
  grantee = redshift_assumerole_grant.all.grantee
  grantee_type = "USER"
}
`, region, workgroup, database, name)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": testAccProviderServer},
		Steps: []resource.TestStep{
			{Config: configuration, Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("redshift_assumerole_grant.all", "privileges.#", "2"),
				resource.TestCheckResourceAttr("data.redshift_assumerole_grants.all", "assumerole_grants.#", "1"),
				resource.TestCheckResourceAttr("data.redshift_assumerole_grants.wrong_type", "assumerole_grants.#", "0"),
				resource.TestCheckResourceAttr("data.redshift_assumerole_grants.all", "assumerole_grants.0.iam_role_arn", "ALL"),
				resource.TestCheckResourceAttr("data.redshift_assumerole_grants.all", "assumerole_grants.0.grantee_type", "ROLE"),
				resource.TestCheckResourceAttr("data.redshift_assumerole_grants.all", "assumerole_grants.0.privileges.#", "2"),
			)},
		},
	})
}
