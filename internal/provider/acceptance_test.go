package provider

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/redshiftdata"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/redshiftdata"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAccLocalSQLLifecycle creates only uniquely named objects on an existing test workgroup.
func TestAccLocalSQLLifecycle(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("set TF_ACC=1 to run against a test Redshift workgroup")
	}
	region := os.Getenv("REDSHIFT_ACC_REGION")
	profile := os.Getenv("REDSHIFT_ACC_PROFILE")
	workgroup := os.Getenv("REDSHIFT_ACC_WORKGROUP")
	database := os.Getenv("REDSHIFT_ACC_DATABASE")
	require.NotEmpty(t, region)
	require.NotEmpty(t, profile)
	require.NotEmpty(t, workgroup)
	require.NotEmpty(t, database)

	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	dbName, schemaName, roleName := "acc_db_"+suffix, "acc_schema_"+suffix, "acc_role_"+suffix
	groupName, userName := "acc_group_"+suffix, "acc_user_"+suffix
	configSQL := func(privileges string) string {
		return fmt.Sprintf(`
provider "redshift" {
  region         = %q
  profile        = %q
  workgroup_name = %q
  database       = %q
}
resource "redshift_database" "local" { name = %q }
resource "redshift_schema" "local" {
  database = redshift_database.local.name
  name     = %q
}
resource "redshift_role" "reader" { name = %q }
resource "redshift_group" "readers" {
  name = %q
  # Identity DDL shares catalogs: order this fixture's user/group creation and destruction.
  depends_on = [redshift_user.reader]
}
resource "redshift_user" "reader" {
  name        = %q
  password_wo = %q
}
resource "redshift_group_membership" "reader" {
  group = redshift_group.readers.name
  user  = redshift_user.reader.name
}
resource "redshift_role_grant" "reader" {
  role = redshift_role.reader.name
  to_user = redshift_user.reader.name
  depends_on = [redshift_group_membership.reader]
}
data "redshift_role_grant" "reader" {
  role = redshift_role_grant.reader.role
  to_user = redshift_role_grant.reader.to_user
}
data "redshift_group" "readers" { name = redshift_group.readers.name }
resource "redshift_object_grant" "schema" {
  database_name = redshift_schema.local.database
  schema_name   = redshift_schema.local.name
  object_type   = "SCHEMA"
  grantee       = redshift_group.readers.name
  grantee_type  = "GROUP"
  privileges    = ["USAGE"]
}
resource "redshift_system_grant" "reader" {
  role       = redshift_role.reader.name
  privileges = ["CREATE ROLE"]
}
resource "redshift_default_privileges" "reader" {
  database_name = redshift_database.local.name
  owner         = redshift_user.reader.name
  object_type   = "TABLES"
  grantee       = redshift_role.reader.name
  grantee_type  = "ROLE"
  privileges    = ["SELECT"]
}
resource "redshift_grant" "schema" {
  database_name = redshift_schema.local.database
  schema_name   = redshift_schema.local.name
  role          = redshift_role.reader.name
  scope         = "SCHEMA"
  privileges    = %s
}
data "redshift_group_membership" "reader" {
  group = redshift_group_membership.reader.group
  user = redshift_group_membership.reader.user
}
data "redshift_object_grant" "schema" {
  database_name = redshift_object_grant.schema.database_name
  schema_name = redshift_object_grant.schema.schema_name
  object_type = redshift_object_grant.schema.object_type
  grantee = redshift_object_grant.schema.grantee
  grantee_type = redshift_object_grant.schema.grantee_type
}
data "redshift_system_grant" "reader" { role = redshift_system_grant.reader.role }
data "redshift_assumerole_grant" "reader" {
  iam_role_arn = "default"
  grantee = redshift_role.reader.name
  grantee_type = "ROLE"
}
data "redshift_default_privileges" "reader" {
  database_name = redshift_default_privileges.reader.database_name
  owner = redshift_default_privileges.reader.owner
  object_type = redshift_default_privileges.reader.object_type
  grantee = redshift_default_privileges.reader.grantee
  grantee_type = redshift_default_privileges.reader.grantee_type
}
data "redshift_grant" "schema" {
  database_name = redshift_grant.schema.database_name
  schema_name = redshift_grant.schema.schema_name
  role = redshift_grant.schema.role
  scope = redshift_grant.schema.scope
}
`, region, profile, workgroup, database, dbName, schemaName, roleName, groupName, userName, "AccPass1"+suffix, privileges)
	}
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region), config.WithSharedConfigProfile(profile))
	require.NoError(t, err)
	client := &dataapi.Client{API: redshiftdata.NewFromConfig(cfg), Workgroup: workgroup, Timeout: 5 * time.Minute, Poll: time.Second}
	connection := sqlclient.Connection{Database: database}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){
			"redshift": providerserver.NewProtocol6WithError(New("test")()),
		},
		CheckDestroy: func(*terraform.State) error {
			rows, err := client.Query(ctx, connection, "SELECT database_name FROM svv_redshift_databases WHERE database_name = :name", map[string]string{"name": dbName})
			if err != nil {
				return err
			}
			assert.Empty(t, rows)
			rows, err = client.Query(ctx, connection, "SELECT role_name FROM svv_roles WHERE role_name = :role", map[string]string{"role": roleName})
			assert.Empty(t, rows)
			if err != nil {
				return err
			}
			rows, err = client.Query(ctx, connection, "SELECT groname FROM pg_group WHERE groname = :name", map[string]string{"name": groupName})
			assert.Empty(t, rows)
			if err != nil {
				return err
			}
			rows, err = client.Query(ctx, connection, "SELECT usename FROM pg_user WHERE usename = :name", map[string]string{"name": userName})
			assert.Empty(t, rows)
			return err
		},
		Steps: []resource.TestStep{
			{Config: configSQL(`["USAGE"]`), Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttr("redshift_database.local", "name", dbName),
				resource.TestCheckResourceAttr("redshift_schema.local", "name", schemaName),
				resource.TestCheckResourceAttr("redshift_grant.schema", "privileges.#", "1"),
				resource.TestCheckResourceAttr("data.redshift_group_membership.reader", "exists", "true"),
				resource.TestCheckResourceAttr("data.redshift_role_grant.reader", "exists", "true"),
				resource.TestCheckResourceAttr("data.redshift_object_grant.schema", "privileges.#", "1"),
				resource.TestCheckResourceAttr("data.redshift_system_grant.reader", "privileges.#", "1"),
				resource.TestCheckResourceAttr("data.redshift_assumerole_grant.reader", "privileges.#", "0"),
				resource.TestCheckResourceAttr("data.redshift_default_privileges.reader", "privileges.#", "1"),
				resource.TestCheckResourceAttr("data.redshift_grant.schema", "privileges.#", "1"),
			)},
			{Config: configSQL(`["USAGE"]`), PlanOnly: true},
			{ResourceName: "redshift_database.local", ImportState: true, ImportStateVerify: true},
			{ResourceName: "redshift_schema.local", ImportState: true, ImportStateVerify: true},
			{ResourceName: "redshift_role.reader", ImportState: true, ImportStateVerify: true},
			{ResourceName: "redshift_grant.schema", ImportState: true, ImportStateVerify: true},
			{ResourceName: "redshift_group.readers", ImportState: true, ImportStateVerify: true},
			{ResourceName: "redshift_group_membership.reader", ImportState: true, ImportStateVerify: true},
			{ResourceName: "redshift_user.reader", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"password_wo_version"}},
			{ResourceName: "redshift_object_grant.schema", ImportState: true, ImportStateVerify: true},
			{ResourceName: "redshift_system_grant.reader", ImportState: true, ImportStateVerify: true},
			{ResourceName: "redshift_default_privileges.reader", ImportState: true, ImportStateVerify: true},
			{Config: configSQL(`["USAGE", "CREATE"]`), Check: resource.TestCheckResourceAttr("redshift_grant.schema", "privileges.#", "2")},
			{
				Config: configSQL(`["USAGE", "CREATE"]`), PlanOnly: true, ExpectNonEmptyPlan: true,
				PreConfig: func() {
					_, err := client.Query(ctx, sqlclient.Connection{Database: dbName}, "REVOKE CREATE ON SCHEMA "+sqlclient.Identifier(dbName)+"."+sqlclient.Identifier(schemaName)+" FROM ROLE "+sqlclient.Identifier(roleName), nil)
					require.NoError(t, err)
				},
			},
			{Config: configSQL(`["USAGE", "CREATE"]`), Check: resource.TestCheckResourceAttr("redshift_grant.schema", "privileges.#", "2")},
			{Config: configSQL(`["USAGE", "CREATE"]`), PlanOnly: true},
		},
	})
}
