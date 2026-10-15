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
	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/redshiftdata"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/require"
)

// TestAccMaskingLifecycle verifies a masking policy, its lookup grant, and its attachment against a live warehouse:
// creation, the catalog shapes the readers parse, priority and expression updates in place, imports, drift repair,
// and teardown in dependency order. The policy reads its lookup table, so masking succeeds only once the grant
// applied, even though no catalog reports the grant.
func TestAccMaskingLifecycle(t *testing.T) {
	region, profile, workgroup, database := testAccWorkgroup(t)
	name := "acc_masking_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	configuration := func(expression string, priority int) string {
		return fmt.Sprintf(`
provider "redshift" {
  region = %q
  profile = %q
  workgroup_name = %q
  database = %q
}
resource "redshift_database" "local" { name = %q }
resource "redshift_role" "readers" { name = "%[5]s_readers" }
resource "redshift_masking_policy" "email" {
  database = redshift_database.local.name
  name = "%[5]s_email"
  expression = %[6]q
  input_column {
    name = "email"
    type = "VARCHAR(256)"
  }
}
resource "redshift_policy_grant" "lookup" {
  database_name = redshift_database.local.name
  schema_name = "public"
  object_name = "fixture_exempt"
  policy_type = "MASKING"
  policy_name = redshift_masking_policy.email.name
  privileges = ["SELECT"]
}
resource "redshift_masking_policy_attachment" "email" {
  database = redshift_masking_policy.email.database
  policy = redshift_masking_policy.email.name
  schema = "public"
  relation = "fixture_customers"
  columns = ["email"]
  grantee = redshift_role.readers.name
  grantee_type = "ROLE"
  priority = %[7]d
  depends_on = [redshift_policy_grant.lookup]
}
data "redshift_masking_policy" "email" {
  database = redshift_masking_policy.email.database
  name = redshift_masking_policy.email.name
}
data "redshift_masking_policy_attachment" "email" {
  database = redshift_masking_policy_attachment.email.database
  policy = redshift_masking_policy_attachment.email.policy
  schema = redshift_masking_policy_attachment.email.schema
  relation = redshift_masking_policy_attachment.email.relation
  columns = redshift_masking_policy_attachment.email.columns
  grantee = redshift_masking_policy_attachment.email.grantee
  grantee_type = redshift_masking_policy_attachment.email.grantee_type
}
data "redshift_masking_policies" "local" {
  database = redshift_masking_policy.email.database
}
`, region, profile, workgroup, database, name, expression, priority)
	}
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region), config.WithSharedConfigProfile(profile))
	require.NoError(t, err)
	client := &dataapi.Client{API: redshiftdata.NewFromConfig(cfg), Workgroup: workgroup, Timeout: 5 * time.Minute, Poll: time.Second}
	admin, target := sqlclient.Connection{Database: database}, sqlclient.Connection{Database: name}
	// A failed run may leave the database; dropping it also removes its policies' attachments and fixture tables.
	t.Cleanup(func() {
		rows, err := client.Query(ctx, admin, "SELECT database_name FROM svv_redshift_databases WHERE database_name = :name", map[string]string{"name": name})
		require.NoError(t, err)
		if len(rows) != 0 {
			_, err = client.Query(ctx, admin, "DROP DATABASE "+sqlclient.Identifier(name), nil)
			require.NoError(t, err)
		}
	})
	initial := configuration("CASE WHEN email IN (SELECT email FROM public.fixture_exempt) THEN email ELSE '***'::VARCHAR(256) END", 10)
	updated := configuration("SHA2(email, 256)::VARCHAR(256)", 20)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(New("test")())},
		Steps: []resource.TestStep{
			{Config: fmt.Sprintf(`
provider "redshift" {
  region = %q
  profile = %q
  workgroup_name = %q
  database = %q
}
resource "redshift_database" "local" { name = %q }
`, region, profile, workgroup, database, name)},
			{Config: initial, PreConfig: func() {
				_, err := client.Query(ctx, target, `CREATE TABLE public.fixture_customers (id INTEGER, email VARCHAR(256))`, nil)
				require.NoError(t, err)
				_, err = client.Query(ctx, target, `CREATE TABLE public.fixture_exempt (email VARCHAR(256))`, nil)
				require.NoError(t, err)
			}, Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttr("data.redshift_masking_policy_attachment.email", "exists", "true"),
				resource.TestCheckResourceAttr("data.redshift_masking_policy_attachment.email", "priority", "10"),
				resource.TestCheckResourceAttr("data.redshift_masking_policy_attachment.email", "input_columns.0", "email"),
				resource.TestCheckResourceAttr("data.redshift_masking_policy.email", "input_column.0.type", "CHARACTER VARYING(256)"),
				resource.TestCheckResourceAttr("data.redshift_masking_policies.local", "masking_policies.#", "1"),
			)},
			{Config: initial, PlanOnly: true},
			{ResourceName: "redshift_masking_policy_attachment.email", ImportState: true, ImportStateVerify: true},
			// Import cannot read a grant to a policy, so it records no privileges.
			{ResourceName: "redshift_policy_grant.lookup", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"privileges"}},
			{ResourceName: "redshift_masking_policy.email", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"expression", "input_column"}},
			{Config: updated, Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttr("redshift_masking_policy_attachment.email", "priority", "20"),
				resource.TestCheckResourceAttr("data.redshift_masking_policy_attachment.email", "priority", "20"),
			)},
			{Config: updated, PlanOnly: true, ExpectNonEmptyPlan: true, PreConfig: func() {
				_, err := client.Query(ctx, target, `ALTER MASKING POLICY `+sqlclient.Identifier(name+"_email")+` USING ('drift'::VARCHAR(256))`, nil)
				require.NoError(t, err)
			}},
			{Config: updated},
			{Config: updated, PlanOnly: true},
		},
	})
}
