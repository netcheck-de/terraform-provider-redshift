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

// A separate snapshot-isolated producer database avoids changing the existing fixture's isolation or share policy.
// Same-account writes require producer object grants to the datashare, not cross-account AllowWrites API calls:
// https://docs.aws.amazon.com/redshift/latest/dg/writes-adding-datashare.html
func TestAccObjectGrantSharedWrite(t *testing.T) {
	region, profile, workgroup, database := testAccWorkgroup(t, "REDSHIFT_ACC_SHARED_WRITE", "REDSHIFT_ACC_DIRECT", "REDSHIFT_ACC_SHARED_PRODUCER_CLUSTER", "REDSHIFT_ACC_SHARED_PRODUCER_SECRET_ARN", "REDSHIFT_ACC_SHARED_PRODUCER_NAMESPACE", "REDSHIFT_ACC_SHARED_CONSUMER_NAMESPACE")
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region), config.WithSharedConfigProfile(profile))
	require.NoError(t, err)

	producer := &dataapi.Client{API: redshiftdata.NewFromConfig(cfg), Cluster: os.Getenv("REDSHIFT_ACC_SHARED_PRODUCER_CLUSTER"), SecretARN: os.Getenv("REDSHIFT_ACC_SHARED_PRODUCER_SECRET_ARN"), Timeout: 5 * time.Minute, Poll: time.Second}
	consumer := &dataapi.Client{API: redshiftdata.NewFromConfig(cfg), Workgroup: workgroup, Timeout: 5 * time.Minute, Poll: time.Second}
	admin := sqlclient.Connection{Database: database}
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	source, shared, share := "acc_write_source_"+suffix, "acc_write_shared_"+suffix, "acc_write_share_"+suffix
	user, password := "acc_write_user_"+suffix, "Acc1Write"+suffix+"!"
	exec := func(client sqlclient.Client, connection sqlclient.Connection, statement string) {
		_, err := client.Query(ctx, connection, statement, nil)
		require.NoError(t, err, "%s", statement)
		t.Log(statement)
	}
	cleanup := func(client sqlclient.Client, connection sqlclient.Connection, statement string) {
		t.Cleanup(func() {
			_, err := client.Query(ctx, connection, statement, nil)
			assert.NoError(t, err, "%s", statement)
			t.Logf("cleanup: %s", statement)
		})
	}

	exec(producer, admin, sqlclient.Stmt("CREATE DATABASE").Ident(source).Kw("ISOLATION LEVEL SNAPSHOT").String())
	cleanup(producer, admin, sqlclient.Stmt("DROP DATABASE").Ident(source).String())
	sourceConnection := sqlclient.Connection{Database: source}
	exec(producer, sourceConnection, sqlclient.Stmt("CREATE SCHEMA").Ident("write_probe").String())
	cleanup(producer, sourceConnection, sqlclient.Stmt("DROP SCHEMA").Ident("write_probe").String())
	exec(producer, sourceConnection, sqlclient.Stmt("CREATE TABLE").Qualified("write_probe", "events").Kw("(id INTEGER)").String())
	cleanup(producer, sourceConnection, sqlclient.Stmt("DROP TABLE").Qualified("write_probe", "events").String())
	exec(producer, sourceConnection, sqlclient.Stmt("CREATE DATASHARE").Ident(share).Kw("SET PUBLICACCESSIBLE TRUE").String())
	cleanup(producer, sourceConnection, sqlclient.Stmt("DROP DATASHARE").Ident(share).String())
	exec(producer, sourceConnection, sqlclient.Stmt("GRANT USAGE ON SCHEMA").Ident("write_probe").KwIdent("TO DATASHARE", share).String())
	exec(producer, sourceConnection, sqlclient.Stmt("GRANT SELECT, INSERT ON TABLE").Qualified("write_probe", "events").KwIdent("TO DATASHARE", share).String())
	exec(producer, sourceConnection, sqlclient.Stmt("GRANT USAGE ON DATASHARE").Ident(share).Kw("TO NAMESPACE").Lit(os.Getenv("REDSHIFT_ACC_SHARED_CONSUMER_NAMESPACE")).String())
	exec(consumer, admin, sqlclient.Stmt("CREATE DATABASE").Ident(shared).KwIdent("WITH PERMISSIONS FROM DATASHARE", share).Kw("OF NAMESPACE").Lit(os.Getenv("REDSHIFT_ACC_SHARED_PRODUCER_NAMESPACE")).String())
	cleanup(consumer, admin, sqlclient.Stmt("DROP DATABASE").Ident(shared).String())

	routing, err := (&redshiftconn.IAM{Workgroup: workgroup, Serverless: redshiftserverless.NewFromConfig(cfg)}).Resolve(ctx, database)
	require.NoError(t, err)

	probe := &redshiftconn.Client{Credentials: redshiftconn.Credentials{Host: routing.Host, Port: routing.Port, Username: user, Password: password}, CACertFile: testAccCACertFile(), SSLMode: testAccSSLMode(), Timeout: 30 * time.Second}
	configuration := func(options bool) string {
		grantOptions := "[]"
		if options {
			grantOptions = `["INSERT", "SELECT"]`
		}

		return grantAcceptanceProvider(region, profile, workgroup, database) + fmt.Sprintf(`
resource "redshift_user" "writer" {
  name = %[1]q
  password_wo = %[2]q
}
resource "redshift_object_grant" "database" {
  database_name = %[3]q
  database_type = "shared"
  object_type = "DATABASE"
  grantee = redshift_user.writer.name
  grantee_type = "USER"
  privileges = ["USAGE"]
}
resource "redshift_object_grant" "schema" {
  database_name = %[3]q
  database_type = "shared"
  schema_name = "write_probe"
  object_type = "SCHEMA"
  grantee = redshift_user.writer.name
  grantee_type = "USER"
  privileges = ["USAGE"]
}
resource "redshift_object_grant" "table" {
  database_name = %[3]q
  database_type = "shared"
  schema_name = "write_probe"
  object_name = "events"
  object_type = "TABLE"
  grantee = redshift_user.writer.name
  grantee_type = "USER"
  privileges = ["INSERT", "SELECT"]
  grant_option_privileges = %[4]s
}
data "redshift_object_grant" "table" {
  database_name = redshift_object_grant.table.database_name
  database_type = "shared"
  schema_name = "write_probe"
  object_name = "events"
  object_type = "TABLE"
  grantee = redshift_user.writer.name
  grantee_type = "USER"
  depends_on = [redshift_object_grant.table]
}
`, user, password, shared, grantOptions)
	}
	check := func(options bool, id int64) resource.TestCheckFunc {
		count := "0"
		if options {
			count = "2"
		}

		return resource.ComposeAggregateTestCheckFunc(
			resource.TestCheckResourceAttr("redshift_object_grant.table", "grant_option_privileges.#", count),
			resource.TestCheckResourceAttr("data.redshift_object_grant.table", "grant_option_privileges.#", count),
			resource.TestCheckResourceAttr("data.redshift_object_grant.table", "privileges.#", "2"),
			func(*terraform.State) error {
				rows, err := consumer.Query(ctx, admin, sqlclient.Stmt("SHOW GRANTS ON TABLE").Qualified(shared, "write_probe", "events").String(), nil)
				require.NoError(t, err)

				for _, privilege := range []string{"INSERT", "SELECT"} {
					found := false

					for _, row := range rows {
						if row["identity_name"] == user && row["privilege_type"] == privilege {
							found = true

							assert.Equal(t, options, row["admin_option"] == "true" || row["admin_option"] == "t", "%s: %v", privilege, row)
						}
					}

					require.True(t, found, "%s missing from independent SHOW GRANTS: %v", privilege, rows)
				}

				exec(probe, admin, sqlclient.Stmt("INSERT INTO").Qualified(shared, "write_probe", "events").Kw("VALUES").Paren(sqlclient.Fragment().Int(id)).String())
				query := sqlclient.Stmt("SELECT id FROM").Qualified("write_probe", "events").Kw("WHERE id =").Int(id).String()
				rows, err = producer.Query(ctx, sourceConnection, query, nil)
				require.NoError(t, err)
				require.Equal(t, []sqlclient.Row{{"id": strconv.FormatInt(id, 10)}}, rows)
				rows, err = probe.Query(ctx, admin, sqlclient.Stmt("SELECT id FROM").Qualified(shared, "write_probe", "events").Kw("WHERE id =").Int(id).String(), nil)
				require.NoError(t, err)
				require.Equal(t, []sqlclient.Row{{"id": strconv.FormatInt(id, 10)}}, rows)
				t.Logf("producer confirms consumer USER INSERT id=%d; SELECT/INSERT grant options=%t", id, options)

				return nil
			},
		)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(New("test")())},
		CheckDestroy: func(*terraform.State) error {
			rows, err := consumer.Query(ctx, admin, sqlclient.Stmt("SHOW GRANTS ON TABLE").Qualified(shared, "write_probe", "events").String(), nil)
			require.NoError(t, err)

			for _, row := range rows {
				assert.NotEqual(t, user, row["identity_name"], "Terraform destroy must revoke the managed table ACL: %v", row)
			}

			t.Log("independent SHOW GRANTS confirms managed USER table ACL removed after Terraform destroy")

			return nil
		},
		Steps: []resource.TestStep{
			{Config: configuration(false), Check: check(false, 1)},
			{Config: configuration(true), Check: check(true, 2)},
			{Config: configuration(true), PlanOnly: true},
			{ResourceName: "redshift_object_grant.table", ImportState: true, ImportStateVerify: true},
			{Config: configuration(false), Check: check(false, 3)},
			{Config: configuration(false), PlanOnly: true},
		},
	})
}
