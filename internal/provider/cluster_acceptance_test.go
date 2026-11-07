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

	region, profile := os.Getenv("REDSHIFT_ACC_REGION"), os.Getenv("REDSHIFT_ACC_PROFILE")
	cluster, database, user := os.Getenv("REDSHIFT_ACC_CLUSTER"), os.Getenv("REDSHIFT_ACC_DATABASE"), os.Getenv("REDSHIFT_ACC_DB_USER")

	require.NotEmpty(t, region)
	require.NotEmpty(t, profile)
	require.NotEmpty(t, database)
	require.NotEmpty(t, user)

	name := "acc_cluster_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region), config.WithSharedConfigProfile(profile))
	require.NoError(t, err)

	client := &dataapi.Client{API: redshiftdata.NewFromConfig(cfg), Cluster: cluster, DBUser: user, Timeout: 5 * time.Minute, Poll: time.Second}
	configuration := func(mode string) string {
		selector := fmt.Sprintf("cluster_identifier = %q\n db_user = %q", cluster, user)
		if mode == "secret" {
			selector = fmt.Sprintf("cluster_identifier = %q\n secret_arn = %q", cluster, os.Getenv("REDSHIFT_ACC_SECRET_ARN"))
		}

		if mode == "direct" {
			selector = fmt.Sprintf("direct_connection {\n %s iam {\n cluster_identifier = %q\n db_user = %q\n }\n}", testAccSSLModeHCL(), cluster, user)
		}

		return fmt.Sprintf(`
provider "redshift" {
  region = %q
  profile = %q
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
}`, region, profile, database, selector, name, name+"_reader")
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
	if os.Getenv("REDSHIFT_ACC_SECRET_ARN") != "" {
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

// TestAccClusterUserLockoutsRollout checks that SHOW USER LOCKOUT output from a provisioned cluster parses, and skips
// while the feature has not rolled out there.
func TestAccClusterUserLockoutsRollout(t *testing.T) {
	testAccPreCheck(t, "REDSHIFT_ACC_CLUSTER")

	region, profile := os.Getenv("REDSHIFT_ACC_REGION"), os.Getenv("REDSHIFT_ACC_PROFILE")
	require.NotEmpty(t, region)
	require.NotEmpty(t, profile)

	database := os.Getenv("REDSHIFT_ACC_DATABASE")
	require.NotEmpty(t, database)

	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region), config.WithSharedConfigProfile(profile))
	require.NoError(t, err)

	client := &dataapi.Client{API: redshiftdata.NewFromConfig(cfg), Cluster: os.Getenv("REDSHIFT_ACC_CLUSTER"), DBUser: os.Getenv("REDSHIFT_ACC_DB_USER"), SecretARN: os.Getenv("REDSHIFT_ACC_SECRET_ARN"), Timeout: time.Minute, Poll: time.Second}

	rows, err := client.Query(ctx, sqlclient.Connection{Database: database}, readUserLockoutsStatement(), nil)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "account lockout security is not supported") {
		t.Skipf("SHOW USER LOCKOUT has not rolled out on this provisioned warehouse: %v", err)
	}

	require.NoError(t, err)
	_, err = userLockoutsItems("", rows)
	require.NoError(t, err)
}
