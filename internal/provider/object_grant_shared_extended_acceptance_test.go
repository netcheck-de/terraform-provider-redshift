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
	"github.com/aws/aws-sdk-go-v2/service/redshiftserverless"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/netcheck-de/terraform-provider-redshift/internal/redshiftconn"
	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/redshiftdata"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAccObjectGrantSharedExtended creates SQL-only routine fixtures inside an already-shared producer schema. It
// verifies name-wide functions (including shared procedures), snapshot options, shared INSERT grants, and teardown.
func TestAccObjectGrantSharedExtended(t *testing.T) {
	region, profile, workgroup, database := testAccWorkgroup(t, "REDSHIFT_ACC_SHARED_DATABASE", "REDSHIFT_ACC_SHARED_SCHEMA", "REDSHIFT_ACC_SHARED_PRODUCER_CLUSTER", "REDSHIFT_ACC_SHARED_PRODUCER_SECRET_ARN", "REDSHIFT_ACC_SHARED_DATASHARE")
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region), config.WithSharedConfigProfile(profile))
	require.NoError(t, err)

	producer := &dataapi.Client{API: redshiftdata.NewFromConfig(cfg), Cluster: os.Getenv("REDSHIFT_ACC_SHARED_PRODUCER_CLUSTER"), SecretARN: os.Getenv("REDSHIFT_ACC_SHARED_PRODUCER_SECRET_ARN"), Timeout: 5 * time.Minute, Poll: time.Second}
	connection := sqlclient.Connection{Database: database}
	schema, share := os.Getenv("REDSHIFT_ACC_SHARED_SCHEMA"), os.Getenv("REDSHIFT_ACC_SHARED_DATASHARE")
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	function, procedure := "f_acc_shared_"+suffix, "p_acc_shared_"+suffix

	exec := func(statement string) {
		_, err := producer.Query(ctx, connection, statement, nil)
		require.NoError(t, err, "%s", statement)
	}
	for _, argument := range []sqlclient.Keyword{"INTEGER", "VARCHAR"} {
		exec(createFunctionStatement(functionSpec{schema: schema, name: function, arguments: []sqlclient.Keyword{argument}, returns: argument, volatility: "IMMUTABLE", body: "SELECT $1"}, false))
		t.Cleanup(func() {
			_, err := producer.Query(ctx, connection, sqlclient.Stmt("ALTER DATASHARE").Ident(share).Kw("REMOVE FUNCTION").Qualified(schema, function).Args(sqlclient.Kw(argument)).String(), nil)
			assert.NoError(t, err)
			_, err = producer.Query(ctx, connection, sqlclient.Stmt("DROP FUNCTION").Qualified(schema, function).Args(sqlclient.Kw(argument)).String(), nil)
			assert.NoError(t, err)
		})
		exec(sqlclient.Stmt("ALTER DATASHARE").Ident(share).Kw("ADD FUNCTION").Qualified(schema, function).Args(sqlclient.Kw(argument)).String())
	}

	exec(createProcedureStatement(procedureSpec{schema: schema, name: procedure, body: "BEGIN RAISE INFO 'probe'; END;", security: "INVOKER"}, false))
	t.Cleanup(func() {
		_, err := producer.Query(ctx, connection, sqlclient.Stmt("REVOKE EXECUTE ON PROCEDURE").Qualified(schema, procedure).Args().KwIdent("FROM DATASHARE", share).String(), nil)
		assert.NoError(t, err)
		_, err = producer.Query(ctx, connection, sqlclient.Stmt("DROP PROCEDURE").Qualified(schema, procedure).Args().String(), nil)
		assert.NoError(t, err)
	})
	exec(sqlclient.Stmt("GRANT EXECUTE ON PROCEDURE").Qualified(schema, procedure).Args().KwIdent("TO DATASHARE", share).String())

	configuration := func(options, empty bool) string {
		privileges, grantOptions := `["SELECT", "INSERT"]`, `["SELECT"]`

		execute, executeOptions := `["EXECUTE"]`, `["EXECUTE"]`
		if !options {
			grantOptions, executeOptions = `[]`, `[]`
		}

		if empty {
			privileges, grantOptions, execute, executeOptions = `[]`, `[]`, `[]`, `[]`
		}

		return grantAcceptanceProvider(region, profile, workgroup, database) + fmt.Sprintf(`
resource "redshift_user" "single" {
  name = "acc_shared_single_%[1]s"
  password_wo = "Acc1Shared%[1]s!"
}
resource "redshift_user" "snapshot" {
  name = "acc_shared_snapshot_%[1]s"
  password_wo = "Acc1Snapshot%[1]s!"
}
resource "redshift_object_grant" "database_usage" {
  database_name = %[2]q
  database_type = "shared"
  object_type = "DATABASE"
  grantee = redshift_user.single.name
  grantee_type = "USER"
  privileges = ["USAGE"]
}
resource "redshift_object_grant" "schema_usage" {
  database_name = %[2]q
  database_type = "shared"
  schema_name = %[3]q
  object_type = "SCHEMA"
  grantee = redshift_user.single.name
  grantee_type = "USER"
  privileges = ["USAGE"]
}
resource "redshift_object_grant" "function" {
  database_name = %[2]q
  database_type = "shared"
  schema_name = %[3]q
  object_name = %[4]q
  object_type = "FUNCTION"
  grantee = redshift_user.single.name
  grantee_type = "USER"
  privileges = %[7]s
  grant_option_privileges = %[8]s
}
resource "redshift_object_grant" "procedure" {
  database_name = %[2]q
  database_type = "shared"
  schema_name = %[3]q
  object_name = %[5]q
  object_type = "PROCEDURE"
  grantee = redshift_user.single.name
  grantee_type = "USER"
  privileges = %[7]s
  grant_option_privileges = %[8]s
}
resource "redshift_object_grant" "table" {
  database_name = %[2]q
  database_type = "shared"
  schema_name = %[3]q
  object_name = "events"
  object_type = "TABLE"
  grantee = redshift_user.single.name
  grantee_type = "USER"
  privileges = %[6]s
  grant_option_privileges = %[9]s
}
resource "redshift_object_grant" "all_tables" {
  database_name = %[2]q
  database_type = "shared"
  schema_name = %[3]q
  object_type = "ALL TABLES"
  grantee = redshift_user.snapshot.name
  grantee_type = "USER"
  privileges = %[6]s
  grant_option_privileges = %[9]s
}
resource "redshift_object_grant" "all_functions" {
  database_name = %[2]q
  database_type = "shared"
  schema_name = %[3]q
  object_type = "ALL FUNCTIONS"
  grantee = redshift_user.snapshot.name
  grantee_type = "USER"
  privileges = %[7]s
  grant_option_privileges = %[8]s
}
resource "redshift_role" "procedure_reader" { name = "acc_shared_procs_%[1]s" }
resource "redshift_object_grant" "all_procedures" {
  database_name = %[2]q
  database_type = "shared"
  schema_name = %[3]q
  object_type = "ALL PROCEDURES"
  grantee = redshift_role.procedure_reader.name
  grantee_type = "ROLE"
  privileges = %[7]s
}
data "redshift_object_grant" "all_functions" {
  database_name = redshift_object_grant.all_functions.database_name
  database_type = "shared"
  schema_name = %[3]q
  object_type = "ALL FUNCTIONS"
  grantee = redshift_user.snapshot.name
  grantee_type = "USER"
  depends_on = [redshift_object_grant.all_functions]
}
`, suffix, os.Getenv("REDSHIFT_ACC_SHARED_DATABASE"), schema, function, procedure, privileges, execute, executeOptions, grantOptions)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(New("test")())},
		Steps: []resource.TestStep{
			{Config: configuration(true, false), Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("redshift_object_grant.function", "grant_option_privileges.#", "1"),
				resource.TestCheckResourceAttr("redshift_object_grant.procedure", "privileges.#", "1"),
				resource.TestCheckResourceAttr("redshift_object_grant.all_procedures", "privileges.#", "1"),
				resource.TestCheckResourceAttr("redshift_object_grant.table", "privileges.#", "2"),
				resource.TestCheckResourceAttr("data.redshift_object_grant.all_functions", "grant_option_privileges.#", "1"),
				func(*terraform.State) error {
					if os.Getenv("REDSHIFT_ACC_DIRECT") == "" {
						t.Log("shared INSERT ACL verified, but write execution boundary not checked without REDSHIFT_ACC_DIRECT")
						return nil
					}

					routing, err := (&redshiftconn.IAM{Workgroup: workgroup, Serverless: redshiftserverless.NewFromConfig(cfg)}).Resolve(ctx, database)
					require.NoError(t, err)

					probe := &redshiftconn.Client{Credentials: redshiftconn.Credentials{Host: routing.Host, Port: routing.Port, Username: "acc_shared_single_" + suffix, Password: "Acc1Shared" + suffix + "!"}, CACertFile: testAccCACertFile(), SSLMode: testAccSSLMode(), Timeout: 30 * time.Second}
					// A zero-row insert still checks the write boundary without changing producer data if it succeeds.
					statement := sqlclient.Stmt("INSERT INTO").Qualified(os.Getenv("REDSHIFT_ACC_SHARED_DATABASE"), schema, "events").Kw("SELECT * FROM").Qualified(os.Getenv("REDSHIFT_ACC_SHARED_DATABASE"), schema, "events").Kw("WHERE FALSE").String()
					_, err = probe.Query(ctx, connection, statement, nil)
					require.Error(t, err, "this acceptance fixture is intentionally shared without write access")
					t.Logf("shared INSERT execution with database/schema USAGE and INSERT ACL: %v", err)
					assert.Contains(t, err.Error(), "Permission denied on producer to execute insert", "denial must be the producer sharing policy, not a missing consumer privilege or a connection failure")

					return nil
				},
			)},
			{Config: configuration(true, false), PlanOnly: true},
			{ResourceName: "redshift_object_grant.function", ImportState: true, ImportStateVerify: true},
			{ResourceName: "redshift_object_grant.all_functions", ImportState: true, ImportStateVerify: true},
			{ResourceName: "redshift_object_grant.procedure", ImportState: true, ImportStateVerify: true},
			{ResourceName: "redshift_object_grant.all_procedures", ImportState: true, ImportStateVerify: true},
			{Config: configuration(false, false), Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("redshift_object_grant.function", "grant_option_privileges.#", "0"),
				resource.TestCheckResourceAttr("redshift_object_grant.all_tables", "grant_option_privileges.#", "0"),
				resource.TestCheckResourceAttr("data.redshift_object_grant.all_functions", "grant_option_privileges.#", "0"),
			)},
			{Config: configuration(false, true), Check: resource.TestCheckResourceAttr("data.redshift_object_grant.all_functions", "privileges.#", "0")},
		},
	})
}
