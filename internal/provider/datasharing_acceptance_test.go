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

// TestAccDatasharing checks the datashare catalog attributes, the listing, ALTER/SHARE permissions with import and
// drift repair, and, when REDSHIFT_ACC_LAKE_FORMATION_ACCOUNT names a Lake Formation account, a VIA DATA CATALOG
// grant. Everything lives in an isolated database that the cleanup removes without cascading.
func TestAccDatasharing(t *testing.T) {
	region, profile, workgroup, database := testAccWorkgroup(t)
	name := "acc_sharing_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region), config.WithSharedConfigProfile(profile))
	require.NoError(t, err)
	client := &dataapi.Client{API: redshiftdata.NewFromConfig(cfg), Workgroup: workgroup, Timeout: 5 * time.Minute, Poll: time.Second}
	execute := func(target, sql string) error {
		_, err := client.Query(ctx, sqlclient.Connection{Database: target}, sql, nil)
		return err
	}
	t.Cleanup(func() {
		// Terraform destroys the objects; these statements only remove leftovers of a failed run.
		for _, cleanup := range []struct{ database, sql string }{
			{name, "DROP DATASHARE " + sqlclient.Identifier(name)},
			{database, "DROP ROLE " + sqlclient.Identifier(name)},
			{database, "DROP DATABASE " + sqlclient.Identifier(name)},
		} {
			if err := execute(cleanup.database, cleanup.sql); err != nil {
				t.Logf("fixture cleanup %q: %v", cleanup.sql, err)
			}
		}
	})
	lakeFormation := ""
	if account := os.Getenv("REDSHIFT_ACC_LAKE_FORMATION_ACCOUNT"); account != "" {
		lakeFormation = fmt.Sprintf(`
resource "redshift_datashare_grant" "lake_formation" {
  database = redshift_datashare.local.database
  datashare = redshift_datashare.local.name
  account_id = %q
  via_data_catalog = true
}`, account)
	}
	configuration := fmt.Sprintf(`
provider "redshift" {
  region = %q
  profile = %q
  workgroup_name = %q
  database = %q
}
resource "redshift_database" "local" { name = %q }
resource "redshift_datashare" "local" {
  database = redshift_database.local.name
  name = %q
}
resource "redshift_role" "operators" { name = %q }
resource "redshift_datashare_privilege" "operators" {
  database_name = redshift_datashare.local.database
  datashare_name = redshift_datashare.local.name
  grantee_type = "ROLE"
  grantee = redshift_role.operators.name
  privileges = ["ALTER", "SHARE"]
}
data "redshift_datashare_privilege" "operators" {
  database_name = redshift_datashare_privilege.operators.database_name
  datashare_name = redshift_datashare_privilege.operators.datashare_name
  grantee_type = "ROLE"
  grantee = redshift_datashare_privilege.operators.grantee
}
data "redshift_datashares" "named" {
  share_type = "OUTBOUND"
  name = redshift_datashare.local.name
}%s`, region, profile, workgroup, database, name, name, name, lakeFormation)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(New("test")())},
		Steps: []resource.TestStep{
			{Config: configuration, Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet("redshift_datashare.local", "owner"),
				resource.TestCheckResourceAttrSet("redshift_datashare.local", "share_id"),
				resource.TestCheckResourceAttrSet("redshift_datashare.local", "producer_namespace"),
				resource.TestCheckResourceAttrSet("redshift_datashare.local", "created_at"),
				resource.TestCheckResourceAttr("data.redshift_datashare_privilege.operators", "privileges.#", "2"),
				resource.TestCheckResourceAttr("data.redshift_datashares.named", "items.#", "1"),
				resource.TestCheckResourceAttrPair("data.redshift_datashares.named", "items.0.owner", "redshift_datashare.local", "owner"),
				resource.TestCheckResourceAttrPair("data.redshift_datashares.named", "items.0.share_id", "redshift_datashare.local", "share_id"),
			)},
			{ResourceName: "redshift_datashare.local", ImportState: true, ImportStateVerify: true},
			{ResourceName: "redshift_datashare_privilege.operators", ImportState: true, ImportStateVerify: true},
			{Config: configuration, PlanOnly: true},
			{Config: configuration, PreConfig: func() {
				require.NoError(t, execute(name, "REVOKE SHARE ON DATASHARE "+sqlclient.Identifier(name)+" FROM ROLE "+sqlclient.Identifier(name)))
			}, Check: resource.TestCheckResourceAttr("redshift_datashare_privilege.operators", "privileges.#", "2")},
		},
	})
}
