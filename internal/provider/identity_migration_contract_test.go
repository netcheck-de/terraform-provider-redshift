package provider

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	testresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

// legacyIdentityProvider reproduces the previous role ID format without requiring an installed historical binary.
type legacyIdentityProvider struct{ *redshiftProvider }

// Resources exposes only the role needed to write representative historical state.
func (p *legacyIdentityProvider) Resources(context.Context) []func() resource.Resource {
	return []func() resource.Resource{func() resource.Resource { return &legacyIdentityRole{} }}
}

// legacyIdentityRole retains legacy IDs through the old provider's refresh and create paths.
type legacyIdentityRole struct{ roleResource }

// legacyID recreates connection-bound state after the normal catalog observation.
func (r *legacyIdentityRole) legacyID(ctx context.Context, state *tfsdk.State) {
	var name types.String
	if state.GetAttribute(ctx, path.Root("name"), &name).HasError() || name.IsNull() {
		return
	}

	encoded, _ := json.Marshal(map[string]string{
		r.warehouse.field: r.warehouse.value.ValueString(), "database": r.database.ValueString(), "name": name.ValueString(),
	})
	state.SetAttribute(ctx, path.Root("id"), string(encoded))
}

// Create writes the old ID after creating the role using the normal SQL implementation.
func (r *legacyIdentityRole) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	r.roleResource.Create(ctx, req, resp)

	if !resp.Diagnostics.HasError() {
		r.legacyID(ctx, &resp.State)
	}
}

// Read prevents the old provider from normalizing its own state before the upgrade step.
func (r *legacyIdentityRole) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	r.roleResource.Read(ctx, req, resp)

	if !resp.Diagnostics.HasError() && !resp.State.Raw.IsNull() {
		r.legacyID(ctx, &resp.State)
	}
}

// TestContractIdentityUpgrade verifies that already-applied legacy import blocks do not prevent state normalization
// or switching connection methods, even though their IDs are no longer accepted for fresh imports.
func TestContractIdentityUpgrade(t *testing.T) {
	t.Parallel()

	c := &catalog{identity: true}
	factories := func(p provider.Provider) map[string]func() (tfprotov6.ProviderServer, error) {
		return map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(p)}
	}
	current := factories(&redshiftProvider{version: "test", client: c})
	legacy := factories(&legacyIdentityProvider{&redshiftProvider{version: "test", client: c}})
	config := `
provider "redshift" {
  region = "eu-central-1"
  workgroup_name = "warehouse"
  database = "admin"
}
resource "redshift_role" "readers" {
  name = "example:readers"
}
`
	legacyImport := `
import {
  to = redshift_role.readers
  id = jsonencode({ workgroup_name = "warehouse", database = "admin", name = "example:readers" })
}
`
	direct := strings.Replace(config, `workgroup_name = "warehouse"`, `direct_connection {
    host = "warehouse.example.com"
    username = "admin"
    password = "Secret123"
  }`, 1)
	direct = strings.Replace(direct, `database = "admin"`, `database = "another_admin"`, 1)
	testresource.UnitTest(t, testresource.TestCase{
		Steps: []testresource.TestStep{
			{Config: config, ProtoV6ProviderFactories: legacy, Check: testresource.TestCheckResourceAttr(
				"redshift_role.readers", "id", `{"database":"admin","name":"example:readers","workgroup_name":"warehouse"}`)},
			{
				Config: config + legacyImport, ProtoV6ProviderFactories: current,
				ConfigPlanChecks: testresource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
				Check:            testresource.TestCheckResourceAttr("redshift_role.readers", "id", `{"name":"example:readers"}`),
			},
			{
				Config: direct + legacyImport, ProtoV6ProviderFactories: current, ConfigPlanChecks: testresource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
				Check: testresource.TestCheckResourceAttr("redshift_role.readers", "id", `{"name":"example:readers"}`),
			},
		},
	})
}
