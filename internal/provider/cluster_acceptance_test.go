package provider

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/redshiftdata"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/redshiftdata"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/require"
)

// TestAccClusterTransportLifecycle exercises provisioned Data API, optional secret authentication, and direct IAM fixtures.
func TestAccClusterTransportLifecycle(t *testing.T) {
	testAccPreCheck(t, "REDSHIFT_ACC_CLUSTER")

	region := os.Getenv("REDSHIFT_ACC_REGION")
	cluster, database, user := os.Getenv("REDSHIFT_ACC_CLUSTER"), os.Getenv("REDSHIFT_ACC_DATABASE"), os.Getenv("REDSHIFT_ACC_DB_USER")

	require.NotEmpty(t, region)
	require.NotEmpty(t, database)
	require.NotEmpty(t, user)

	name := "acc_cluster_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	require.NoError(t, err)

	client := &dataapi.Client{API: redshiftdata.NewFromConfig(cfg), Cluster: cluster, DBUser: user, Timeout: 5 * time.Minute, Poll: time.Second}
	configuration := func(mode string) string {
		selector := fmt.Sprintf("cluster_identifier = %q\n db_user = %q", cluster, user)
		if mode == "secret" {
			selector = fmt.Sprintf("cluster_identifier = %q\n secret_arn = %q", cluster, testAccSecretARN(true))
		}

		if mode == "direct" {
			selector = fmt.Sprintf("direct_connection {\n %s iam {\n cluster_identifier = %q\n db_user = %q\n }\n}", testAccSSLModeHCL(), cluster, user)
		}

		return fmt.Sprintf(`
provider "redshift" {
  region = %q
  database = %q
  %s
}
resource "redshift_database" "local" { name = %q }
resource "redshift_schema" "local" {
  database = redshift_database.local.name
  name = "serving"
}
resource "redshift_role" "reader" { name = %q }
resource "redshift_grant" "schema" {
  database_name = redshift_schema.local.database
  schema_name = redshift_schema.local.name
  role = redshift_role.reader.name
  scope = "SCHEMA"
  privileges = ["USAGE"]
}`, region, database, selector, name, name+"_reader")
	}

	steps := []resource.TestStep{
		{Config: configuration("data_api")},
		{Config: configuration("data_api"), PlanOnly: true},
		{ResourceName: "redshift_database.local", ImportState: true, ImportStateVerify: true},
		{ResourceName: "redshift_schema.local", ImportState: true, ImportStateVerify: true},
		{ResourceName: "redshift_role.reader", ImportState: true, ImportStateVerify: true},
		{ResourceName: "redshift_grant.schema", ImportState: true, ImportStateVerify: true},
		{Config: configuration("data_api"), PreConfig: func() {
			statement := sqlclient.Stmt("REVOKE USAGE ON SCHEMA").Ident("serving").Kw("FROM ROLE").Ident(name + "_reader").String()
			_, err := client.Query(ctx, sqlclient.Connection{Database: name}, statement, nil)
			require.NoError(t, err)
		}, Check: resource.TestCheckResourceAttr("redshift_grant.schema", "privileges.#", "1")},
	}
	if testAccSecretARN(true) != "" {
		steps = append(steps, resource.TestStep{Config: configuration("secret"), PlanOnly: true}, resource.TestStep{Config: configuration("secret")})
	}

	if os.Getenv("REDSHIFT_ACC_DIRECT") == "1" {
		steps = append(steps, resource.TestStep{Config: configuration("direct"), PlanOnly: true}, resource.TestStep{Config: configuration("direct")}, resource.TestStep{Config: configuration("data_api"), PlanOnly: true})
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(New("test")())},
		Steps:                    steps,
	})
}

// TestAccClusterUserLockoutsRollout exercises Terraform listings through manual lock and unlock of a disposable password
// user. The explicit flag requires a provisioned cluster with lockout security enabled and a superuser identity.
func TestAccClusterUserLockoutsRollout(t *testing.T) {
	testAccPreCheck(t, "REDSHIFT_ACC_CLUSTER", "REDSHIFT_ACC_USER_LOCKOUTS")

	region := os.Getenv("REDSHIFT_ACC_REGION")
	require.NotEmpty(t, region)

	database := os.Getenv("REDSHIFT_ACC_DATABASE")
	require.NotEmpty(t, database)

	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	require.NoError(t, err)

	client := &dataapi.Client{API: redshiftdata.NewFromConfig(cfg), Cluster: os.Getenv("REDSHIFT_ACC_CLUSTER"), DBUser: os.Getenv("REDSHIFT_ACC_DB_USER"), SecretARN: testAccSecretARN(true), Timeout: time.Minute, Poll: time.Second}

	rows, err := client.Query(ctx, sqlclient.Connection{Database: database}, readUserLockoutsStatement(), nil)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "account lockout security is not supported") {
		t.Skipf("SHOW USER LOCKOUT has not rolled out on this provisioned warehouse: %v", err)
	}

	require.NoError(t, err)
	_, err = userLockoutsItems("", rows)
	require.NoError(t, err)

	name := "acc_lockout_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	password := "Acc1!" + name

	selector := fmt.Sprintf("db_user = %q", os.Getenv("REDSHIFT_ACC_DB_USER"))
	if secret := testAccSecretARN(true); secret != "" {
		selector = fmt.Sprintf("secret_arn = %q", secret)
	} else {
		require.NotEmpty(t, os.Getenv("REDSHIFT_ACC_DB_USER"))
	}

	configuration := fmt.Sprintf(`
provider "redshift" {
  region = %q
  cluster_identifier = %q
  database = %q
  %s
}
resource "redshift_user" "lockout" {
  name = %q
  password_wo = %q
}
data "redshift_user_lockouts" "user" {
  user_name = redshift_user.lockout.name
  depends_on = [redshift_user.lockout]
}
data "redshift_user_lockouts" "all" {
  depends_on = [redshift_user.lockout]
}
data "redshift_user_lockouts" "missing" {
  user_name = %q
  depends_on = [redshift_user.lockout]
}
output "fixture_is_listed" {
  value = contains([for user in data.redshift_user_lockouts.all.user_lockouts : user.user_name], redshift_user.lockout.name)
}
`, region, os.Getenv("REDSHIFT_ACC_CLUSTER"), database, selector, name, password, name+"_missing")

	const address = "data.redshift_user_lockouts.user"

	unlocked := resource.ComposeAggregateTestCheckFunc(
		resource.TestCheckResourceAttr(address, "user_lockouts.#", "1"),
		resource.TestCheckResourceAttr(address, "user_lockouts.0.user_name", name),
		resource.TestCheckResourceAttr(address, "user_lockouts.0.is_locked", "false"),
		resource.TestCheckResourceAttr(address, "user_lockouts.0.failed_login_attempts", "0"),
		resource.TestCheckNoResourceAttr(address, "user_lockouts.0.lock_reason"),
		resource.TestCheckResourceAttr("data.redshift_user_lockouts.missing", "user_lockouts.#", "0"),
		resource.TestCheckOutput("fixture_is_listed", "true"),
	)
	exec := func(statement sqlclient.Statement) {
		require.NoError(t, statement.Err())
		_, err := client.Query(ctx, sqlclient.Connection{Database: database}, statement.String(), nil)
		require.NoError(t, err)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(New("test")())},
		Steps: []resource.TestStep{
			{Config: configuration, Check: unlocked},
			{Config: configuration, PreConfig: func() {
				exec(sqlclient.Stmt("ALTER USER").Ident(name).Kw("NOLOGIN"))
			}, Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(address, "user_lockouts.#", "1"),
				resource.TestCheckResourceAttr(address, "user_lockouts.0.user_name", name),
				resource.TestCheckResourceAttr(address, "user_lockouts.0.is_locked", "true"),
				resource.TestCheckResourceAttr(address, "user_lockouts.0.lock_reason", "manual"),
				resource.TestCheckResourceAttr(address, "user_lockouts.0.failed_login_attempts", "0"),
				resource.TestCheckResourceAttrSet(address, "user_lockouts.0.locked_at"),
				resource.TestCheckResourceAttrSet(address, "user_lockouts.0.locked_by"),
				resource.TestCheckOutput("fixture_is_listed", "true"),
			)},
			{Config: configuration, PreConfig: func() {
				exec(sqlclient.Stmt("ALTER USER").Ident(name).Kw("LOGIN PASSWORD").Lit(password))
			}, Check: unlocked},
		},
	})
}
