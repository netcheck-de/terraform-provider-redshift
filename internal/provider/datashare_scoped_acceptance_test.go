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
	"github.com/stretchr/testify/require"
)

// TestAccDatashareScopedGrants checks current/future objects, replacement, imports, drift, and cleanup in isolated state.
func TestAccDatashareScopedGrants(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("set TF_ACC=1 with a disposable dev warehouse")
	}
	region, profile := os.Getenv("REDSHIFT_ACC_REGION"), os.Getenv("REDSHIFT_ACC_PROFILE")
	workgroup, database := os.Getenv("REDSHIFT_ACC_WORKGROUP"), os.Getenv("REDSHIFT_ACC_DATABASE")
	require.NotEmpty(t, region)
	require.NotEmpty(t, profile)
	require.NotEmpty(t, workgroup)
	require.NotEmpty(t, database)
	name := "acc_scoped_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region), config.WithSharedConfigProfile(profile))
	require.NoError(t, err)
	client := &dataapi.Client{API: redshiftdata.NewFromConfig(cfg), Workgroup: workgroup, Timeout: 5 * time.Minute, Poll: time.Second}
	connection := sqlclient.Connection{Database: name}
	t.Cleanup(func() {
		rows, err := client.Query(ctx, sqlclient.Connection{Database: database}, "SELECT database_name FROM svv_redshift_databases WHERE database_name = :name", map[string]string{"name": name})
		require.NoError(t, err)
		if len(rows) == 0 {
			return
		}
		_, err = client.Query(ctx, connection, "DROP SCHEMA IF EXISTS serving CASCADE", nil)
		require.NoError(t, err)
		_, err = client.Query(ctx, connection, "DROP DATASHARE "+sqlclient.Identifier(name), nil)
		// The acceptance harness may already have removed the datashare.
		if err != nil {
			t.Logf("fixture datashare cleanup: %v", err)
		}
		_, err = client.Query(ctx, sqlclient.Connection{Database: database}, "DROP DATABASE "+sqlclient.Identifier(name), nil)
		require.NoError(t, err)
	})
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
resource "redshift_datashare" "local" {
  database = redshift_database.local.name
  name = %q
}`, region, profile, workgroup, database, name, name)
	grants := `
resource "redshift_grant" "schema" {
  database_name = redshift_schema.local.database
  schema_name = redshift_schema.local.name
  datashare = redshift_datashare.local.name
  scope = "SCHEMA"
  privileges = ["USAGE"]
}
resource "redshift_grant" "tables" {
  database_name = redshift_schema.local.database
  schema_name = redshift_schema.local.name
  datashare = redshift_datashare.local.name
  scope = "TABLES"
  privileges = ["SELECT"]
  depends_on = [redshift_grant.schema]
}
data "redshift_grant" "tables" {
  database_name = redshift_grant.tables.database_name
  schema_name = redshift_grant.tables.schema_name
  datashare = redshift_grant.tables.datashare
  scope = "TABLES"
}`
	execute := func(sql string) {
		_, err := client.Query(ctx, connection, sql, nil)
		require.NoError(t, err)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(New("test")())},
		Steps: []resource.TestStep{
			{Config: base, Check: func(*terraform.State) error {
				execute("CREATE TABLE serving.existing (id INTEGER)")
				return nil
			}},
			{Config: base + grants, Check: func(*terraform.State) error {
				execute("CREATE TABLE serving.future (id INTEGER)")
				execute("CREATE VIEW serving.replaced AS SELECT id FROM serving.existing WITH NO SCHEMA BINDING")
				execute("DROP VIEW serving.replaced")
				execute("CREATE VIEW serving.replaced AS SELECT id FROM serving.future WITH NO SCHEMA BINDING")
				rows, err := client.Query(ctx, connection, "SELECT object_name, object_type FROM svv_datashare_objects WHERE share_name = :share AND object_name <> 'serving'", map[string]string{"share": name})
				require.NoError(t, err)
				require.Len(t, rows, 3, "%v", rows)
				return nil
			}},
			{ResourceName: "redshift_grant.schema", ImportState: true, ImportStateVerify: true},
			{ResourceName: "redshift_grant.tables", ImportState: true, ImportStateVerify: true},
			{Config: base + grants, PlanOnly: true},
			{Config: base + grants, PreConfig: func() {
				execute("REVOKE SELECT FOR TABLES IN SCHEMA serving FROM DATASHARE " + sqlclient.Identifier(name))
			}, Check: resource.TestCheckResourceAttr("data.redshift_grant.tables", "privileges.#", "1")},
			{Config: base + grants, PreConfig: func() {
				execute("DROP VIEW serving.replaced")
				execute("DROP TABLE serving.future")
				execute("DROP TABLE serving.existing")
			}},
		},
	})
}
