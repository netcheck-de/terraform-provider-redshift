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
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/redshiftdata"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/require"
)

// TestAccDatashareMembership exercises both exclusive kinds against real catalog rows, including indistinguishable
// overload names. All SQL objects belong to this test's unique database; the shared warehouse is never removed.
func TestAccDatashareMembership(t *testing.T) {
	region, profile, workgroup, database := testAccWorkgroup(t)
	name := "acc_bulk_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region), config.WithSharedConfigProfile(profile))
	require.NoError(t, err)
	client := &dataapi.Client{API: redshiftdata.NewFromConfig(cfg), Workgroup: workgroup, Timeout: 5 * time.Minute, Poll: time.Second}
	admin := sqlclient.Connection{Database: database}
	producer := sqlclient.Connection{Database: name}
	databases := sqlclient.Select("database_name").From("svv_redshift_databases").Where("database_name = :name", sqlclient.Bind("name", name))
	shares := sqlclient.Select("share_name").From("svv_datashares").Where("share_type = 'OUTBOUND'").Where("share_name = :name", sqlclient.Bind("name", name))
	query := func(connection sqlclient.Connection, statement sqlclient.Query) ([]sqlclient.Row, error) {
		sql, parameters, err := statement.Build()
		if err != nil {
			return nil, err
		}
		return client.Query(ctx, connection, sql, parameters)
	}
	for _, statement := range []sqlclient.Query{databases, shares} {
		rows, err := query(admin, statement)
		require.NoError(t, err)
		require.Empty(t, rows, "acceptance fixture name must not refer to pre-existing SQL objects")
	}
	t.Cleanup(func() {
		rows, err := query(admin, shares)
		require.NoError(t, err)
		if len(rows) != 0 {
			_, err = client.Query(ctx, producer, sqlclient.Stmt("DROP DATASHARE").Ident(name).String(), nil)
			require.NoError(t, err)
		}
		rows, err = query(admin, databases)
		require.NoError(t, err)
		if len(rows) == 0 {
			return
		}
		// CASCADE is confined to the unique schema in the database whose absence was checked before this test.
		_, err = client.Query(ctx, producer, sqlclient.Stmt("DROP SCHEMA IF EXISTS").Ident(name).Kw("CASCADE").String(), nil)
		require.NoError(t, err)
		_, err = client.Query(ctx, admin, sqlclient.Stmt("DROP DATABASE").Ident(name).String(), nil)
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
  name = %q
}
resource "redshift_table" "events" {
  database = redshift_schema.local.database
  schema = redshift_schema.local.name
  name = "events"
  column {
    name = "id"
    type = "INTEGER"
  }
}
resource "redshift_table" "audit" {
  database = redshift_schema.local.database
  schema = redshift_schema.local.name
  name = "audit"
  column {
    name = "id"
    type = "INTEGER"
  }
}
resource "redshift_function" "integer" {
  database = redshift_schema.local.database
  schema = redshift_schema.local.name
  name = "f_identity"
  arguments = ["INTEGER"]
  return_type = "INTEGER"
  volatility = "IMMUTABLE"
  body = "SELECT $1"
}
resource "redshift_function" "varchar" {
  database = redshift_schema.local.database
  schema = redshift_schema.local.name
  name = "f_identity"
  arguments = ["VARCHAR"]
  return_type = "VARCHAR"
  volatility = "IMMUTABLE"
  body = "SELECT $1"
}
resource "redshift_datashare" "local" {
  database = redshift_database.local.name
  name = %q
}
resource "redshift_datashare_schema" "local" {
  database = redshift_datashare.local.database
  datashare = redshift_datashare.local.name
  schema = redshift_schema.local.name
  include_new = false
}`, region, profile, workgroup, database, name, name, name)
	memberships := `
resource "redshift_datashare_membership" "tables" {
  database = redshift_datashare_schema.local.database
  datashare = redshift_datashare_schema.local.datashare
  schema = redshift_datashare_schema.local.schema
  kind = "TABLES"
  all = true
  depends_on = [redshift_table.events, redshift_table.audit]
}
resource "redshift_datashare_membership" "functions" {
  database = redshift_datashare_schema.local.database
  datashare = redshift_datashare_schema.local.datashare
  schema = redshift_datashare_schema.local.schema
  kind = "FUNCTIONS"
  all = true
  depends_on = [redshift_function.integer, redshift_function.varchar]
}
data "redshift_datashare_membership" "tables" {
  database = redshift_datashare_membership.tables.database
  datashare = redshift_datashare_membership.tables.datashare
  schema = redshift_datashare_membership.tables.schema
  kind = redshift_datashare_membership.tables.kind
}
data "redshift_datashare_membership" "functions" {
  database = redshift_datashare_membership.functions.database
  datashare = redshift_datashare_membership.functions.datashare
  schema = redshift_datashare_membership.functions.schema
  kind = redshift_datashare_membership.functions.kind
}`
	coverage := func(value string) resource.TestCheckFunc {
		return resource.ComposeTestCheckFunc(
			resource.TestCheckResourceAttr("redshift_datashare_membership.tables", "all", value),
			resource.TestCheckResourceAttr("redshift_datashare_membership.functions", "all", value),
			resource.TestCheckResourceAttr("data.redshift_datashare_membership.tables", "exists", "true"),
			resource.TestCheckResourceAttr("data.redshift_datashare_membership.functions", "exists", "true"),
			resource.TestCheckResourceAttr("data.redshift_datashare_membership.tables", "all", value),
			resource.TestCheckResourceAttr("data.redshift_datashare_membership.functions", "all", value),
		)
	}
	memberCounts := func(tables, functions int) resource.TestCheckFunc {
		return func(*terraform.State) error {
			rows, err := query(producer, sqlclient.Select("object_name", "object_type").From("svv_datashare_objects").Where("share_type = 'OUTBOUND'").Where("share_name = :share", sqlclient.Bind("share", name)))
			if err != nil {
				return err
			}
			counts := map[string]int{}
			for _, row := range rows {
				counts[row["object_type"]]++
				if row["object_type"] == "function" && row["object_name"] != name+".f_identity" {
					return fmt.Errorf("expected name-only overload catalog rows, got %q", row["object_name"])
				}
			}
			if counts["table"] != tables || counts["function"] != functions || counts["schema"] != 1 {
				return fmt.Errorf("expected %d tables, %d functions, and one schema; got %v", tables, functions, counts)
			}
			return nil
		}
	}
	configuration := base + memberships
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(New("test")())},
		CheckDestroy: func(*terraform.State) error {
			for _, statement := range []sqlclient.Query{databases, shares} {
				rows, err := query(admin, statement)
				if err != nil {
					return err
				}
				if len(rows) != 0 {
					return fmt.Errorf("owned acceptance SQL fixture remains: %v", rows)
				}
			}
			return nil
		},
		Steps: []resource.TestStep{
			{Config: configuration, Check: resource.ComposeTestCheckFunc(coverage("true"), memberCounts(2, 2))},
			{ResourceName: "redshift_datashare_membership.tables", ImportState: true, ImportStateVerify: true},
			{ResourceName: "redshift_datashare_membership.functions", ImportState: true, ImportStateVerify: true},
			{Config: configuration, PlanOnly: true},
			// Dependent lookups may be deferred while their managed inputs need repair; refresh must still
			// retain both ownership tuples and expose incomplete coverage rather than remove either from state.
			{RefreshState: true, ExpectNonEmptyPlan: true, PreConfig: func() {
				remove := sqlclient.Stmt("ALTER DATASHARE").Ident(name)
				for _, statement := range []string{
					remove.Kw("REMOVE TABLE").Qualified(name, "events").String(),
					remove.Kw("REMOVE FUNCTION").Qualified(name, "f_identity").Args(sqlclient.Kw("INTEGER")).String(),
				} {
					_, err := client.Query(ctx, producer, statement, nil)
					require.NoError(t, err)
				}
			}, Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttr("redshift_datashare_membership.tables", "all", "false"),
				resource.TestCheckResourceAttr("redshift_datashare_membership.functions", "all", "false"),
				memberCounts(1, 1),
			)},
			{Config: configuration, Check: resource.ComposeTestCheckFunc(coverage("true"), memberCounts(2, 2))},
			{Config: configuration, PlanOnly: true},
			// Destroy just the exclusive owners first: their source SQL objects and schema membership must survive.
			{Config: base, Check: resource.ComposeTestCheckFunc(memberCounts(0, 0),
				resource.TestCheckResourceAttr("redshift_table.events", "name", "events"),
				resource.TestCheckResourceAttr("redshift_table.audit", "name", "audit"),
				resource.TestCheckResourceAttr("redshift_function.integer", "name", "f_identity"),
				resource.TestCheckResourceAttr("redshift_function.varchar", "name", "f_identity"),
			)},
		},
	})
}
