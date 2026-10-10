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
	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/redshiftdata"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/require"
)

// TestAccDiscoveryListings verifies the catalog listings and constraint comments against live system views, which
// the offline fakes can only approximate: owner joins, materialized view detection, nullability spelling, and
// pg_get_constraintdef output.
func TestAccDiscoveryListings(t *testing.T) {
	region, profile, workgroup, database := testAccWorkgroup(t)
	name := "acc_discovery_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	configuration := func(text string, enabled bool) string {
		base := fmt.Sprintf(`
provider "redshift" {
  region = %q
  profile = %q
  workgroup_name = %q
  database = %q
}
resource "redshift_database" "local" { name = %q }
`, region, profile, workgroup, database, name)
		if !enabled {
			return base
		}
		return base + fmt.Sprintf(`
data "redshift_databases" "acc" {
  name_like = redshift_database.local.name
}
data "redshift_schemas" "acc" {
  database = redshift_database.local.name
  schema_type = "local"
}
data "redshift_tables" "views" {
  database = redshift_database.local.name
  schema = "public"
  table_type = "VIEW"
}
data "redshift_tables" "materialized" {
  database = redshift_database.local.name
  schema = "public"
  table_type = "MATERIALIZED VIEW"
}
data "redshift_columns" "orders" {
  database = redshift_database.local.name
  schema = "public"
  table = "orders"
}
data "redshift_constraints" "lines" {
  database = redshift_database.local.name
  schema = "public"
  table = "order_lines"
}
data "redshift_constraints" "orders" {
  database = redshift_database.local.name
  schema = "public"
  table = "orders"
  constraint_type = "UNIQUE"
}
resource "redshift_comment" "key" {
  database_name = redshift_database.local.name
  object_type = "CONSTRAINT"
  schema_name = "public"
  object_name = "orders"
  constraint_name = "orders_pkey"
  text = %q
}
data "redshift_comment" "key" {
  database_name = redshift_comment.key.database_name
  object_type = redshift_comment.key.object_type
  schema_name = redshift_comment.key.schema_name
  object_name = redshift_comment.key.object_name
  constraint_name = redshift_comment.key.constraint_name
}
`, text)
	}
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region), config.WithSharedConfigProfile(profile))
	require.NoError(t, err)
	client := &dataapi.Client{API: redshiftdata.NewFromConfig(cfg), Workgroup: workgroup, Timeout: 5 * time.Minute, Poll: time.Second}
	admin, target := sqlclient.Connection{Database: database}, sqlclient.Connection{Database: name}
	// A failed test may leave the database behind; its fixtures live in public and go with it.
	t.Cleanup(func() {
		rows, err := client.Query(ctx, admin, "SELECT database_name FROM svv_redshift_databases WHERE database_name = :name", map[string]string{"name": name})
		require.NoError(t, err)
		if len(rows) != 0 {
			_, err = client.Query(ctx, admin, sqlclient.Stmt("DROP DATABASE").Ident(name).String(), nil)
			require.NoError(t, err)
		}
	})
	fixtures := []string{
		`CREATE TABLE public.orders (id INTEGER NOT NULL, "Region" VARCHAR(16), label VARCHAR(64) DEFAULT 'none', PRIMARY KEY (id), UNIQUE (label, "Region"))`,
		`CREATE TABLE public.order_lines (order_id INTEGER, line INTEGER, CONSTRAINT order_lines_order_fkey FOREIGN KEY (order_id) REFERENCES public.orders (id))`,
		`CREATE VIEW public.order_labels AS SELECT id, label FROM public.orders`,
		`CREATE MATERIALIZED VIEW public.order_totals AS SELECT COUNT(*) AS total FROM public.orders`,
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(New("test")())},
		Steps: []resource.TestStep{
			{Config: configuration("", false)},
			{
				Config: configuration("O'Reilly key", true),
				PreConfig: func() {
					for _, statement := range fixtures {
						_, err := client.Query(ctx, target, statement, nil)
						require.NoError(t, err, statement)
					}
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.redshift_databases.acc", "items.#", "1"),
					resource.TestCheckResourceAttr("data.redshift_databases.acc", "items.0.database_type", "local"),
					resource.TestCheckResourceAttrSet("data.redshift_databases.acc", "items.0.owner"),
					resource.TestCheckTypeSetElemNestedAttrs("data.redshift_schemas.acc", "items.*", map[string]string{"name": "public", "schema_type": "local"}),
					resource.TestCheckResourceAttr("data.redshift_tables.views", "items.#", "1"),
					resource.TestCheckResourceAttr("data.redshift_tables.views", "items.0.name", "order_labels"),
					resource.TestCheckResourceAttr("data.redshift_tables.materialized", "items.#", "1"),
					resource.TestCheckResourceAttr("data.redshift_tables.materialized", "items.0.name", "order_totals"),
					resource.TestCheckResourceAttrSet("data.redshift_tables.materialized", "items.0.owner"),
					resource.TestCheckResourceAttr("data.redshift_columns.orders", "items.#", "3"),
					resource.TestCheckResourceAttr("data.redshift_columns.orders", "items.0.name", "id"),
					resource.TestCheckResourceAttr("data.redshift_columns.orders", "items.0.nullable", "false"),
					resource.TestCheckResourceAttr("data.redshift_columns.orders", "items.1.name", "Region"),
					resource.TestCheckResourceAttr("data.redshift_columns.orders", "items.1.character_maximum_length", "16"),
					resource.TestCheckResourceAttr("data.redshift_columns.orders", "items.2.nullable", "true"),
					resource.TestCheckResourceAttrSet("data.redshift_columns.orders", "items.2.default"),
					resource.TestCheckResourceAttr("data.redshift_constraints.lines", "items.#", "1"),
					resource.TestCheckResourceAttr("data.redshift_constraints.lines", "items.0.name", "order_lines_order_fkey"),
					resource.TestCheckResourceAttr("data.redshift_constraints.lines", "items.0.constraint_type", "FOREIGN KEY"),
					resource.TestCheckResourceAttr("data.redshift_constraints.lines", "items.0.columns.0", "order_id"),
					resource.TestCheckResourceAttr("data.redshift_constraints.lines", "items.0.referenced_table", "orders"),
					resource.TestCheckResourceAttr("data.redshift_constraints.lines", "items.0.referenced_columns.0", "id"),
					resource.TestCheckResourceAttr("data.redshift_constraints.orders", "items.#", "1"),
					resource.TestCheckResourceAttr("data.redshift_constraints.orders", "items.0.columns.#", "2"),
					resource.TestCheckResourceAttr("data.redshift_constraints.orders", "items.0.columns.1", "Region"),
					resource.TestCheckResourceAttr("data.redshift_comment.key", "text", "O'Reilly key"),
				),
			},
			{Config: configuration("O'Reilly key", true), PlanOnly: true},
			{ResourceName: "redshift_comment.key", ImportState: true, ImportStateVerify: true},
			{
				Config: configuration("O'Reilly key", true), PlanOnly: true, ExpectNonEmptyPlan: true,
				PreConfig: func() {
					_, err := client.Query(ctx, target, `COMMENT ON CONSTRAINT orders_pkey ON public.orders IS 'drift'`, nil)
					require.NoError(t, err)
				},
			},
			{Config: configuration("updated", true), Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("redshift_comment.key", "text", "updated"),
				resource.TestCheckResourceAttr("data.redshift_comment.key", "text", "updated"),
			)},
			{Config: configuration("", false)},
		},
	})
}
