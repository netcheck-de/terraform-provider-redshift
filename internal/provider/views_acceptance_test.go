package provider

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/redshiftdata"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/redshiftdata"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/require"
)

// TestAccViewLifecycle verifies ordinary, late-binding, and materialized views: creation, lookups, import, in-place
// query, storage, and refresh changes, definition drift detection and repair, and restrictive deletion.
func TestAccViewLifecycle(t *testing.T) {
	region, profile, workgroup, database := testAccWorkgroup(t)
	name := "acc_views_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	configuration := func(label string, autoRefresh, enabled bool) string {
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
`, region, profile, workgroup, database, name)
		if !enabled {
			return base
		}
		// Turning auto refresh off also moves the materialized view to key distribution and a longer sort key, which
		// ALTER MATERIALIZED VIEW applies in place.
		storage := map[bool]string{true: `distribution {
    style = "ALL"
  }
  sort_key {
    columns = ["label"]
  }`, false: `distribution {
    key = "label"
  }
  sort_key {
    columns = ["label", "events"]
  }`}[autoRefresh]
		return base + fmt.Sprintf(`
resource "redshift_view" "ordinary" {
  database = redshift_schema.local.database
  schema = redshift_schema.local.name
  name = "labels"
  query = "SELECT id, %[1]s AS label FROM serving.events"
}
resource "redshift_view" "late" {
  database = redshift_schema.local.database
  schema = redshift_schema.local.name
  name = "labels_late"
  late_binding = true
  query = "SELECT id, label FROM serving.events"
}
resource "redshift_materialized_view" "counts" {
  database = redshift_schema.local.database
  schema = redshift_schema.local.name
  name = "label_counts"
  %[3]s
  auto_refresh = %[2]t
  query = "SELECT label, COUNT(*) AS events FROM serving.events GROUP BY label"
}
data "redshift_view" "late" {
  database = redshift_view.late.database
  schema = redshift_view.late.schema
  name = redshift_view.late.name
}
data "redshift_materialized_view" "counts" {
  database = redshift_materialized_view.counts.database
  schema = redshift_materialized_view.counts.schema
  name = redshift_materialized_view.counts.name
}
`, label, autoRefresh, storage)
	}
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region), config.WithSharedConfigProfile(profile))
	require.NoError(t, err)
	client := &dataapi.Client{API: redshiftdata.NewFromConfig(cfg), Workgroup: workgroup, Timeout: 5 * time.Minute, Poll: time.Second}
	admin, target := sqlclient.Connection{Database: database}, sqlclient.Connection{Database: name}
	// A failed test may leave the fixture table and views, which block restrictive schema deletion.
	t.Cleanup(func() {
		rows, err := client.Query(ctx, admin, "SELECT database_name FROM svv_redshift_databases WHERE database_name = :name", map[string]string{"name": name})
		require.NoError(t, err)
		if len(rows) != 0 {
			_, err = client.Query(ctx, admin, "DROP DATABASE "+sqlclient.Identifier(name), nil)
			require.NoError(t, err)
		}
	})
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(New("test")())},
		Steps: []resource.TestStep{
			{Config: configuration("label", true, false)},
			{
				Config: configuration("label", true, true),
				PreConfig: func() {
					_, err := client.Query(ctx, target, `CREATE TABLE serving.events (id INTEGER, label VARCHAR(64))`, nil)
					require.NoError(t, err)
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("redshift_view.ordinary", "late_binding", "false"),
					resource.TestCheckResourceAttrSet("redshift_view.ordinary", "owner"),
					resource.TestCheckResourceAttrSet("redshift_view.ordinary", "definition_fingerprint"),
					resource.TestCheckResourceAttr("data.redshift_view.late", "late_binding", "true"),
					resource.TestCheckResourceAttr("data.redshift_materialized_view.counts", "auto_refresh", "true"),
					resource.TestCheckResourceAttrPair("data.redshift_materialized_view.counts", "owner", "redshift_materialized_view.counts", "owner"),
				),
			},
			{Config: configuration("label", true, true), PlanOnly: true},
			{ResourceName: "redshift_view.ordinary", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"query"}},
			{ResourceName: "redshift_view.late", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"query"}},
			{ResourceName: "redshift_materialized_view.counts", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"query", "distribution", "sort_key"}},
			// The new query keeps the column names and types, so CREATE OR REPLACE VIEW succeeds in place.
			{
				Config: configuration("UPPER(label)", false, true),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("redshift_materialized_view.counts", plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr("redshift_materialized_view.counts", "auto_refresh", "false"),
			},
			{Config: configuration("UPPER(label)", false, true), PlanOnly: true},
			{
				Config: configuration("UPPER(label)", false, true), PlanOnly: true, ExpectNonEmptyPlan: true,
				PreConfig: func() {
					_, err := client.Query(ctx, target, `CREATE OR REPLACE VIEW serving.labels AS SELECT id, LOWER(label) AS label FROM serving.events`, nil)
					require.NoError(t, err)
				},
			},
			{Config: configuration("UPPER(label)", false, true)},
			{Config: configuration("UPPER(label)", false, true), PlanOnly: true},
			{
				Config: configuration("label", false, false),
				Check: func(*terraform.State) error {
					_, err := client.Query(ctx, target, `DROP TABLE serving.events`, nil)
					return err
				},
			},
		},
	})
}
