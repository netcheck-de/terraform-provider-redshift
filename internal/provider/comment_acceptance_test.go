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
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/redshiftdata"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAccCommentLifecycle verifies all annotation kinds, imports, drift repair, and annotation-only deletion.
func TestAccCommentLifecycle(t *testing.T) {
	region, profile, workgroup, database := testAccWorkgroup(t)
	name := "acc_comment_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	configuration := func(text string, enabled bool) string {
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
		for _, kind := range []string{"DATABASE", "SCHEMA", "TABLE", "VIEW", "COLUMN"} {
			base += fmt.Sprintf(`
resource "redshift_comment" %q {
  database_name = redshift_database.local.name
  object_type = %[2]q
  object_name = %[2]q == "DATABASE" ? redshift_database.local.name : %[2]q == "SCHEMA" ? redshift_schema.local.name : %[2]q == "VIEW" ? "fixture_view" : "fixture_table"
  schema_name = contains(["TABLE", "VIEW", "COLUMN"], %[2]q) ? redshift_schema.local.name : null
  column_name = %[2]q == "COLUMN" ? "id" : null
  text = %[3]q
}
data "redshift_comment" %[1]q {
  database_name = redshift_comment.%[1]s.database_name
  object_type = redshift_comment.%[1]s.object_type
  object_name = redshift_comment.%[1]s.object_name
  schema_name = redshift_comment.%[1]s.schema_name
  column_name = redshift_comment.%[1]s.column_name
}`, strings.ToLower(kind), kind, text)
		}
		return base
	}
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region), config.WithSharedConfigProfile(profile))
	require.NoError(t, err)
	client := &dataapi.Client{API: redshiftdata.NewFromConfig(cfg), Workgroup: workgroup, Timeout: 5 * time.Minute, Poll: time.Second}
	admin, target := sqlclient.Connection{Database: database}, sqlclient.Connection{Database: name}
	// A failed test may leave manual fixtures that block restrictive schema deletion.
	t.Cleanup(func() {
		rows, err := client.Query(ctx, admin, "SELECT database_name FROM svv_redshift_databases WHERE database_name = :name", map[string]string{"name": name})
		require.NoError(t, err)
		if len(rows) != 0 {
			_, err = client.Query(ctx, admin, "DROP DATABASE "+sqlclient.Identifier(name), nil)
			require.NoError(t, err)
		}
	})
	steps := []resource.TestStep{
		{Config: configuration("", false)},
		{Config: configuration("O'Reilly initial", true), PreConfig: func() {
			_, err := client.Query(ctx, target, `CREATE TABLE serving.fixture_table (id INTEGER)`, nil)
			require.NoError(t, err)
			_, err = client.Query(ctx, target, `CREATE VIEW serving.fixture_view AS SELECT id FROM serving.fixture_table`, nil)
			require.NoError(t, err)
		}},
		{Config: configuration("O'Reilly initial", true), PlanOnly: true},
	}
	for _, kind := range []string{"DATABASE", "SCHEMA", "TABLE", "VIEW", "COLUMN"} {
		steps = append(steps, resource.TestStep{ResourceName: "redshift_comment." + strings.ToLower(kind), ImportState: true, ImportStateVerify: true})
	}
	steps = append(steps,
		resource.TestStep{Config: configuration("updated", true), Check: resource.TestCheckResourceAttr("redshift_comment.column", "text", "updated")},
		resource.TestStep{Config: configuration("updated", true), PlanOnly: true, ExpectNonEmptyPlan: true, PreConfig: func() {
			_, err := client.Query(ctx, target, `COMMENT ON TABLE serving.fixture_table IS 'drift'`, nil)
			require.NoError(t, err)
		}},
		resource.TestStep{Config: configuration("updated", true)},
		resource.TestStep{Config: configuration("", false), Check: func(*terraform.State) error {
			for _, kind := range []string{"DATABASE", "SCHEMA", "TABLE", "VIEW", "COLUMN"} {
				data := commentModel{DatabaseName: types.StringValue(name), ObjectType: types.StringValue(kind), ObjectName: types.StringValue("fixture_table")}
				switch kind {
				case "DATABASE":
					data.ObjectName = types.StringValue(name)
				case "SCHEMA":
					data.ObjectName = types.StringValue("serving")
				default:
					data.SchemaName = types.StringValue("serving")
				}
				if kind == "VIEW" {
					data.ObjectName = types.StringValue("fixture_view")
				}
				if kind == "COLUMN" {
					data.ColumnName = types.StringValue("id")
				}
				_, query, err := data.target()
				require.NoError(t, err)
				rows, err := client.Query(ctx, target, query.sql, query.parameters)
				require.NoError(t, err)
				require.Len(t, rows, 1, "target object must survive comment deletion")
				assert.Empty(t, rows[0]["text"])
			}
			_, err := client.Query(ctx, target, `DROP VIEW serving.fixture_view`, nil)
			require.NoError(t, err)
			_, err = client.Query(ctx, target, `DROP TABLE serving.fixture_table`, nil)
			return err
		}},
	)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(New("test")())},
		Steps:                    steps,
	})
}
