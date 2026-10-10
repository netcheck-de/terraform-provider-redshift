package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	testresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rlsPolicyLifecycleModel matches the policy the rls fake family holds.
func rlsPolicyLifecycleModel() rlsPolicyModel {
	return rlsPolicyModel{
		Database: types.StringValue("admin"), Name: types.StringValue("region_filter"), Columns: rlsPolicyColumnsValue("region", "VARCHAR(64)"),
		Alias: types.StringNull(), Predicate: types.StringValue(rlsFakePredicate), DefinitionFingerprint: types.StringValue(definitionFingerprint(rlsFakePredicate)),
	}
}

// rlsFakeOf returns the rls family state of a fake catalog.
func rlsFakeOf(c *catalog) *rlsFake {
	return c.family("rls").(*rlsFake)
}

var (
	_ = registerLifecycleCase(lifecycleCase{
		name: "rls_policy", new: newRlsPolicyResource, model: rlsPolicyLifecycleModel(),
		absent:     func(c *catalog) { rlsFakeOf(c).policy, rlsFakeOf(c).attached = false, false },
		dependents: func(c *catalog) { rlsFakeOf(c).attached = false },
	})
	_ = registerReplacementPolicy("redshift_rls_policy", map[string]replaceRule{
		"database":  replaceAlways,
		"name":      replaceAlways,
		"columns":   replaceAlways,
		"alias":     replaceAlways,
		"predicate": replaceNever,
	})
	_ = registerValidateConfigCase("rls_policy", validateConfigCase{
		new:   newRlsPolicyResource,
		valid: rlsPolicyLifecycleModel(),
		invalid: func() rlsPolicyModel {
			data := rlsPolicyLifecycleModel()
			data.Predicate = types.StringValue("true; DROP TABLE t")
			return data
		}(),
		unknown: func() rlsPolicyModel {
			data := rlsPolicyLifecycleModel()
			data.Predicate = types.StringUnknown()
			return data
		}(),
	})
)

// TestRlsPolicyAlterCoverage keeps an alter step for every in-place attribute.
func TestRlsPolicyAlterCoverage(t *testing.T) {
	assertAlterCoverage(t, newRlsPolicyResource(), rlsPolicyAlterSteps)
}

// rlsPolicyCatalog returns a fake catalog factory holding the representative policy with predicate.
func rlsPolicyCatalog(predicate string, changes ...func(*rlsFake)) func() sqlclient.Client {
	return catalogWith(func(c *catalog) {
		f := rlsFakeOf(c)
		f.predicate = predicate
		for _, change := range changes {
			change(f)
		}
	})
}

// TestRlsPolicyTranscripts records predicate changes and policy flows beyond the representative lifecycle case.
func TestRlsPolicyTranscripts(t *testing.T) {
	model := rlsPolicyLifecycleModel()
	changed := model
	changed.Predicate = types.StringValue("region = 'O''Brien' OR region IS NULL")
	changed.DefinitionFingerprint = types.StringUnknown()
	unattached := func(f *rlsFake) { f.attached = false }
	runTranscripts(t, "rls_transcripts/rls_policy", newRlsPolicyResource, []transcriptCase{
		{name: "update_predicate", operation: "update", catalog: rlsPolicyCatalog(rlsFakePredicate), prior: model, planned: changed},
		{name: "read_outside_change", operation: "read", catalog: rlsPolicyCatalog("region = 'eu'"), prior: model},
		{name: "delete_unattached", operation: "delete", catalog: rlsPolicyCatalog(rlsFakePredicate, unattached), prior: model},
	})
}

// rlsPolicyOperation runs one lifecycle RPC against client and decodes the resulting state.
func rlsPolicyOperation(t *testing.T, client sqlclient.Client, operation string, prior, planned any) (rlsPolicyModel, bool) {
	t.Helper()
	r := newRlsPolicyResource()
	configureTestResource(t, r, client)
	state, diagnostics := applyOperation(t, r, operation, prior, planned, nil)
	var data rlsPolicyModel
	if !state.Raw.IsNull() {
		require.False(t, state.Get(context.Background(), &data).HasError())
	}
	return data, diagnostics.HasError()
}

// TestRlsPolicyPredicateDrift keeps the configured predicate while the catalog matches the recorded fingerprint and
// surfaces the catalog text after an outside change.
func TestRlsPolicyPredicateDrift(t *testing.T) {
	c := fullCatalog()
	planned := rlsPolicyLifecycleModel()
	planned.Predicate, planned.DefinitionFingerprint = types.StringValue("region   =\ncurrent_user"), types.StringUnknown()
	rlsFakeOf(c).policy, rlsFakeOf(c).attached = false, false
	created, failed := rlsPolicyOperation(t, c, "create", nil, planned)
	require.False(t, failed)
	assert.Equal(t, planned.Predicate, created.Predicate, "state keeps the configured text")
	assert.Equal(t, definitionFingerprint(rlsFakePredicate), created.DefinitionFingerprint.ValueString(), "whitespace does not change the fingerprint")
	assertLookupIdentity(t, created.ID, "admin", map[string]string{"name": "region_filter"})

	refreshed, failed := rlsPolicyOperation(t, c, "read", created, nil)
	require.False(t, failed)
	assert.Equal(t, created.Predicate, refreshed.Predicate)

	rlsFakeOf(c).predicate = `"region" = CAST('eu' AS TEXT)`
	drifted, failed := rlsPolicyOperation(t, c, "read", refreshed, nil)
	require.False(t, failed)
	assert.Equal(t, `"region" = CAST('eu' AS TEXT)`, drifted.Predicate.ValueString(), "an outside change surfaces the catalog text")
	assert.Equal(t, definitionFingerprint(`"region" = CAST('eu' AS TEXT)`), drifted.DefinitionFingerprint.ValueString())

	repaired, failed := rlsPolicyOperation(t, c, "update", drifted, planned)
	require.False(t, failed)
	assert.Equal(t, planned.Predicate, repaired.Predicate)
	assert.Equal(t, "region   =\ncurrent_user", rlsFakeOf(c).predicate, "update re-applies the configured predicate")
	assert.Contains(t, c.writes[len(c.writes)-1], "ALTER RLS POLICY")
}

// TestRlsPolicyReadAdoptsCatalogShape keeps equivalent configured columns and alias and replaces differing ones.
func TestRlsPolicyReadAdoptsCatalogShape(t *testing.T) {
	row := sqlclient.Row{"poldb": "admin", "polname": "region_filter", "polalias": "t", "polatts": `[{"colname":"region","type":"character varying(64)"}]`, "polqual": `"t"."region" = current_user`}
	client := queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
		return []sqlclient.Row{row}, nil
	})
	r := &rlsPolicyResource{testResourceClient(client)}
	data := rlsPolicyLifecycleModel()
	data.Columns, data.Alias = rlsPolicyColumnsValue("REGION", "varchar(64)"), types.StringValue("T")
	found, predicate, err := r.read(context.Background(), &data)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, `"t"."region" = current_user`, predicate)
	assert.Equal(t, rlsPolicyColumnsValue("REGION", "varchar(64)"), data.Columns, "equivalent configuration keeps its spelling")
	assert.Equal(t, types.StringValue("T"), data.Alias)

	data.Columns, data.Alias = rlsPolicyColumnsValue("region", "integer"), types.StringNull()
	_, _, err = r.read(context.Background(), &data)
	require.NoError(t, err)
	assert.Equal(t, rlsPolicyColumnsValue("region", "character varying(64)"), data.Columns, "differing columns surface the catalog")
	assert.Equal(t, types.StringValue("t"), data.Alias)

	for _, rows := range [][]sqlclient.Row{
		{{"polname": ""}},
		{row, row},
		{{"poldb": "admin", "polname": "region_filter", "polatts": "not json"}},
	} {
		r := &rlsPolicyResource{testResourceClient(queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
			return rows, nil
		}))}
		data := rlsPolicyLifecycleModel()
		_, _, err := r.read(context.Background(), &data)
		require.Error(t, err)
	}
}

// TestRlsPolicyCreateVerification fails Create when the catalog does not hold the planned shape, while keeping a
// serializable state with the identity of the created policy.
func TestRlsPolicyCreateVerification(t *testing.T) {
	for name, row := range map[string]sqlclient.Row{
		"columns": {"poldb": "admin", "polname": "region_filter", "polalias": "", "polatts": `[{"colname":"region","type":"integer"}]`, "polqual": rlsFakePredicate},
		"alias":   {"poldb": "admin", "polname": "region_filter", "polalias": "other", "polatts": rlsFakeColumns, "polqual": rlsFakePredicate},
	} {
		t.Run(name, func(t *testing.T) {
			client := queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
				if strings.HasPrefix(sql, "SELECT") {
					return []sqlclient.Row{row}, nil
				}
				return nil, nil
			})
			planned := rlsPolicyLifecycleModel()
			planned.Alias, planned.DefinitionFingerprint = types.StringValue("t"), types.StringUnknown()
			if name == "columns" {
				planned.Alias = types.StringUnknown()
			}
			r := newRlsPolicyResource()
			configureTestResource(t, r, client)
			state, diagnostics := applyOperation(t, r, "create", nil, planned, nil)
			require.True(t, diagnostics.HasError())
			assert.Contains(t, diagnostics.Errors()[0].Detail(), name)
			var data rlsPolicyModel
			require.False(t, state.Get(context.Background(), &data).HasError())
			assert.False(t, data.ID.IsNull())
			assert.True(t, state.Raw.IsFullyKnown())
		})
	}
	// Unset alias accepts whatever alias Redshift reports.
	client := queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
		if strings.HasPrefix(sql, "SELECT") {
			return []sqlclient.Row{{"poldb": "admin", "polname": "region_filter", "polalias": "t", "polatts": rlsFakeColumns, "polqual": rlsFakePredicate}}, nil
		}
		return nil, nil
	})
	planned := rlsPolicyLifecycleModel()
	planned.Alias = types.StringUnknown()
	created, failed := rlsPolicyOperation(t, client, "create", nil, planned)
	require.False(t, failed)
	assert.Equal(t, types.StringValue("t"), created.Alias)
}

// TestRlsPolicyRejectsInvalidPlansBeforeSQL keeps invalid definitions out of SQL and state.
func TestRlsPolicyRejectsInvalidPlansBeforeSQL(t *testing.T) {
	client := queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
		return nil, errors.New("no SQL expected")
	})
	invalid := rlsPolicyLifecycleModel()
	invalid.Predicate = types.StringValue("region = 'eu")
	r := newRlsPolicyResource()
	configureTestResource(t, r, client)
	state, diagnostics := applyOperation(t, r, "create", nil, invalid, nil)
	require.True(t, diagnostics.HasError())
	assert.True(t, state.Raw.IsNull(), "nothing is stored before validation passes")
	_, diagnostics = applyOperation(t, r, "update", rlsPolicyLifecycleModel(), invalid, nil)
	require.True(t, diagnostics.HasError())
}

// TestRlsPolicyImport restores the identity and reads the definition from the catalog.
func TestRlsPolicyImport(t *testing.T) {
	c := fullCatalog()
	r := newRlsPolicyResource()
	configureTestResource(t, r, c)
	ctx := context.Background()
	imported := resource.ImportStateResponse{State: emptyState(t, r)}
	r.(resource.ResourceWithImportState).ImportState(ctx, resource.ImportStateRequest{ID: `{"workgroup_name":"warehouse","database":"admin","name":"region_filter"}`}, &imported)
	require.False(t, imported.Diagnostics.HasError(), "%v", imported.Diagnostics)
	resp := resource.ReadResponse{State: imported.State}
	r.Read(ctx, resource.ReadRequest{State: imported.State}, &resp)
	require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
	var data rlsPolicyModel
	require.False(t, resp.State.Get(ctx, &data).HasError())
	assert.Equal(t, rlsFakePredicate, data.Predicate.ValueString())
	assert.Equal(t, rlsPolicyColumnsValue("region", "character varying(64)"), data.Columns)
	assert.True(t, data.Alias.IsNull())
	assert.Equal(t, definitionFingerprint(rlsFakePredicate), data.DefinitionFingerprint.ValueString())
	assert.Empty(t, c.writes)
	missing := resource.ImportStateResponse{State: tfsdk.State{Schema: imported.State.Schema}}
	r.(resource.ResourceWithImportState).ImportState(ctx, resource.ImportStateRequest{ID: `{"workgroup_name":"warehouse","database":"admin"}`}, &missing)
	assert.True(t, missing.Diagnostics.HasError())
}

// TestRlsPolicyDeleteKeepsAttachedPolicy surfaces the RESTRICT failure instead of detaching on the user's behalf.
func TestRlsPolicyDeleteKeepsAttachedPolicy(t *testing.T) {
	c := fullCatalog()
	_, failed := rlsPolicyOperation(t, c, "delete", rlsPolicyLifecycleModel(), nil)
	assert.True(t, failed)
	assert.True(t, rlsFakeOf(c).policy)
	assert.Empty(t, c.writes)
}

// rlsPolicyPlanAttribute runs one attribute's plan modifiers in schema order, carrying each planned value into the
// next modifier as the framework does, starting from the initial plan, and returns the final plan and whether any modifier requires replacement.
func rlsPolicyPlanAttribute(t *testing.T, name string, prior, configured, initial attr.Value) (attr.Value, bool) {
	t.Helper()
	ctx := context.Background()
	var response resource.SchemaResponse
	newRlsPolicyResource().Schema(ctx, resource.SchemaRequest{}, &response)
	existing := tftypes.NewValue(tftypes.Object{}, map[string]tftypes.Value{})
	state, plan, config := tfsdk.State{Raw: existing}, tfsdk.Plan{Raw: existing}, tfsdk.Config{Raw: existing}
	replace := false
	switch attribute := response.Schema.Attributes[name].(type) {
	case schema.ListNestedAttribute:
		planned := initial.(types.List)
		for _, modifier := range attribute.PlanModifiers {
			request := planmodifier.ListRequest{Path: path.Root(name), State: state, Plan: plan, Config: config, StateValue: prior.(types.List), ConfigValue: configured.(types.List), PlanValue: planned}
			result := planmodifier.ListResponse{PlanValue: planned}
			modifier.PlanModifyList(ctx, request, &result)
			require.False(t, result.Diagnostics.HasError(), "%v", result.Diagnostics)
			planned, replace = result.PlanValue, replace || result.RequiresReplace
		}
		return planned, replace
	case schema.StringAttribute:
		planned := initial.(types.String)
		for _, modifier := range attribute.PlanModifiers {
			request := planmodifier.StringRequest{Path: path.Root(name), State: state, Plan: plan, Config: config, StateValue: prior.(types.String), ConfigValue: configured.(types.String), PlanValue: planned}
			result := planmodifier.StringResponse{PlanValue: planned}
			modifier.PlanModifyString(ctx, request, &result)
			require.False(t, result.Diagnostics.HasError(), "%v", result.Diagnostics)
			planned, replace = result.PlanValue, replace || result.RequiresReplace
		}
		return planned, replace
	default:
		t.Fatalf("no plan helper for attribute %s", name)
		return nil, false
	}
}

// TestRlsPolicyEquivalentSpellingPlansNoReplacement keeps an imported or respelled WITH clause from planning a
// replacement, which DROP RLS POLICY would refuse while the policy is attached, and still replaces on real changes.
func TestRlsPolicyEquivalentSpellingPlansNoReplacement(t *testing.T) {
	imported := rlsPolicyColumnsValue("region", "character varying(64)")
	for name, test := range map[string]struct {
		prior, configured types.List
		replace           bool
	}{
		"imported catalog spelling": {prior: imported, configured: rlsPolicyColumnsValue("region", "VARCHAR(64)")},
		"respelled type and name":   {prior: rlsPolicyColumnsValue("region", "VARCHAR(64)"), configured: rlsPolicyColumnsValue("REGION", "varchar(64)")},
		"changed length":            {prior: imported, configured: rlsPolicyColumnsValue("region", "VARCHAR(128)"), replace: true},
		"added column":              {prior: imported, configured: rlsPolicyColumnsValue("region", "VARCHAR(64)", "id", "INTEGER"), replace: true},
		"renamed column":            {prior: imported, configured: rlsPolicyColumnsValue("area", "VARCHAR(64)"), replace: true},
		"invalid type":              {prior: imported, configured: rlsPolicyColumnsValue("region", "VARCHAR(64"), replace: true},
		"removed clause":            {prior: imported, configured: types.ListNull(rlsPolicyColumnType), replace: true},
		"unknown":                   {prior: imported, configured: types.ListUnknown(rlsPolicyColumnType), replace: true},
	} {
		t.Run(name, func(t *testing.T) {
			planned, replace := rlsPolicyPlanAttribute(t, "columns", test.prior, test.configured, test.configured)
			assert.Equal(t, test.replace, replace)
			if test.replace {
				assert.Equal(t, test.configured, planned, "a real change plans the configuration")
			} else {
				assert.Equal(t, test.prior, planned, "an equivalent configuration keeps the prior value")
			}
		})
	}
	for name, test := range map[string]struct {
		prior, configured types.String
		planned           types.String
		replace           bool
	}{
		"case only":     {prior: types.StringValue("t"), configured: types.StringValue("T"), planned: types.StringValue("t")},
		"unset":         {prior: types.StringValue("t"), configured: types.StringNull(), planned: types.StringValue("t")},
		"changed alias": {prior: types.StringValue("t"), configured: types.StringValue("r"), planned: types.StringValue("r"), replace: true},
	} {
		t.Run("alias "+name, func(t *testing.T) {
			initial := test.configured
			if initial.IsNull() {
				// The framework marks an unset Optional+Computed value unknown before modifiers run.
				initial = types.StringUnknown()
			}
			result, replace := rlsPolicyPlanAttribute(t, "alias", test.prior, test.configured, initial)
			assert.Equal(t, test.replace, replace)
			assert.Equal(t, test.planned, result)
		})
	}
}

// TestRlsPolicyReplacementDetachesAttachments runs Terraform against the fake catalog: importing a policy and
// respelling its WITH clause plan nothing, and a real WITH change replaces the policy only after
// replace_triggered_by detached its attachment, since DROP RLS POLICY is RESTRICT.
func TestRlsPolicyReplacementDetachesAttachments(t *testing.T) {
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
  columns = [%s]
  alias = %q
  predicate = "t.region = current_user"
}
resource "redshift_rls_policy_attachment" "everyone" {
  policy = redshift_rls_policy.own_region.name
  database = redshift_rls_policy.own_region.database
  schema = "public"
  relation = "events"
  grantee = "public"
  grantee_type = "PUBLIC"
  lifecycle {
    replace_triggered_by = [redshift_rls_policy.own_region.columns, redshift_rls_policy.own_region.alias]
  }
}`, columns, alias)
	}
	region := `{ name = "region", type = "VARCHAR(64)" }`
	testresource.UnitTest(t, testresource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(&redshiftProvider{version: "test", client: c})},
		Steps: []testresource.TestStep{
			{Config: configuration(region, "t"), ConfigPlanChecks: testresource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
				plancheck.ExpectResourceAction("redshift_rls_policy.own_region", plancheck.ResourceActionNoop),
				plancheck.ExpectResourceAction("redshift_rls_policy_attachment.everyone", plancheck.ResourceActionCreate),
			}}},
			{Config: configuration(region, "t"), PlanOnly: true},
			{Config: configuration(`{ name = "REGION", type = "varchar(64)" }`, "T"), PlanOnly: true},
			{
				Config: configuration(region+`, { name = "id", type = "INTEGER" }`, "t"),
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
			{Config: configuration(region+`, { name = "id", type = "INTEGER" }`, "t"), PlanOnly: true},
		},
	})
}
