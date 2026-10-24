package provider

import (
	"context"
	"fmt"
	"os"
	"regexp"
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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// regressionFixture owns one disposable database for a live regression check and drops it after the test.
type regressionFixture struct {
	// client runs fixture SQL through the Data API as the acceptance identity.
	client *dataapi.Client
	// admin is the administration database connection.
	admin sqlclient.Connection
	// target is the disposable database connection.
	target sqlclient.Connection
	// name is the disposable database name, also used as a unique object prefix.
	name string
	// provider is the provider block for the acceptance warehouse.
	provider string
}

// newRegressionFixture prepares a uniquely named database and registers its cleanup before any mutation.
func newRegressionFixture(t *testing.T, prefix string, flags ...string) regressionFixture {
	t.Helper()
	region, profile, workgroup, database := testAccWorkgroup(t, flags...)
	cfg, err := config.LoadDefaultConfig(t.Context(), config.WithRegion(region), config.WithSharedConfigProfile(profile))
	require.NoError(t, err)

	name := prefix + strconv.FormatInt(time.Now().UnixNano(), 36)
	f := regressionFixture{
		client: &dataapi.Client{API: redshiftdata.NewFromConfig(cfg), Workgroup: workgroup, Timeout: 5 * time.Minute, Poll: time.Second},
		admin:  sqlclient.Connection{Database: database}, target: sqlclient.Connection{Database: name}, name: name,
		provider: fmt.Sprintf(`
provider "redshift" {
  region = %q
  profile = %q
  workgroup_name = %q
  database = %q
}
`, region, profile, workgroup, database),
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		rows, err := f.client.Query(ctx, f.admin, "SELECT database_name FROM svv_redshift_databases WHERE database_name = :name", map[string]string{"name": name})
		require.NoError(t, err)

		if len(rows) != 0 {
			_, err = f.client.Query(ctx, f.admin, sqlclient.Stmt("DROP DATABASE").Ident(name).String(), nil)
			assert.NoError(t, err, "clean up regression database")
		}
	})

	return f
}

// exec runs fixture SQL in the disposable database and fails the test on error.
func (f regressionFixture) exec(t *testing.T, statement string) {
	t.Helper()

	_, err := f.client.Query(context.Background(), f.target, statement, nil)
	require.NoError(t, err, statement)
}

// rows runs a catalog read in the disposable database.
func (f regressionFixture) rows(statement string, parameters map[string]string) ([]sqlclient.Row, error) {
	return f.client.Query(context.Background(), f.target, statement, parameters)
}

// regressionFactories serves the provider under test.
var regressionFactories = map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(New("test")())}

// TestAccViewDetectsQuotedWhitespaceDrift detects an outside change that only alters whitespace inside a literal,
// which the earlier fingerprint ignored.
func TestAccViewDetectsQuotedWhitespaceDrift(t *testing.T) {
	f := newRegressionFixture(t, "acc_fingerprint_")
	configuration := f.provider + fmt.Sprintf(`
resource "redshift_database" "local" { name = %q }
resource "redshift_schema" "local" {
  database = redshift_database.local.name
  name = "serving"
}
resource "redshift_view" "label" {
  database = redshift_schema.local.database
  schema = redshift_schema.local.name
  name = "label"
  query = "SELECT CAST('a  b' AS VARCHAR(8)) AS label"
}
`, f.name)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: regressionFactories,
		Steps: []resource.TestStep{
			{Config: configuration},
			{Config: configuration, PlanOnly: true},
			{
				Config: configuration, PlanOnly: true, ExpectNonEmptyPlan: true,
				PreConfig: func() { f.exec(t, `CREATE OR REPLACE VIEW serving.label AS SELECT CAST('a b' AS VARCHAR(8)) AS label`) },
			},
			{Config: configuration},
			{Config: configuration, PlanOnly: true},
		},
	})
}

// TestAccObjectGrantSnapshotRevokesPartialGrants creates tables in parallel with an ALL TABLES grant, then adds an
// ungranted table outside Terraform and destroys the grant: no table may keep the privilege.
func TestAccObjectGrantSnapshotRevokesPartialGrants(t *testing.T) {
	f := newRegressionFixture(t, "acc_snapshot_")
	base := f.provider + fmt.Sprintf(`
resource "redshift_database" "local" { name = %q }
resource "redshift_schema" "local" {
  database = redshift_database.local.name
  name = "serving"
}
resource "redshift_role" "reader" { name = %q }
`, f.name, f.name+"_reader")

	var tables string
	for i := range 4 {
		tables += fmt.Sprintf(`
resource "redshift_table" "t%[1]d" {
  database = redshift_schema.local.database
  schema = redshift_schema.local.name
  name = "t%[1]d"
  column {
    name = "id"
    type = "INTEGER"
  }
}
`, i)
	}

	grant := `
resource "redshift_object_grant" "all_tables" {
  database_name = redshift_schema.local.database
  schema_name = redshift_schema.local.name
  object_type = "ALL TABLES"
  grantee = redshift_role.reader.name
  grantee_type = "ROLE"
  privileges = ["SELECT"]
}
`
	held := func(want int) resource.TestCheckFunc {
		return func(*terraform.State) error {
			rows, err := f.rows("SELECT relation_name FROM svv_relation_privileges WHERE namespace_name = 'serving' AND identity_name = :role AND privilege_type = 'SELECT'", map[string]string{"role": f.name + "_reader"})
			if err != nil {
				return err
			}

			if len(rows) != want {
				return fmt.Errorf("expected SELECT on %d tables, got %v", want, rows)
			}

			return nil
		}
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: regressionFactories,
		Steps: []resource.TestStep{
			// The grant does not depend on the tables, so Terraform creates them in parallel with it.
			{Config: base + tables + grant},
			{Config: base + tables + grant, Check: held(4)},
			{
				Config: base + tables,
				PreConfig: func() {
					f.exec(t, `CREATE TABLE serving.outside (id INTEGER)`)
				},
				Check: resource.ComposeTestCheckFunc(held(0), func(*terraform.State) error {
					// The outside table would block the restrictive schema drop of the final destroy.
					_, err := f.rows(`DROP TABLE serving.outside`, nil)
					return err
				}),
			},
		},
	})
}

// TestAccUserCreateRecordsUserWhenDefaultsFail keeps a user whose stored default Redshift rejects in state, so the
// next apply replaces it instead of failing to create it again.
func TestAccUserCreateRecordsUserWhenDefaultsFail(t *testing.T) {
	f := newRegressionFixture(t, "acc_user_defaults_")
	configuration := func(timezone string) string {
		return f.provider + fmt.Sprintf(`
resource "redshift_user" "reader" {
  name = %q
  password_disabled = true
  session_defaults = { timezone = %q }
}
`, f.name, timezone)
	}

	t.Cleanup(func() {
		_, err := f.client.Query(context.Background(), f.admin, sqlclient.Stmt("DROP USER IF EXISTS").Ident(f.name).String(), nil)
		assert.NoError(t, err, "clean up regression user")
	})
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: regressionFactories,
		Steps: []resource.TestStep{
			{Config: configuration("Not/A_Zone"), ExpectError: regexp.MustCompile(`Configure Redshift user`)},
			{Config: configuration("UTC"), Check: resource.TestCheckResourceAttr("redshift_user.reader", "session_defaults.timezone", "UTC")},
			{Config: configuration("UTC"), PlanOnly: true},
		},
	})
}

// TestAccTemplateRenameKeepsStateOnFailure records a completed rename when a later statement of the update fails, so
// the next apply updates the renamed template instead of failing to create it.
func TestAccTemplateRenameKeepsStateOnFailure(t *testing.T) {
	f := newRegressionFixture(t, "acc_template_rename_")
	configuration := func(name, owner string) string {
		owned := ""
		if owner != "" {
			owned = fmt.Sprintf("owner = %q", owner)
		}

		return f.provider + fmt.Sprintf(`
resource "redshift_database" "local" { name = %q }
resource "redshift_schema" "local" {
  database = redshift_database.local.name
  name = "serving"
}
resource "redshift_template" "load" {
  database = redshift_schema.local.database
  schema = redshift_schema.local.name
  name = %q
  format = "CSV"
  %s
}
`, f.name, name, owned)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: regressionFactories,
		Steps: []resource.TestStep{
			{Config: configuration("csv_load", "")},
			{Config: configuration("csv_import", "acc_missing_owner_"+f.name), ExpectError: regexp.MustCompile(`Update template`)},
			{Config: configuration("csv_import", ""), Check: resource.TestCheckResourceAttr("redshift_template.load", "name", "csv_import")},
			{Config: configuration("csv_import", ""), PlanOnly: true},
		},
	})
}

// TestAccSharedDatabaseWithoutPermissions creates a consumer database from a datashare without WITH PERMISSIONS,
// imports it, and destroys it.
func TestAccSharedDatabaseWithoutPermissions(t *testing.T) {
	f := newRegressionFixture(t, "acc_shared_noperm_", "REDSHIFT_ACC_SHARED_DATASHARE_ARN", "REDSHIFT_ACC_SHARED_SCHEMA", "REDSHIFT_ACC_SHARED_TABLE")
	configuration := f.provider + fmt.Sprintf(`
resource "redshift_database" "shared" {
  name = %q
  datashare_arn = %q
  with_permissions = false
}
`, f.name, os.Getenv("REDSHIFT_ACC_SHARED_DATASHARE_ARN"))
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: regressionFactories,
		Steps: []resource.TestStep{
			{
				Config: configuration,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("redshift_database.shared", "with_permissions", "false"),
					func(*terraform.State) error {
						// Without WITH PERMISSIONS, a superuser reads shared objects without object-level grants.
						statement := sqlclient.Stmt("SELECT COUNT(*) FROM").Qualified(f.name, os.Getenv("REDSHIFT_ACC_SHARED_SCHEMA"), os.Getenv("REDSHIFT_ACC_SHARED_TABLE"))
						_, err := f.client.Query(context.Background(), f.admin, statement.String(), nil)

						return err
					},
				),
			},
			{Config: configuration, PlanOnly: true},
			{ResourceName: "redshift_database.shared", ImportState: true, ImportStateVerify: true},
		},
	})
}

// TestAccUppercaseNameRejected fails planning for an object name Redshift would fold to lowercase.
func TestAccUppercaseNameRejected(t *testing.T) {
	f := newRegressionFixture(t, "acc_upper_")
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: regressionFactories,
		Steps: []resource.TestStep{{
			Config: f.provider + `resource "redshift_schema" "mixed" {
  database = "dev"
  name = "Analytics"
}`,
			PlanOnly:    true,
			ExpectError: regexp.MustCompile(`uppercase letters`),
		}},
	})
}

// TestAccDataAPICancellationSettles cancels a long statement when its query timeout expires and confirms Redshift
// stopped it within the settle wait, so the mutation gate is released without an unsettled statement.
func TestAccDataAPICancellationSettles(t *testing.T) {
	f := newRegressionFixture(t, "acc_cancel_")
	client := *f.client
	client.Timeout = 5 * time.Second
	started := time.Now()
	_, err := client.Query(context.Background(), f.admin, "SELECT COUNT(*) FROM pg_catalog.pg_attribute a, pg_catalog.pg_attribute b, pg_catalog.pg_attribute c", nil)
	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.NotErrorIs(t, err, sqlclient.ErrUnsettled, "Redshift must report the cancelled statement stopped")
	assert.Less(t, time.Since(started), client.Timeout+settleWait, "settlement is bounded")
	rows, err := f.client.Query(context.Background(), f.admin, "SELECT 1 AS one", nil)
	require.NoError(t, err)
	assert.Equal(t, []sqlclient.Row{{"one": "1"}}, rows)
}

// settleWait mirrors the Data API client's bound on waiting for an abandoned statement, plus request latency.
const settleWait = 40 * time.Second

// TestAccDirectCatalogCodes reads one-letter catalog codes over a direct connection: a function's volatility and a
// table's primary key must converge and plan no change, which they did not when pgx returned the codes as numbers.
func TestAccDirectCatalogCodes(t *testing.T) {
	f := newRegressionFixture(t, "acc_direct_codes_", "REDSHIFT_ACC_DIRECT")
	region, profile, workgroup, database := testAccWorkgroup(t, "REDSHIFT_ACC_DIRECT")
	configuration := fmt.Sprintf(`
provider "redshift" {
  region = %q
  profile = %q
  database = %q
  direct_connection {
    %s iam { workgroup_name = %q }
  }
}
resource "redshift_database" "local" { name = %q }
resource "redshift_schema" "local" {
  database = redshift_database.local.name
  name = "serving"
}
resource "redshift_function" "stable" {
  database = redshift_schema.local.database
  schema = redshift_schema.local.name
  name = "f_stable"
  arguments = ["INTEGER"]
  return_type = "INTEGER"
  volatility = "STABLE"
  body = "SELECT $1"
}
resource "redshift_table" "keyed" {
  database = redshift_schema.local.database
  schema = redshift_schema.local.name
  name = "keyed"
  column {
    name = "id"
    type = "INTEGER"
    nullable = false
  }
  primary_key { columns = ["id"] }
}
`, region, profile, database, testAccSSLModeHCL(), workgroup, f.name)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: regressionFactories,
		Steps: []resource.TestStep{
			{Config: configuration, Check: resource.TestCheckResourceAttr("redshift_function.stable", "volatility", "STABLE")},
			{Config: configuration, PlanOnly: true},
			{ResourceName: "redshift_function.stable", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"body"}},
		},
	})
}
