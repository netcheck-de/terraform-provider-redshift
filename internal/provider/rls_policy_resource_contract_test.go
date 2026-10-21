package provider

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	testresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// TestContractRlsPolicyReplacementDetachesAttachments runs Terraform against the fake catalog: importing a policy and
// respelling its WITH clause plan nothing, and a real WITH change replaces the policy only after
// replace_triggered_by detached its attachment, since DROP RLS POLICY is RESTRICT.
func TestContractRlsPolicyReplacementDetachesAttachments(t *testing.T) {
	t.Parallel()
	c := &catalog{identity: true, privileges: map[string]bool{}}
	// The policy exists before Terraform imports it, with the catalog spelling of its WITH clause.
	existing := rlsFakeOf(c)
	existing.populate()
	existing.attached, existing.alias, existing.predicate = false, "t", "t.region = current_user"
	configuration := func(columns, alias string) string {
		return fmt.Sprintf(`
provider "redshift" {
  region = "eu-central-1"
  workgroup_name = "warehouse"
  database = "admin"
}
import {
  to = redshift_rls_policy.own_region
  id = jsonencode({ workgroup_name = "warehouse", database = "admin", name = "region_filter" })
}
resource "redshift_rls_policy" "own_region" {
  database = "admin"
  name = "region_filter"
  alias = %[2]q
  predicate = "t.region = current_user"
  %[1]s
}
resource "redshift_rls_policy_attachment" "everyone" {
  policy = redshift_rls_policy.own_region.name
  database = redshift_rls_policy.own_region.database
  schema = "public"
  relation = "events"
  grantee = "public"
  grantee_type = "PUBLIC"
  lifecycle {
    replace_triggered_by = [redshift_rls_policy.own_region.column, redshift_rls_policy.own_region.alias]
  }
}`, columns, alias)
	}
	region := `column {
    name = "region"
    type = "VARCHAR(64)"
  }`
	added := `
  column {
    name = "id"
    type = "INTEGER"
  }`
	testresource.UnitTest(t, testresource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(&redshiftProvider{version: "test", client: c})},
		Steps: []testresource.TestStep{
			{Config: configuration(region, "t"), ConfigPlanChecks: testresource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
				plancheck.ExpectResourceAction("redshift_rls_policy.own_region", plancheck.ResourceActionNoop),
				plancheck.ExpectResourceAction("redshift_rls_policy_attachment.everyone", plancheck.ResourceActionCreate),
			}}},
			{Config: configuration(region, "t"), PlanOnly: true},
			{Config: configuration(`column {
    name = "REGION"
    type = "varchar(64)"
  }`, "T"), PlanOnly: true},
			{
				Config: configuration(region+added, "t"),
				ConfigPlanChecks: testresource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("redshift_rls_policy.own_region", plancheck.ResourceActionDestroyBeforeCreate),
					plancheck.ExpectResourceAction("redshift_rls_policy_attachment.everyone", plancheck.ResourceActionDestroyBeforeCreate),
				}},
				Check: func(*terraform.State) error {
					if f := rlsFakeOf(c); !f.policy || !f.attached || !strings.Contains(f.columns, `"id"`) {
						return fmt.Errorf("the replaced policy is not attached with the new WITH clause: %+v", *f)
					}
					return nil
				},
			},
			{Config: configuration(region+added, "t"), PlanOnly: true},
		},
	})
}

// TestContractRlsPolicyColumnBlocksConverge runs Terraform against the fake catalog: a policy created with or without column
// blocks plans no change afterwards, so absent blocks and a catalog without WITH columns both read as no blocks, and
// adding or removing the blocks replaces the policy.
func TestContractRlsPolicyColumnBlocksConverge(t *testing.T) {
	t.Parallel()
	c := &catalog{identity: true, privileges: map[string]bool{}}
	configuration := func(columns string) string {
		return fmt.Sprintf(`
provider "redshift" {
  region = "eu-central-1"
  workgroup_name = "warehouse"
  database = "admin"
}
resource "redshift_rls_policy" "auditor" {
  database = "admin"
  name = "region_filter"
  predicate = "current_user = 'auditor'"
  %s
}`, columns)
	}
	columns := `column {
    name = "region"
    type = "VARCHAR(64)"
  }
  column {
    name = "tenant_id"
    type = "INTEGER"
  }`
	replaced := testresource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
		plancheck.ExpectResourceAction("redshift_rls_policy.auditor", plancheck.ResourceActionDestroyBeforeCreate),
	}}
	testresource.UnitTest(t, testresource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(&redshiftProvider{version: "test", client: c})},
		Steps: []testresource.TestStep{
			{Config: configuration(""), Check: testresource.TestCheckResourceAttr("redshift_rls_policy.auditor", "column.#", "0")},
			{Config: configuration(""), PlanOnly: true},
			{
				Config: configuration(columns), ConfigPlanChecks: replaced,
				Check: testresource.ComposeAggregateTestCheckFunc(
					testresource.TestCheckResourceAttr("redshift_rls_policy.auditor", "column.1.name", "tenant_id"),
					func(*terraform.State) error {
						if f := rlsFakeOf(c); !strings.Contains(f.columns, `"tenant_id"`) {
							return fmt.Errorf("the catalog lacks the configured WITH columns: %+v", *f)
						}
						return nil
					},
				),
			},
			{Config: configuration(columns), PlanOnly: true},
			{Config: configuration(""), ConfigPlanChecks: replaced},
			{Config: configuration(""), PlanOnly: true},
		},
	})
}
