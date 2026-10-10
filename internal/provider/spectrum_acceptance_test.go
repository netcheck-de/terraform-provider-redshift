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
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/redshiftdata"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/require"
)

// TestAccSpectrumExternalTableLifecycle creates a partitioned external table and a partition, changes both in place,
// imports them, and checks that a refresh plans nothing. An ORC table beside them, which maps columns by name, inserts
// a column mid-list in place while the catalog appends it. It needs an existing Glue database, an IAM role attached to
// the namespace that may write it, and an S3 prefix in the warehouse's region; the S3 data is never written.
func TestAccSpectrumExternalTableLifecycle(t *testing.T) {
	region, profile, workgroup, database := testAccWorkgroup(t, "REDSHIFT_ACC_SPECTRUM_ROLE_ARN", "REDSHIFT_ACC_SPECTRUM_GLUE_DATABASE", "REDSHIFT_ACC_SPECTRUM_LOCATION")
	role, glue := os.Getenv("REDSHIFT_ACC_SPECTRUM_ROLE_ARN"), os.Getenv("REDSHIFT_ACC_SPECTRUM_GLUE_DATABASE")
	location := strings.TrimSuffix(os.Getenv("REDSHIFT_ACC_SPECTRUM_LOCATION"), "/") + "/"
	metrics := strings.TrimSuffix(location, "/") + "_metrics/"
	schema := "acc_spectrum_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region), config.WithSharedConfigProfile(profile))
	require.NoError(t, err)
	client := &dataapi.Client{API: redshiftdata.NewFromConfig(cfg), Workgroup: workgroup, Timeout: 5 * time.Minute, Poll: time.Second}
	t.Cleanup(func() {
		// A failed run may leave the table in the shared Glue database, which restrictive DROP SCHEMA cannot remove.
		for _, table := range []string{"events", "metrics"} {
			_, err := client.Query(ctx, sqlclient.Connection{Database: database}, "DROP TABLE IF EXISTS "+sqlclient.Identifier(schema)+"."+table, nil)
			if err != nil {
				t.Logf("external table cleanup: %v", err)
			}
		}
	})
	configuration := func(tableLocation, partitionLocation, extraColumn, numRows, orcColumns string) string {
		return fmt.Sprintf(`
provider "redshift" {
  region = %q
  profile = %q
  workgroup_name = %q
  database = %q
}
resource "redshift_external_schema" "lake" {
  database = %[4]q
  name = %[5]q
  glue_database = %[6]q
  iam_role_arn = %[7]q
}
resource "redshift_external_table" "events" {
  database = redshift_external_schema.lake.database
  schema = redshift_external_schema.lake.name
  name = "events"
  dynamic "column" {
    for_each = concat([
      { name = "id", type = "int4" },
      { name = "label", type = "varchar(64)" },
    ], %[8]s)
    content {
      name = column.value.name
      type = column.value.type
    }
  }
  partition_key {
    name = "event_date"
    type = "date"
  }
  field_delimiter = "\t"
  stored_as = "TEXTFILE"
  location = %[9]q
  table_properties = { "skip.header.line.count" = "1", "numRows" = %[10]q }
}
resource "redshift_external_table" "metrics" {
  database = redshift_external_schema.lake.database
  schema = redshift_external_schema.lake.name
  name = "metrics"
  dynamic "column" {
    for_each = %[12]s
    content {
      name = column.value.name
      type = column.value.type
    }
  }
  stored_as = "ORC"
  location = %[13]q
}
data "redshift_external_table" "metrics" {
  database = redshift_external_table.metrics.database
  schema = redshift_external_table.metrics.schema
  name = redshift_external_table.metrics.name
}
resource "redshift_external_partition" "first" {
  database = redshift_external_table.events.database
  schema = redshift_external_table.events.schema
  table = redshift_external_table.events.name
  values = { event_date = "2024-01-01" }
  location = %[11]q
}
data "redshift_external_table" "events" {
  database = redshift_external_table.events.database
  schema = redshift_external_table.events.schema
  name = redshift_external_table.events.name
}
data "redshift_external_partition" "first" {
  database = redshift_external_partition.first.database
  schema = redshift_external_partition.first.schema
  table = redshift_external_partition.first.table
  values = redshift_external_partition.first.values
}`, region, profile, workgroup, database, schema, glue, role, extraColumn, tableLocation, numRows, partitionLocation, orcColumns, metrics)
	}
	moved := location + "v2/"
	initial := configuration(location, location+"event_date=2024-01-01/", "[]", "10", `[{ name = "id", type = "bigint" }, { name = "label", type = "varchar(64)" }]`)
	changed := configuration(moved, moved+"event_date=2024-01-01/", `[{ name = "amount", type = "decimal(8,2)" }]`, "20",
		`[{ name = "id", type = "bigint" }, { name = "amount", type = "decimal(8,2)" }, { name = "label", type = "varchar(64)" }]`)
	stateID := func(name string) resource.ImportStateIdFunc {
		return func(state *terraform.State) (string, error) {
			return state.RootModule().Resources[name].Primary.Attributes["id"], nil
		}
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(New("test")())},
		Steps: []resource.TestStep{
			{Config: initial, Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttr("data.redshift_external_table.events", "column.0.type", "INTEGER"),
				resource.TestCheckResourceAttr("data.redshift_external_table.events", "stored_as", "TEXTFILE"),
				resource.TestCheckResourceAttr("data.redshift_external_table.events", "field_delimiter", "\t"),
				resource.TestCheckResourceAttr("data.redshift_external_table.events", "partition_key.0.name", "event_date"),
				resource.TestCheckResourceAttrSet("data.redshift_external_partition.first", "location"),
			)},
			{Config: initial, PlanOnly: true},
			{Config: changed, ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
				plancheck.ExpectResourceAction("redshift_external_table.events", plancheck.ResourceActionUpdate),
				plancheck.ExpectResourceAction("redshift_external_partition.first", plancheck.ResourceActionUpdate),
				plancheck.ExpectResourceAction("redshift_external_table.metrics", plancheck.ResourceActionUpdate),
			}}, Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttr("data.redshift_external_table.events", "column.2.name", "amount"),
				resource.TestCheckResourceAttr("redshift_external_table.metrics", "column.1.name", "amount"),
				resource.TestCheckResourceAttr("data.redshift_external_table.metrics", "column.2.name", "amount"),
			)},
			{ResourceName: "redshift_external_table.events", ImportState: true, ImportStateIdFunc: stateID("redshift_external_table.events"), ImportStateVerify: true, ImportStateVerifyIgnore: []string{"table_properties", "column.0.type", "location"}},
			{ResourceName: "redshift_external_partition.first", ImportState: true, ImportStateIdFunc: stateID("redshift_external_partition.first"), ImportStateVerify: true, ImportStateVerifyIgnore: []string{"location"}},
			{Config: changed, PlanOnly: true},
		},
	})
}
