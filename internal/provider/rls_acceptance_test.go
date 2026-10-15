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
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/redshiftdata"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/require"
)

// TestAccRlsLifecycle verifies policy, attachment, and table security creation, in-place predicate and setting
// changes, imports, predicate drift, a WITH change that replaces the attached policy, and that destroying table
// security turns row-level security off.
func TestAccRlsLifecycle(t *testing.T) {
	region, profile, workgroup, database := testAccWorkgroup(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	name, role := "acc_rls_"+suffix, "acc_rls_reader_"+suffix
	configuration := func(predicate, conjunction string, protected bool) string {
		base := fmt.Sprintf(`
provider "redshift" {
  region = %q
  profile = %q
  workgroup_name = %q
  database = %q
}
resource "redshift_database" "local" { name = %q }
resource "redshift_role" "reader" { name = %q }
resource "redshift_rls_policy" "own_region" {
  database = redshift_database.local.name
  name = "own_region"
  alias = "t"
  predicate = %q
  column {
    name = "region"
    type = "VARCHAR(64)"
  }
}
resource "redshift_rls_policy_attachment" "reader" {
  policy = redshift_rls_policy.own_region.name
  database = redshift_rls_policy.own_region.database
  schema = "public"
  relation = "fixture"
  grantee = redshift_role.reader.name
  grantee_type = "ROLE"
  lifecycle {
    replace_triggered_by = [redshift_rls_policy.own_region.column, redshift_rls_policy.own_region.alias]
  }
}
data "redshift_rls_policies" "local" { database = redshift_database.local.name }
`, region, profile, workgroup, database, name, role, predicate)
		if !protected {
			return base
		}
		return base + fmt.Sprintf(`
resource "redshift_table_security" "fixture" {
  database = redshift_rls_policy_attachment.reader.database
  schema = redshift_rls_policy_attachment.reader.schema
  relation = redshift_rls_policy_attachment.reader.relation
  row_level_security = true
  conjunction_type = %q
}
data "redshift_table_security" "fixture" {
  database = redshift_table_security.fixture.database
  schema = redshift_table_security.fixture.schema
  relation = redshift_table_security.fixture.relation
}
data "redshift_rls_policy_attachment" "reader" {
  policy = redshift_rls_policy_attachment.reader.policy
  database = redshift_rls_policy_attachment.reader.database
  schema = redshift_rls_policy_attachment.reader.schema
  relation = redshift_rls_policy_attachment.reader.relation
  grantee = redshift_rls_policy_attachment.reader.grantee
  grantee_type = redshift_rls_policy_attachment.reader.grantee_type
}
data "redshift_rls_policy" "own_region" {
  database = redshift_rls_policy.own_region.database
  name = redshift_rls_policy.own_region.name
}
`, conjunction)
	}
	// respelled and widened rewrite the WITH clause: the first only respells it, the second changes it.
	respelled := func(configuration string) string {
		return strings.Replace(configuration, `type = "VARCHAR(64)"`, `type = "character varying(64)"`, 1)
	}
	widened := func(configuration string) string {
		return strings.Replace(configuration, `type = "VARCHAR(64)"
  }`, `type = "VARCHAR(64)"
  }
  column {
    name = "id"
    type = "INTEGER"
  }`, 1)
	}
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region), config.WithSharedConfigProfile(profile))
	require.NoError(t, err)
	client := &dataapi.Client{API: redshiftdata.NewFromConfig(cfg), Workgroup: workgroup, Timeout: 5 * time.Minute, Poll: time.Second}
	admin, target := sqlclient.Connection{Database: database}, sqlclient.Connection{Database: name}
	// A failed run may leave the protected fixture behind; dropping the database removes it with its policies.
	t.Cleanup(func() {
		rows, err := client.Query(ctx, admin, "SELECT database_name FROM svv_redshift_databases WHERE database_name = :name", map[string]string{"name": name})
		require.NoError(t, err)
		if len(rows) != 0 {
			_, err = client.Query(ctx, admin, "DROP DATABASE "+sqlclient.Identifier(name), nil)
			require.NoError(t, err)
		}
	})
	rlsOn := func() (bool, error) {
		data := tableSecurityModel{Database: types.StringValue(name), Schema: types.StringValue("public"), Relation: types.StringValue("fixture")}
		r := &tableSecurityResource{resourceClient{client: client, warehouse: warehouseBinding{field: "workgroup_name", value: types.StringValue(workgroup)}, database: types.StringValue(database)}}
		found, err := r.read(ctx, &data)
		return found && data.RowLevelSecurity.ValueBool(), err
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(New("test")())},
		Steps: []resource.TestStep{
			{Config: `
provider "redshift" {
  region = "` + region + `"
  profile = "` + profile + `"
  workgroup_name = "` + workgroup + `"
  database = "` + database + `"
}
resource "redshift_database" "local" { name = "` + name + `" }`},
			{
				PreConfig: func() {
					_, err := client.Query(ctx, target, `CREATE TABLE public.fixture (id INTEGER, region VARCHAR(64))`, nil)
					require.NoError(t, err)
				},
				Config: configuration("t.region = current_user", "AND", true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.redshift_table_security.fixture", "row_level_security", "true"),
					resource.TestCheckResourceAttr("data.redshift_table_security.fixture", "conjunction_type", "AND"),
					resource.TestCheckResourceAttr("data.redshift_rls_policy_attachment.reader", "exists", "true"),
					resource.TestCheckResourceAttr("data.redshift_rls_policy.own_region", "column.0.type", "CHARACTER VARYING(64)"),
					resource.TestCheckResourceAttrSet("redshift_rls_policy.own_region", "definition_fingerprint"),
				),
			},
			{Config: configuration("t.region = current_user", "AND", true), PlanOnly: true},
			{Config: respelled(configuration("t.region = current_user", "AND", true)), PlanOnly: true},
			{ResourceName: "redshift_rls_policy_attachment.reader", ImportState: true, ImportStateVerify: true},
			{ResourceName: "redshift_table_security.fixture", ImportState: true, ImportStateVerify: true},
			// The imported predicate is the catalog's rewritten text and the imported column blocks use the catalog spelling,
			// which plans no change (see the respelled step), so only the identity attributes are compared.
			{ResourceName: "redshift_rls_policy.own_region", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"predicate", "column"}},
			{
				Config: configuration("t.region = current_user OR t.region IS NULL", "OR", true),
				Check:  resource.TestCheckResourceAttr("data.redshift_table_security.fixture", "conjunction_type", "OR"),
			},
			{
				Config: configuration("t.region = current_user OR t.region IS NULL", "OR", true), PlanOnly: true, ExpectNonEmptyPlan: true,
				PreConfig: func() {
					_, err := client.Query(ctx, target, `ALTER RLS POLICY own_region USING (t.region = 'drift')`, nil)
					require.NoError(t, err)
				},
			},
			{Config: configuration("t.region = current_user OR t.region IS NULL", "OR", true)},
			// DROP RLS POLICY is RESTRICT, so the replacement succeeds only because replace_triggered_by detaches first.
			{
				Config: widened(configuration("t.region = current_user OR t.region IS NULL", "OR", true)),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("redshift_rls_policy.own_region", plancheck.ResourceActionDestroyBeforeCreate),
					plancheck.ExpectResourceAction("redshift_rls_policy_attachment.reader", plancheck.ResourceActionDestroyBeforeCreate),
				}},
				Check: resource.TestCheckResourceAttr("data.redshift_rls_policy_attachment.reader", "exists", "true"),
			},
			{
				Config: widened(configuration("t.region = current_user OR t.region IS NULL", "OR", false)),
				Check: func(*terraform.State) error {
					on, err := rlsOn()
					if err == nil && on {
						err = fmt.Errorf("row-level security is still on after destroying redshift_table_security")
					}
					return err
				},
			},
		},
	})
}
