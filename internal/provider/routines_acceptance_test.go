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
	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/redshiftdata"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/require"
)

// TestAccRoutinesLifecycle verifies function and procedure creation, overload lookups, in-place redefinition, drift
// repair, import followed by an in-place plan, and restrictive deletion in an isolated database.
func TestAccRoutinesLifecycle(t *testing.T) {
	region, profile, workgroup, database := testAccWorkgroup(t)
	name := "acc_routines_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	configuration := func(functionBody, volatility, security string) string {
		return fmt.Sprintf(`
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
resource "redshift_function" "label" {
  database = redshift_schema.local.database
  schema = redshift_schema.local.name
  name = "f_acc_label"
  arguments = ["int", "varchar(32)"]
  return_type = "varchar(64)"
  volatility = %q
  body = %q
}
resource "redshift_function" "label_overload" {
  database = redshift_schema.local.database
  schema = redshift_schema.local.name
  name = "f_acc_label"
  arguments = ["int"]
  return_type = "varchar"
  body = "SELECT $1::varchar"
}
data "redshift_function" "label" {
  database = redshift_function.label.database
  schema = redshift_function.label.schema
  name = redshift_function.label.name
  arguments = ["integer", "character varying"]
}
resource "redshift_procedure" "scale" {
  database = redshift_schema.local.database
  schema = redshift_schema.local.name
  name = "sp_acc_scale"
  argument {
    name = "factor"
    type = "integer"
  }
  argument {
    name = "amount"
    mode = "INOUT"
    type = "bigint"
  }
  argument {
    name = "label"
    mode = "OUT"
    type = "varchar(64)"
  }
  security = %q
  body = <<-SQL
    BEGIN
      amount := amount * factor;
      label := 'scaled';
    END;
  SQL
}
resource "redshift_procedure" "nonatomic" {
  database = redshift_schema.local.database
  schema = redshift_schema.local.name
  name = "sp_acc_nonatomic"
  nonatomic = true
  body = "BEGIN NULL; END;"
}
data "redshift_procedure" "scale" {
  database = redshift_procedure.scale.database
  schema = redshift_procedure.scale.schema
  name = redshift_procedure.scale.name
  arguments = [for argument in redshift_procedure.scale.argument : argument.type if argument.mode != "OUT"]
}
`, region, profile, workgroup, database, name, volatility, functionBody, security)
	}
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region), config.WithSharedConfigProfile(profile))
	require.NoError(t, err)
	client := &dataapi.Client{API: redshiftdata.NewFromConfig(cfg), Workgroup: workgroup, Timeout: 5 * time.Minute, Poll: time.Second}
	admin, target := sqlclient.Connection{Database: database}, sqlclient.Connection{Database: name}
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
			{Config: configuration("SELECT $2 || '-' || $1::varchar", "IMMUTABLE", "INVOKER"), Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("redshift_function.label", "signature", "INTEGER, CHARACTER VARYING"),
				resource.TestCheckResourceAttr("data.redshift_function.label", "return_type", "CHARACTER VARYING"),
				resource.TestCheckResourceAttr("redshift_procedure.scale", "signature", "INTEGER, BIGINT"),
				resource.TestCheckResourceAttr("data.redshift_procedure.scale", "security", "INVOKER"),
				resource.TestCheckResourceAttr("data.redshift_procedure.scale", "argument.#", "3"),
				resource.TestCheckResourceAttr("data.redshift_procedure.scale", "argument.2.mode", "OUT"),
				resource.TestCheckResourceAttr("data.redshift_procedure.scale", "argument.2.type", "CHARACTER VARYING"),
			)},
			{Config: configuration("SELECT $2 || '-' || $1::varchar", "IMMUTABLE", "INVOKER"), PlanOnly: true},
			// Imports report canonical catalog spellings and the catalog body, and cannot observe nonatomic or SET.
			{ResourceName: "redshift_function.label", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"arguments", "return_type", "body"}},
			{ResourceName: "redshift_procedure.scale", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"argument", "body", "nonatomic", "configuration"}},
			// Imported state holds types without their modifiers; planning the original configuration against it must
			// update in place rather than replace a routine that grants or views may depend on.
			{ResourceName: "redshift_function.label", ImportState: true, ImportStatePersist: true},
			{ResourceName: "redshift_procedure.scale", ImportState: true, ImportStatePersist: true},
			{Config: configuration("SELECT $2 || '-' || $1::varchar", "IMMUTABLE", "INVOKER"), ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
				plancheck.ExpectResourceAction("redshift_function.label", plancheck.ResourceActionUpdate),
				plancheck.ExpectResourceAction("redshift_procedure.scale", plancheck.ResourceActionUpdate),
			}}},
			{Config: configuration("SELECT $2 || '-' || $1::varchar", "IMMUTABLE", "INVOKER"), PlanOnly: true},
			{Config: configuration("SELECT $1::varchar || ':' || $2", "STABLE", "DEFINER"), Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("redshift_function.label", "volatility", "STABLE"),
				resource.TestCheckResourceAttr("redshift_procedure.scale", "security", "DEFINER"),
			)},
			{Config: configuration("SELECT $1::varchar || ':' || $2", "STABLE", "DEFINER"), PlanOnly: true, ExpectNonEmptyPlan: true, PreConfig: func() {
				_, err := client.Query(ctx, target, `CREATE OR REPLACE FUNCTION serving.f_acc_label(integer, varchar) RETURNS varchar STABLE AS $$ SELECT 'drift' $$ LANGUAGE sql`, nil)
				require.NoError(t, err)
			}},
			{Config: configuration("SELECT $1::varchar || ':' || $2", "STABLE", "DEFINER")},
		},
	})
}
