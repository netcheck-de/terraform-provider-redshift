package provider

import (
	"context"
	"fmt"
	"strconv"
	"strings"
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

// TestAccDatashareTable needs no second warehouse. Explicit membership never overlaps a bulk owner
// or INCLUDENEW policy. Removing/replacing membership must preserve both source tables.
func TestAccDatashareTable(t *testing.T) {
	region, workgroup, database := testAccWorkgroup(t)
	name := "acc_member_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	require.NoError(t, err)

	client := &dataapi.Client{API: redshiftdata.NewFromConfig(cfg), Workgroup: workgroup, Timeout: 5 * time.Minute, Poll: time.Second}
	admin := sqlclient.Connection{Database: database}
	producer := sqlclient.Connection{Database: name}
	query := func(connection sqlclient.Connection, statement sqlclient.Query) ([]sqlclient.Row, error) {
		sql, parameters, err := statement.Build()
		if err != nil {
			return nil, err
		}

		return client.Query(ctx, connection, sql, parameters)
	}

	databases := sqlclient.Select("database_name").From("svv_redshift_databases").Where("database_name = :name", sqlclient.Bind("name", name))
	shares := sqlclient.Select("share_name").From("svv_datashares").Where("share_type = 'OUTBOUND'").Where("share_name = :name", sqlclient.Bind("name", name))

	for _, statement := range []sqlclient.Query{databases, shares} {
		rows, err := query(admin, statement)
		require.NoError(t, err)
		require.Empty(t, rows, "fixture name must not refer to existing SQL objects")
	}

	t.Cleanup(func() {
		rows, err := query(admin, databases)
		require.NoError(t, err)

		if len(rows) == 0 {
			return
		}

		rows, err = query(admin, shares)
		require.NoError(t, err)

		if len(rows) != 0 {
			_, err = client.Query(ctx, producer, sqlclient.Stmt("DROP DATASHARE").Ident(name).String(), nil)
			require.NoError(t, err)
		}
		// CASCADE is restricted to the unique schema in this test's newly created database.
		_, err = client.Query(ctx, producer, sqlclient.Stmt("DROP SCHEMA IF EXISTS").Ident(name).Kw("CASCADE").String(), nil)
		require.NoError(t, err)
		_, err = client.Query(ctx, admin, sqlclient.Stmt("DROP DATABASE").Ident(name).String(), nil)
		require.NoError(t, err)
	})

	base := fmt.Sprintf(`
provider "redshift" {
  region = %q
  workgroup_name = %q
  database = %q
}
resource "redshift_database" "local" { name = %q }
resource "redshift_schema" "local" {
  database = redshift_database.local.name
  name = %q
}
resource "redshift_table" "source" {
  for_each = toset(["events", "audit"])
  database = redshift_schema.local.database
  schema = redshift_schema.local.name
  name = each.key
  column {
    name = "id"
    type = "INTEGER"
  }
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
}`, region, workgroup, database, name, name, name)
	observations := func(dependency string) string {
		return fmt.Sprintf(`
data "redshift_datashare_schema" "local" {
  database = redshift_datashare_schema.local.database
  datashare = redshift_datashare_schema.local.datashare
  schema = redshift_datashare_schema.local.schema
}
data "redshift_datashare_schema" "missing" {
  database = redshift_datashare.local.database
  datashare = redshift_datashare.local.name
  schema = "absent"
}
data "redshift_datashare_table" "source" {
  for_each = redshift_table.source
  database = each.value.database
  datashare = redshift_datashare.local.name
  schema = each.value.schema
  table = each.value.name
  depends_on = [%s]
}
data "redshift_datashare_tables" "tables" {
  database = redshift_datashare.local.database
  datashare = redshift_datashare.local.name
  schema = redshift_schema.local.name
  depends_on = [%s]
}
data "redshift_datashare_schemas" "schemas" {
  database = redshift_datashare_schema.local.database
  datashare = redshift_datashare_schema.local.datashare
  schema = redshift_schema.local.name
}
data "redshift_datashare_schemas" "missing" {
  database = redshift_datashare_schema.local.database
  datashare = redshift_datashare_schema.local.datashare
  schema = "absent"
}`, dependency, dependency)
	}
	membership := func(table string) string {
		return fmt.Sprintf(`
resource "redshift_datashare_table" "local" {
  database = redshift_datashare_schema.local.database
  datashare = redshift_datashare_schema.local.datashare
  schema = redshift_datashare_schema.local.schema
  table = redshift_table.source[%q].name
}`, table)
	}
	check := func(table string, includeNew bool) resource.TestCheckFunc {
		count := 0
		if table != "" {
			count = 1
		}

		checks := []resource.TestCheckFunc{
			resource.TestCheckResourceAttr("data.redshift_datashare_schema.local", "exists", "true"),
			resource.TestCheckResourceAttr("data.redshift_datashare_schema.local", "include_new", strconv.FormatBool(includeNew)),
			resource.TestCheckResourceAttr("data.redshift_datashare_schema.missing", "exists", "false"),
			resource.TestCheckResourceAttr("data.redshift_datashare_tables.tables", "datashare_tables.#", strconv.Itoa(count)),
			resource.TestCheckResourceAttr("data.redshift_datashare_schemas.schemas", "datashare_schemas.#", "1"),
			resource.TestCheckResourceAttr("data.redshift_datashare_schemas.missing", "datashare_schemas.#", "0"),
			resource.TestCheckResourceAttr("data.redshift_datashare_schemas.schemas", "datashare_schemas.0.schema", name),
			resource.TestCheckResourceAttr("data.redshift_datashare_schemas.schemas", "datashare_schemas.0.include_new", strconv.FormatBool(includeNew)),
		}
		for _, source := range []string{"events", "audit"} {
			checks = append(checks, resource.TestCheckResourceAttr(fmt.Sprintf("data.redshift_datashare_table.source[%q]", source), "exists", strconv.FormatBool(source == table)))
		}

		if table != "" {
			checks = append(checks,
				resource.TestCheckResourceAttr("redshift_datashare_table.local", "table", table),
				resource.TestCheckResourceAttr("data.redshift_datashare_tables.tables", "datashare_tables.0.table", table),
				resource.TestCheckResourceAttr("data.redshift_datashare_tables.tables", "datashare_tables.0.schema", name),
				resource.TestCheckResourceAttr("data.redshift_datashare_tables.tables", "datashare_tables.0.datashare", name),
			)
		}

		checks = append(checks, func(*terraform.State) error {
			rows, err := query(producer, sqlclient.Select("object_name").From("svv_datashare_objects").Where("share_name = :share", sqlclient.Bind("share", name)).Where("share_type = 'OUTBOUND'").Where("object_type = 'table'"))
			if err != nil {
				return err
			}

			if len(rows) != count || (count == 1 && rows[0]["object_name"] != name+"."+table) {
				return fmt.Errorf("expected only table membership %q, got %v", table, rows)
			}

			rows, err = query(producer, sqlclient.Select("table_name").From("svv_tables").Where("table_schema = :schema", sqlclient.Bind("schema", name)))
			if err != nil {
				return err
			}

			if len(rows) != 2 {
				return fmt.Errorf("both source tables must survive membership changes: %v", rows)
			}

			return nil
		})

		return resource.ComposeAggregateTestCheckFunc(checks...)
	}
	withoutMember := base + observations("redshift_datashare_schema.local")
	events := base + membership("events") + observations("redshift_datashare_table.local")
	audit := base + membership("audit") + observations("redshift_datashare_table.local")
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(New("test")())},
		CheckDestroy: func(*terraform.State) error {
			for _, statement := range []sqlclient.Query{databases, shares} {
				rows, err := query(admin, statement)
				if err != nil {
					return err
				}

				if len(rows) != 0 {
					return fmt.Errorf("acceptance fixture remains after destroy: %v", rows)
				}
			}

			return nil
		},
		Steps: []resource.TestStep{
			{Config: withoutMember, Check: check("", false)},
			{Config: events, Check: check("events", false)},
			{ResourceName: "redshift_datashare_table.local", ImportState: true, ImportStateVerify: true},
			{Config: audit, Check: check("audit", false)},
			{ResourceName: "redshift_datashare_table.local", ImportState: true, ImportStateVerify: true},
			{Config: audit, PreConfig: func() {
				_, err := client.Query(ctx, producer, sqlclient.Stmt("ALTER DATASHARE").Ident(name).Kw("REMOVE TABLE").Qualified(name, "audit").String(), nil)
				require.NoError(t, err)
			}, Check: check("audit", false)},
			// Drop the lookups with the membership, then read again so they cannot observe the pre-delete catalog.
			{Config: base},
			{Config: withoutMember, Check: check("", false)},
			// Observe the future-object flag without creating future tables or overlapping an explicit owner.
			{Config: strings.Replace(withoutMember, "include_new = false", "include_new = true", 1), Check: check("", true)},
		},
	})
}
