package provider

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/redshiftdata"
	redshifttypes "github.com/aws/aws-sdk-go-v2/service/redshiftdata/types"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/redshiftdata"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/require"
)

// copyJobAcceptanceClient returns the acceptance Data API client, authenticated with REDSHIFT_ACC_SECRET_ARN when it is
// set.
func copyJobAcceptanceClient(t *testing.T, region, profile, workgroup string) sqlclient.Client {
	t.Helper()
	client := testAccRolesClient(t, region, profile, workgroup).(*dataapi.Client)
	client.SecretARN = os.Getenv("REDSHIFT_ACC_SECRET_ARN")

	return client
}

// TestAccCopyJobDatabaseScope confirms that SYS_COPY_JOB lists a job only in its own database, so the read query needs
// no database filter.
func TestAccCopyJobDatabaseScope(t *testing.T) {
	region, profile, workgroup, _ := testAccWorkgroup(t, "REDSHIFT_ACC_COPY_JOB_NAME", "REDSHIFT_ACC_COPY_JOB_DATABASE", "REDSHIFT_ACC_COPY_JOB_OTHER_DATABASE")
	client := copyJobAcceptanceClient(t, region, profile, workgroup)
	job, database, other := os.Getenv("REDSHIFT_ACC_COPY_JOB_NAME"), os.Getenv("REDSHIFT_ACC_COPY_JOB_DATABASE"), os.Getenv("REDSHIFT_ACC_COPY_JOB_OTHER_DATABASE")
	require.NotEqual(t, database, other)

	query, args, err := readCopyJobQuery(job).Build()
	require.NoError(t, err)
	rows, err := client.Query(context.Background(), sqlclient.Connection{Database: database}, query, args)
	require.NoError(t, err)
	require.Len(t, rows, 1)

	model := copyJobModel{Database: types.StringValue(database), Name: types.StringValue(job), Parameters: types.MapNull(types.StringType)}
	require.NoError(t, readCopyJobDefinition(&model, rows[0]["copy_query"]))
	t.Logf("job %s definition is readable in %s", job, database)

	rows, err = client.Query(context.Background(), sqlclient.Connection{Database: other}, query, args)
	require.NoError(t, err)
	require.Empty(t, rows, "SYS_COPY_JOB must be database-local; warehouse-wide rows would require a database filter")
	t.Logf("job %s is absent from SYS_COPY_JOB in %s", job, other)
}

// TestAccCopyJobNonOwnerVisibility uses an isolated SQL identity and a short-lived Data API session, not AWS fixtures.
func TestAccCopyJobNonOwnerVisibility(t *testing.T) {
	region, profile, workgroup, _ := testAccWorkgroup(t, "REDSHIFT_ACC_COPY_JOB_DATABASE", "REDSHIFT_ACC_SECRET_ARN")
	database := os.Getenv("REDSHIFT_ACC_COPY_JOB_DATABASE")
	client := copyJobAcceptanceClient(t, region, profile, workgroup).(*dataapi.Client)
	name := "acc_copy_reader_" + strconv.FormatInt(time.Now().UnixNano(), 36)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	_, err := client.Query(ctx, sqlclient.Connection{Database: database}, sqlclient.Stmt("CREATE USER").Ident(name).Kw("PASSWORD DISABLE").String(), nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := client.Query(context.Background(), sqlclient.Connection{Database: database}, sqlclient.Stmt("DROP USER").Ident(name).String(), nil)
		require.NoError(t, err)
	})

	run := func(statement string, session *string, keep int32) *redshiftdata.ExecuteStatementOutput {
		input := &redshiftdata.ExecuteStatementInput{
			Sql: aws.String(statement), SessionId: session, SessionKeepAliveSeconds: aws.Int32(keep), WaitTimeSeconds: aws.Int32(30),
		}
		if session == nil {
			input.Database = aws.String(database)
			input.WorkgroupName = aws.String(workgroup)
			input.SecretArn = aws.String(client.SecretARN)
		}

		result, err := client.API.ExecuteStatement(ctx, input)
		require.NoError(t, err)

		status := result.Status
		for status != redshifttypes.StatementStatusStringFinished {
			described, err := client.API.DescribeStatement(ctx, &redshiftdata.DescribeStatementInput{Id: result.Id})
			require.NoError(t, err)
			require.NotEqual(t, redshifttypes.StatusStringFailed, described.Status, "%s", aws.ToString(described.Error))
			require.NotEqual(t, redshifttypes.StatusStringAborted, described.Status)

			if described.Status == redshifttypes.StatusStringFinished {
				break
			}

			time.Sleep(time.Second)
		}

		return result
	}
	started := run(sqlclient.Stmt("SET SESSION AUTHORIZATION").Ident(name).String(), nil, 60)

	require.NotNil(t, started.SessionId)
	defer run("RESET SESSION AUTHORIZATION", started.SessionId, 0)

	query, _, err := sqlclient.Select("job_name").From("sys_copy_job").Build()
	require.NoError(t, err)

	result := run(query, started.SessionId, 60)
	rows, err := client.API.GetStatementResult(ctx, &redshiftdata.GetStatementResultInput{Id: result.Id})
	require.NoError(t, err)
	require.Empty(t, rows.Records, "an unprivileged identity with no COPY jobs must not see other owners' jobs")
}

// TestAccCopyCatalogDefinitionAvailability probes engine support even before an integrated COPY job exists.
func TestAccCopyCatalogDefinitionAvailability(t *testing.T) {
	region, profile, workgroup, database := testAccWorkgroup(t)
	client := copyJobAcceptanceClient(t, region, profile, workgroup)
	query := sqlclient.Select("copy_query", "data_source", "table_id", "job_owner").From("sys_copy_job").Where("1 = 0")
	text, args, err := query.Build()
	require.NoError(t, err)
	_, err = client.Query(context.Background(), sqlclient.Connection{Database: database}, text, args)
	require.NoError(t, err, "this engine must expose the live, undocumented COPY definition columns")
	t.Log("SYS_COPY_JOB exposes copy_query, data_source, table_id and job_owner; definition readback uses SELECT * and rejects missing/empty copy_query")
}

// TestAccCopyJobDefinition uses an already integrated bucket and attached IAM role; it provisions no AWS fixtures.
func TestAccCopyJobDefinition(t *testing.T) {
	region, profile, workgroup, database := testAccWorkgroup(t, "REDSHIFT_ACC_COPY_SOURCE", "REDSHIFT_ACC_COPY_ROLE")
	name := "acc_copy_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	source := strings.TrimRight(os.Getenv("REDSHIFT_ACC_COPY_SOURCE"), "/") + "/" + name + "/"
	configuration := func(source string, csv, auto bool) string {
		secret := ""
		if arn := os.Getenv("REDSHIFT_ACC_SECRET_ARN"); arn != "" {
			secret = fmt.Sprintf("secret_arn = %q", arn)
		}

		columns, options := "", ""
		if csv {
			columns = `columns = ["id"]`
			options = `format = "CSV"
  parameters = { IGNOREHEADER = "1" }`
		}

		return fmt.Sprintf(`
provider "redshift" {
  region = %q
  profile = %q
  workgroup_name = %q
  database = %q
  %s
}
resource "redshift_schema" "copy" {
  database = %q
  name = %q
}
resource "redshift_table" "copy" {
  database = redshift_schema.copy.database
  schema = redshift_schema.copy.name
  name = "events"
  column {
    name = "id"
    type = "INTEGER"
  }
}
resource "redshift_copy_job" "copy" {
  database = redshift_table.copy.database
  name = %q
  table {
    schema = redshift_table.copy.schema
    name = redshift_table.copy.name
    %s
  }
  source = %q
  iam_role = %q
  %s
  auto = %t
}
data "redshift_copy_job" "copy" {
  database = redshift_copy_job.copy.database
  name = redshift_copy_job.copy.name
}
`, region, profile, workgroup, database, secret, database, name, name, columns, source, os.Getenv("REDSHIFT_ACC_COPY_ROLE"), options, auto)
	}
	moved := strings.TrimRight(source, "/") + "/replacement/"
	checkScope := func(*terraform.State) error {
		other := os.Getenv("REDSHIFT_ACC_COPY_JOB_OTHER_DATABASE")
		if other == "" || other == database {
			return nil
		}

		client := copyJobAcceptanceClient(t, region, profile, workgroup)

		query, args, err := readCopyJobQuery(name).Build()
		if err != nil {
			return err
		}

		rows, err := client.Query(context.Background(), sqlclient.Connection{Database: database}, query, args)
		if err != nil {
			return err
		}

		if len(rows) != 1 {
			return fmt.Errorf("expected the created COPY job in %s, got %d rows", database, len(rows))
		}

		rows, err = client.Query(context.Background(), sqlclient.Connection{Database: other}, query, args)
		if err != nil {
			return err
		}

		if len(rows) != 0 {
			return fmt.Errorf("SYS_COPY_JOB returned %d rows for a job in another database", len(rows))
		}

		t.Logf("SYS_COPY_JOB is database-local: %s is visible in %s but not %s", name, database, other)

		return nil
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(New("test")())},
		Steps: []resource.TestStep{
			{Config: configuration(source, true, false), Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttr("data.redshift_copy_job.copy", "source", source),
				resource.TestCheckResourceAttr("data.redshift_copy_job.copy", "format", "CSV"),
				resource.TestCheckResourceAttr("data.redshift_copy_job.copy", "parameters.IGNOREHEADER", "1"),
				resource.TestCheckResourceAttr("data.redshift_copy_job.copy", "table.columns.0", "id"),
				checkScope,
			)},
			{ResourceName: "redshift_copy_job.copy", ImportState: true, ImportStateVerify: true},
			{Config: configuration(source, true, false), PlanOnly: true},
			{Config: configuration(moved, true, false), ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
				plancheck.ExpectResourceAction("redshift_copy_job.copy", plancheck.ResourceActionDestroyBeforeCreate),
			}}},
			{Config: configuration(moved, true, false), PlanOnly: true},
			{Config: configuration(moved, false, false)},
			{ResourceName: "redshift_copy_job.copy", ImportState: true, ImportStateVerify: true},
			{Config: configuration(moved, false, false), PlanOnly: true},
			{Config: configuration(moved, false, true), ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
				plancheck.ExpectResourceAction("redshift_copy_job.copy", plancheck.ResourceActionUpdate),
			}}, Check: resource.TestCheckResourceAttr("data.redshift_copy_job.copy", "auto", "true")},
			{Config: configuration(moved, false, false), Check: resource.TestCheckResourceAttr("data.redshift_copy_job.copy", "auto", "false")},
			{Config: configuration(moved, false, false), PlanOnly: true},
		},
	})
}
