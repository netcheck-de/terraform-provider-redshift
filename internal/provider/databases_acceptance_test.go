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
	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/redshiftdata"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/require"
)

// TestAccDatabaseOptionsLifecycle creates a local database and schema with every option, changes the in-place
// options, imports both, and checks that no drift remains. REDSHIFT_ACC_SHARED_DATABASE optionally names an existing
// consumer database for a cross-database external schema.
func TestAccDatabaseOptionsLifecycle(t *testing.T) {
	region, profile, workgroup, database := testAccWorkgroup(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	name, owner, successor := "acc_db_"+suffix, "acc_owner_"+suffix, "acc_successor_"+suffix
	shared := os.Getenv("REDSHIFT_ACC_SHARED_DATABASE")
	// Owners switch between two users created here, because the acceptance identity's own name is not configured.
	configuration := func(limit int, isolation string, quota int, schemaOwner string) string {
		external := ""
		if shared != "" {
			external = fmt.Sprintf(`
resource "redshift_external_schema" "shared" {
  database        = redshift_database.local.name
  name            = "shared_sales"
  source_type     = "REDSHIFT"
  source_database = %q
  source_schema   = "public"
  owner           = %s
}`, shared, schemaOwner)
		}
		return fmt.Sprintf(`
provider "redshift" {
  region         = %q
  profile        = %q
  workgroup_name = %q
  database       = %q
}
resource "redshift_user" "owner" {
  name                = %q
  password_wo         = "AccOwner1234"
  password_wo_version = 1
}
resource "redshift_user" "successor" {
  name                = %q
  password_wo         = "AccSuccessor1234"
  password_wo_version = 1
}
resource "redshift_database" "local" {
  name             = %q
  owner            = redshift_user.owner.name
  connection_limit = %d
  collation        = "CASE_INSENSITIVE"
  isolation_level  = %q
}
resource "redshift_schema" "local" {
  database = redshift_database.local.name
  name     = "serving"
  owner    = %s
  quota    = %d
}
data "redshift_database" "local" {
  name = redshift_database.local.name
}
data "redshift_schema" "local" {
  database = redshift_schema.local.database
  name     = redshift_schema.local.name
}%s
`, region, profile, workgroup, database, owner, successor, name, limit, isolation, schemaOwner, quota, external)
	}
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region), config.WithSharedConfigProfile(profile))
	require.NoError(t, err)
	client := &dataapi.Client{API: redshiftdata.NewFromConfig(cfg), Workgroup: workgroup, Timeout: 5 * time.Minute, Poll: time.Second}
	admin := sqlclient.Connection{Database: database}
	// A failed test may leave the database behind; the schema inside it is dropped with it.
	t.Cleanup(func() {
		rows, err := client.Query(ctx, admin, "SELECT database_name FROM svv_redshift_databases WHERE database_name = :name", map[string]string{"name": name})
		require.NoError(t, err)
		if len(rows) != 0 {
			_, err = client.Query(ctx, admin, sqlclient.Stmt("DROP DATABASE").Ident(name).String(), nil)
			require.NoError(t, err)
		}
	})
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(New("test")())},
		Steps: []resource.TestStep{
			{Config: configuration(10, "SNAPSHOT", 100, "redshift_user.owner.name"), Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttr("data.redshift_database.local", "owner", owner),
				resource.TestCheckResourceAttr("data.redshift_database.local", "connection_limit", "10"),
				resource.TestCheckResourceAttr("data.redshift_database.local", "collation", "CASE_INSENSITIVE"),
				resource.TestCheckResourceAttr("data.redshift_database.local", "isolation_level", "SNAPSHOT"),
				resource.TestCheckResourceAttr("data.redshift_schema.local", "owner", owner),
				resource.TestCheckResourceAttr("data.redshift_schema.local", "quota", "100"),
			)},
			{Config: configuration(10, "SNAPSHOT", 100, "redshift_user.owner.name"), PlanOnly: true},
			{Config: configuration(-1, "SERIALIZABLE", -1, "redshift_user.successor.name"), Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttr("redshift_database.local", "connection_limit", "-1"),
				resource.TestCheckResourceAttr("redshift_database.local", "isolation_level", "SERIALIZABLE"),
				resource.TestCheckResourceAttr("redshift_schema.local", "quota", "-1"),
				resource.TestCheckResourceAttr("redshift_schema.local", "owner", successor),
			)},
			{ResourceName: "redshift_database.local", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"with_permissions"}},
			{ResourceName: "redshift_schema.local", ImportState: true, ImportStateVerify: true},
			{Config: configuration(-1, "SERIALIZABLE", -1, "redshift_user.successor.name"), PlanOnly: true},
		},
	})
}
