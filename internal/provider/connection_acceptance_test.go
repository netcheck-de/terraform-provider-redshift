package provider

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/redshiftserverless"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/netcheck-de/terraform-provider-redshift/internal/redshiftconn"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAccDirectIAMQueries checks real TLS, IAM authentication, parameter binding, and textual results without mutations.
func TestAccDirectIAMQueries(t *testing.T) {
	region, profile, workgroup, database := testAccWorkgroup(t, "REDSHIFT_ACC_DIRECT")
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region), config.WithSharedConfigProfile(profile))
	require.NoError(t, err)
	client := &redshiftconn.Client{IAM: &redshiftconn.IAM{Workgroup: workgroup, Serverless: redshiftserverless.NewFromConfig(cfg)}, Timeout: 30 * time.Second}
	rows, err := client.Query(ctx, sqlclient.Connection{Database: database}, "SELECT current_user AS username, CAST(:value AS VARCHAR) AS value, true AS enabled, CAST(NULL AS VARCHAR) AS empty", map[string]string{"value": "O'Reilly"})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.NotEmpty(t, rows[0]["username"])
	assert.Equal(t, "O'Reilly", rows[0]["value"])
	assert.Equal(t, "true", rows[0]["enabled"])
	assert.Empty(t, rows[0]["empty"])
}

// TestAccTransportSwitchLifecycle verifies stable Serverless identity while switching Data API to direct IAM and back.
func TestAccTransportSwitchLifecycle(t *testing.T) {
	region, profile, workgroup, database := testAccWorkgroup(t, "REDSHIFT_ACC_DIRECT")
	name := "acc_transport_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	configuration := func(direct bool, privileges string) string {
		selector := fmt.Sprintf("workgroup_name = %q", workgroup)
		if direct {
			selector = fmt.Sprintf("direct_connection {\n iam { workgroup_name = %q }\n}", workgroup)
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
  privileges = %s
}
data "redshift_grant" "schema" {
  database_name = redshift_grant.schema.database_name
  schema_name = redshift_grant.schema.schema_name
  role = redshift_grant.schema.role
  scope = redshift_grant.schema.scope
}`, region, profile, database, selector, name, name+"_reader", privileges)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(New("test")())},
		Steps: []resource.TestStep{
			{Config: configuration(false, `["USAGE"]`)},
			{Config: configuration(true, `["USAGE"]`), PlanOnly: true},
			{Config: configuration(true, `["USAGE"]`)},
			{ResourceName: "redshift_database.local", ImportState: true, ImportStateVerify: true},
			{ResourceName: "redshift_schema.local", ImportState: true, ImportStateVerify: true},
			{ResourceName: "redshift_role.reader", ImportState: true, ImportStateVerify: true},
			{ResourceName: "redshift_grant.schema", ImportState: true, ImportStateVerify: true},
			{Config: configuration(true, `["USAGE", "CREATE"]`), Check: resource.TestCheckResourceAttr("data.redshift_grant.schema", "privileges.#", "2")},
			{Config: configuration(false, `["USAGE", "CREATE"]`), PlanOnly: true},
			{Config: configuration(true, `["USAGE", "CREATE"]`)},
		},
	})
}
