package provider

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/redshiftdata"
	"github.com/aws/aws-sdk-go-v2/service/redshiftserverless"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/netcheck-de/terraform-provider-redshift/internal/redshiftconn"
	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/redshiftdata"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/require"
)

// TestAccPermissionsLifecycle verifies column and language grants, their lookups and listings, imports, drift repair,
// and revocation on destroy against a disposable database.
func TestAccPermissionsLifecycle(t *testing.T) {
	region, profile, workgroup, database := testAccWorkgroup(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	name := "acc_permissions_" + suffix
	configuration := func(enabled bool) string {
		base := fmt.Sprintf(`
provider "redshift" {
  region = %q
  profile = %q
  workgroup_name = %q
  database = %q
}
resource "redshift_database" "local" { name = %q }
resource "redshift_schema" "local" {
  database = redshift_database.local.name
  name = "serving"
}
resource "redshift_role" "readers" { name = "acc_readers_%[6]s" }
resource "redshift_group" "readers" { name = "acc_reader_group_%[6]s" }
`, region, profile, workgroup, database, name, suffix)
		if !enabled {
			return base
		}
		return base + `
resource "redshift_column_grant" "role" {
  database_name = redshift_database.local.name
  schema_name = redshift_schema.local.name
  object_name = "fixture_table"
  grantee = redshift_role.readers.name
  grantee_type = "ROLE"
  privileges = { SELECT = ["id", "label"], UPDATE = ["label"] }
}
resource "redshift_column_grant" "group_view" {
  database_name = redshift_database.local.name
  schema_name = redshift_schema.local.name
  object_name = "fixture_view"
  grantee = redshift_group.readers.name
  grantee_type = "GROUP"
  privileges = { SELECT = ["label"] }
}
resource "redshift_language_grant" "role" {
  database_name = redshift_database.local.name
  language_name = "plpgsql"
  grantee = redshift_role.readers.name
  grantee_type = "ROLE"
  privileges = ["USAGE"]
}
data "redshift_column_grant" "role" {
  database_name = redshift_column_grant.role.database_name
  schema_name = redshift_column_grant.role.schema_name
  object_name = redshift_column_grant.role.object_name
  grantee = redshift_column_grant.role.grantee
  grantee_type = redshift_column_grant.role.grantee_type
}
data "redshift_language_grant" "role" {
  database_name = redshift_language_grant.role.database_name
  language_name = redshift_language_grant.role.language_name
  grantee = redshift_language_grant.role.grantee
  grantee_type = redshift_language_grant.role.grantee_type
}
data "redshift_column_grants" "serving" {
  database_name = redshift_database.local.name
  schema_name = redshift_schema.local.name
  depends_on = [redshift_column_grant.role, redshift_column_grant.group_view]
}
data "redshift_grants" "role" {
  database_name = redshift_database.local.name
  grantee = redshift_role.readers.name
  grantee_type = "ROLE"
  depends_on = [redshift_language_grant.role]
}
data "redshift_grants" "table" {
  database_name = redshift_database.local.name
  object_type = "TABLE"
  schema_name = redshift_schema.local.name
  object_name = "fixture_table"
  depends_on = [redshift_column_grant.role]
}`
	}
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region), config.WithSharedConfigProfile(profile))
	require.NoError(t, err)
	client := &dataapi.Client{API: redshiftdata.NewFromConfig(cfg), Workgroup: workgroup, Timeout: 5 * time.Minute, Poll: time.Second}
	admin, target := sqlclient.Connection{Database: database}, sqlclient.Connection{Database: name}
	// A failed test may leave the fixture database behind; dropping it removes its tables and grants.
	t.Cleanup(func() {
		rows, err := client.Query(ctx, admin, "SELECT database_name FROM svv_redshift_databases WHERE database_name = :name", map[string]string{"name": name})
		require.NoError(t, err)
		if len(rows) != 0 {
			_, err = client.Query(ctx, admin, "DROP DATABASE "+sqlclient.Identifier(name), nil)
			require.NoError(t, err)
		}
	})
	exec := func(sql string) {
		_, err := client.Query(ctx, target, sql, nil)
		require.NoError(t, err)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(New("test")())},
		Steps: []resource.TestStep{
			{Config: configuration(false)},
			{Config: configuration(true), PreConfig: func() {
				exec(`CREATE TABLE serving.fixture_table (id INTEGER, label VARCHAR(64), secret VARCHAR(64))`)
				exec(`CREATE VIEW serving.fixture_view AS SELECT id, label FROM serving.fixture_table`)
			}, Check: resource.ComposeTestCheckFunc(
				resource.TestCheckTypeSetElemAttr("data.redshift_column_grant.role", "privileges.SELECT.*", "label"),
				resource.TestCheckTypeSetElemAttr("data.redshift_column_grant.role", "privileges.UPDATE.*", "label"),
				resource.TestCheckTypeSetElemAttr("data.redshift_language_grant.role", "privileges.*", "USAGE"),
				resource.TestCheckResourceAttr("data.redshift_column_grants.serving", "column_grants.#", "4"),
			)},
			{Config: configuration(true), PlanOnly: true},
			{ResourceName: "redshift_column_grant.role", ImportState: true, ImportStateVerify: true},
			{ResourceName: "redshift_column_grant.group_view", ImportState: true, ImportStateVerify: true},
			{ResourceName: "redshift_language_grant.role", ImportState: true, ImportStateVerify: true},
			{Config: configuration(true), PlanOnly: true, ExpectNonEmptyPlan: true, PreConfig: func() {
				exec(fmt.Sprintf(`REVOKE SELECT (label) ON TABLE serving.fixture_table FROM ROLE %s`, sqlclient.Identifier("acc_readers_"+suffix)))
				exec(fmt.Sprintf(`GRANT SELECT (secret) ON TABLE serving.fixture_table TO ROLE %s`, sqlclient.Identifier("acc_readers_"+suffix)))
			}},
			{Config: configuration(true)},
			{Config: configuration(true), PlanOnly: true},
			{Config: configuration(false), Check: func(*terraform.State) error {
				rows, err := client.Query(ctx, target, "SELECT column_name FROM svv_column_privileges WHERE relation_name IN ('fixture_table', 'fixture_view')", nil)
				require.NoError(t, err)
				require.Empty(t, rows, "destroy revokes every owned column privilege")
				rows, err = client.Query(ctx, target, "SELECT language_name FROM svv_language_privileges WHERE identity_name = :name", map[string]string{"name": "acc_readers_" + suffix})
				require.NoError(t, err)
				require.Empty(t, rows, "destroy revokes language usage")
				exec(`DROP VIEW serving.fixture_view`)
				exec(`DROP TABLE serving.fixture_table`)
				return nil
			}},
		},
	})
}

// TestAccLanguageGrantPublicDefault verifies that a PUBLIC tuple without USAGE removes Redshift's built-in PUBLIC USAGE,
// which SVV_LANGUAGE_PRIVILEGES may not list, by creating procedures as a disposable non-superuser over a direct
// connection before the revoke, after it, and after destroy.
func TestAccLanguageGrantPublicDefault(t *testing.T) {
	region, profile, workgroup, database := testAccWorkgroup(t, "REDSHIFT_ACC_DIRECT")
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region), config.WithSharedConfigProfile(profile))
	require.NoError(t, err)
	routing, err := (&redshiftconn.IAM{Workgroup: workgroup, Serverless: redshiftserverless.NewFromConfig(cfg)}).Resolve(ctx, database)
	require.NoError(t, err)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	name, userName := "acc_language_"+suffix, "acc_language_user_"+suffix
	secret := make([]byte, 16)
	_, err = rand.Read(secret)
	require.NoError(t, err)
	password := "Aa1" + hex.EncodeToString(secret)
	admin := &dataapi.Client{API: redshiftdata.NewFromConfig(cfg), Workgroup: workgroup, Timeout: 5 * time.Minute, Poll: time.Second}
	adminDB, target := sqlclient.Connection{Database: database}, sqlclient.Connection{Database: name}
	// Cleanups run last in first out: the database, with the probe's procedures, goes before its owner.
	t.Cleanup(func() {
		rows, err := admin.Query(ctx, adminDB, "SELECT usename FROM pg_user WHERE usename = :name", map[string]string{"name": userName})
		require.NoError(t, err)
		if len(rows) != 0 {
			_, err = admin.Query(ctx, adminDB, "DROP USER "+sqlclient.Identifier(userName), nil)
			require.NoError(t, err)
		}
	})
	t.Cleanup(func() {
		rows, err := admin.Query(ctx, adminDB, "SELECT database_name FROM svv_redshift_databases WHERE database_name = :name", map[string]string{"name": name})
		require.NoError(t, err)
		if len(rows) != 0 {
			_, err = admin.Query(ctx, adminDB, "DROP DATABASE "+sqlclient.Identifier(name), nil)
			require.NoError(t, err)
		}
	})
	_, err = admin.Query(ctx, adminDB, "CREATE USER "+sqlclient.Identifier(userName)+" PASSWORD "+sqlclient.Literal(password)+" NOCREATEDB NOCREATEUSER", nil)
	require.NoError(t, err)
	probe := &redshiftconn.Client{Credentials: redshiftconn.Credentials{Host: routing.Host, Port: routing.Port, Username: userName, Password: password}, SSLMode: testAccSSLMode(), Timeout: 30 * time.Second}
	// Every user may create in the public schema by default, so only language USAGE decides the outcome.
	createProcedure := func(procedure string) error {
		_, err := probe.Query(ctx, target, "CREATE PROCEDURE public."+procedure+"() AS $$ BEGIN RAISE INFO 'probe'; END; $$ LANGUAGE plpgsql", nil)
		return err
	}
	configuration := func(restricted bool) string {
		base := fmt.Sprintf(`
provider "redshift" {
  region = %q
  profile = %q
  workgroup_name = %q
  database = %q
}
resource "redshift_database" "local" { name = %q }
`, region, profile, workgroup, database, name)
		if !restricted {
			return base
		}
		return base + `
resource "redshift_language_grant" "public_plpgsql" {
  database_name = redshift_database.local.name
  language_name = "plpgsql"
  grantee = "public"
  grantee_type = "PUBLIC"
  privileges = []
}
resource "redshift_language_grant" "public_sql" {
  database_name = redshift_database.local.name
  language_name = "sql"
  grantee = "public"
  grantee_type = "PUBLIC"
  privileges = []
}`
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(New("test")())},
		Steps: []resource.TestStep{
			{Config: configuration(false), Check: func(*terraform.State) error {
				rows, err := admin.Query(ctx, target, "SELECT language_name FROM svv_language_privileges WHERE identity_type = 'public'", nil)
				require.NoError(t, err)
				t.Logf("SVV_LANGUAGE_PRIVILEGES rows for the PUBLIC default: %v", rows)
				require.NoError(t, createProcedure("sp_acc_default"), "PUBLIC holds USAGE on plpgsql by default")
				return nil
			}},
			{Config: configuration(true), Check: func(*terraform.State) error {
				require.ErrorContains(t, createProcedure("sp_acc_restricted"), "permission denied", "the PUBLIC tuple revoked the default")
				return nil
			}},
			{Config: configuration(true), PlanOnly: true},
			{Config: configuration(false), Check: func(*terraform.State) error {
				require.ErrorContains(t, createProcedure("sp_acc_destroyed"), "permission denied", "destroy does not restore the default")
				return nil
			}},
		},
	})
}
