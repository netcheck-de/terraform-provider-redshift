package provider

import (
	"context"
	"slices"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// alterCoverageGaps compares the alter steps with the in-place inputs of a resource schema. missing lists in-place
// inputs that neither a step nor exempt covers; unexpected lists steps and exemptions that name a replacing,
// computed-only or unknown attribute, or that are listed twice.
func alterCoverageGaps[M any](t *testing.T, r resource.Resource, steps []alterStep[M], exempt ...string) (missing, unexpected []string) {
	t.Helper()
	var metadata resource.MetadataResponse
	r.Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "redshift"}, &metadata)
	var response resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &response)
	require.False(t, response.Diagnostics.HasError(), "%v", response.Diagnostics)
	inPlace := inPlaceInputs(t, response.Schema, replacementPolicies.entries[metadata.TypeName])
	covered := map[string]bool{}
	claims := slices.Clone(exempt)
	for _, step := range steps {
		claims = append(claims, step.attribute)
	}
	for _, name := range claims {
		if !inPlace[name] || covered[name] {
			unexpected = append(unexpected, name)
		}
		covered[name] = true
	}
	for name, updates := range inPlace {
		if updates && !covered[name] {
			missing = append(missing, name)
		}
	}
	slices.Sort(missing)
	slices.Sort(unexpected)
	return missing, unexpected
}

// inPlaceInputs maps each input attribute and block, except the timeouts block, to whether an update can change it
// without replacement. The registered replacement policy decides, because a conditional attribute such as a widening
// column type is updated in place for some changes, which one generic sample cannot show. Inputs the policy does not
// name are sampled.
func inPlaceInputs(t *testing.T, s schema.Schema, policy map[string]replaceRule) map[string]bool {
	t.Helper()
	inPlace := map[string]bool{}
	classify := func(name string, sample func() bool) {
		if rule, ok := policy[name]; ok {
			inPlace[name] = rule.kind != replaceKindAlways
		} else {
			inPlace[name] = !sample()
		}
	}
	for name, attribute := range s.Attributes {
		if attribute.IsRequired() || attribute.IsOptional() {
			classify(name, func() bool { return attributeReplacement(t, attribute, true, true) })
		}
	}
	for name, block := range s.Blocks {
		// A timeouts change only updates state and runs no SQL, so it needs no alter step.
		if name != timeoutsBlockName {
			classify(name, func() bool { return blockReplacement(t, block, true, true) })
		}
	}
	return inPlace
}

// assertAlterCoverage checks that every input the schema updates in place has exactly one alter step, unless the
// resource changes it outside alterStatements and lists it in exempt, such as a write-only password.
// It catches a new in-place attribute that Update would silently ignore, and a step left behind after an
// attribute starts to require replacement.
func assertAlterCoverage[M any](t *testing.T, r resource.Resource, steps []alterStep[M], exempt ...string) {
	t.Helper()
	missing, unexpected := alterCoverageGaps(t, r, steps, exempt...)
	assert.Empty(t, missing, "in-place attributes without an alter step")
	assert.Empty(t, unexpected, "alter steps or exemptions for attributes that are not updated in place, or listed twice")
}

// alterTestModel is a minimal model for exercising alterStatements.
type alterTestModel struct {
	// Name is part of every rendered statement.
	Name types.String
	// Limit is an optional integer option.
	Limit types.Int64
	// Superuser is a boolean capability.
	Superuser types.Bool
	// Timeout is skipped when the prior state predates it.
	Timeout types.Int64
}

// alterTestSteps renders one ALTER USER statement per changed option.
func alterTestSteps() []alterStep[alterTestModel] {
	prefix := func(m alterTestModel) sqlclient.Statement {
		return sqlclient.Stmt("ALTER USER").Ident(m.Name.ValueString())
	}
	return []alterStep[alterTestModel]{
		{
			attribute: "connection_limit",
			value:     func(m alterTestModel) attr.Value { return m.Limit },
			render: func(_, plan alterTestModel) []string {
				if plan.Limit.IsNull() {
					return []string{prefix(plan).Kw("CONNECTION LIMIT UNLIMITED").String()}
				}
				return []string{prefix(plan).KwInt("CONNECTION LIMIT", plan.Limit.ValueInt64()).String()}
			},
		},
		{
			attribute: "superuser",
			value:     func(m alterTestModel) attr.Value { return m.Superuser },
			render: func(_, plan alterTestModel) []string {
				return []string{prefix(plan).Toggle(plan.Superuser.ValueBool(), "CREATEUSER", "NOCREATEUSER").String()}
			},
		},
		{
			attribute:     "session_timeout",
			value:         func(m alterTestModel) attr.Value { return m.Timeout },
			skipNullPrior: true,
			render: func(prev, plan alterTestModel) []string {
				if plan.Timeout.IsNull() {
					return []string{prefix(plan).Kw("RESET SESSION TIMEOUT").String()}
				}
				return []string{prefix(plan).KwInt("SESSION TIMEOUT", plan.Timeout.ValueInt64()).String()}
			},
		},
	}
}

// TestAlterStatements emits statements only for changed, known attributes, in step order.
func TestAlterStatements(t *testing.T) {
	base := alterTestModel{Name: types.StringValue(`etl"x`), Limit: types.Int64Value(5), Superuser: types.BoolValue(false), Timeout: types.Int64Value(60)}
	with := func(change func(*alterTestModel)) alterTestModel {
		model := base
		change(&model)
		return model
	}
	for _, test := range []struct {
		name       string
		prev, plan alterTestModel
		expected   []string
	}{
		{"unchanged", base, base, nil},
		{"one option", base, with(func(m *alterTestModel) { m.Limit = types.Int64Value(10) }), []string{`ALTER USER "etl""x" CONNECTION LIMIT 10`}},
		{
			"several options in step order",
			base,
			with(func(m *alterTestModel) {
				m.Timeout, m.Superuser, m.Limit = types.Int64Value(120), types.BoolValue(true), types.Int64Value(1)
			}),
			[]string{`ALTER USER "etl""x" CONNECTION LIMIT 1`, `ALTER USER "etl""x" CREATEUSER`, `ALTER USER "etl""x" SESSION TIMEOUT 120`},
		},
		{"removed value", base, with(func(m *alterTestModel) { m.Limit = types.Int64Null() }), []string{`ALTER USER "etl""x" CONNECTION LIMIT UNLIMITED`}},
		{"unknown plan value", base, with(func(m *alterTestModel) { m.Limit = types.Int64Unknown() }), nil},
		{"null prior without skip", with(func(m *alterTestModel) { m.Limit = types.Int64Null() }), base, []string{`ALTER USER "etl""x" CONNECTION LIMIT 5`}},
		{"null prior with skip", with(func(m *alterTestModel) { m.Timeout = types.Int64Null() }), base, nil},
		{"reset with skip", base, with(func(m *alterTestModel) { m.Timeout = types.Int64Null() }), []string{`ALTER USER "etl""x" RESET SESSION TIMEOUT`}},
	} {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expected, alterStatements(test.prev, test.plan, alterTestSteps()))
		})
	}
	assert.Nil(t, alterStatements(base, with(func(m *alterTestModel) { m.Limit = types.Int64Value(1) }), []alterStep[alterTestModel]{}))
}

// TestAlterCoverage checks the coverage helper against the user resource's in-place attributes.
func TestAlterCoverage(t *testing.T) {
	step := func(attribute string) alterStep[userModel] {
		return alterStep[userModel]{attribute: attribute}
	}
	complete := []alterStep[userModel]{step("superuser"), step("create_database"), step("password_wo_version")}
	// The user resource's other options get steps too, so the cases below vary only the three above.
	for _, extra := range userAlterSteps {
		if !slices.ContainsFunc(complete, func(existing alterStep[userModel]) bool { return existing.attribute == extra.attribute }) {
			complete = append(complete, step(extra.attribute))
		}
	}
	assertAlterCoverage(t, newUserResource(), complete, "password_wo")

	for _, test := range []struct {
		name                string
		steps               []alterStep[userModel]
		exempt              []string
		missing, unexpected []string
	}{
		{"missing step", slices.Delete(slices.Clone(complete), 2, 3), []string{"password_wo"}, []string{"password_wo_version"}, nil},
		{"missing exemption", complete, nil, []string{"password_wo"}, nil},
		{"replacing attribute", append(slices.Clone(complete), step("name")), []string{"password_wo"}, nil, []string{"name"}},
		{"computed attribute", append(slices.Clone(complete), step("id")), []string{"password_wo"}, nil, []string{"id"}},
		{"unknown attribute", complete, []string{"password_wo", "pasword_wo"}, nil, []string{"pasword_wo"}},
		{"duplicate step", append(slices.Clone(complete), step("superuser")), []string{"password_wo"}, nil, []string{"superuser"}},
		{"step and exemption", complete, []string{"password_wo", "superuser"}, nil, []string{"superuser"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			missing, unexpected := alterCoverageGaps(t, newUserResource(), test.steps, test.exempt...)
			assert.Equal(t, test.missing, missing)
			assert.Equal(t, test.unexpected, unexpected)
		})
	}
}

// TestAlterCoverageFollowsPolicy classifies attributes by the replacement policy, so a conditional attribute whose
// generic sample replaces still needs its in-place step, and falls back to sampling without a policy.
func TestAlterCoverageFollowsPolicy(t *testing.T) {
	widening := stringplanmodifier.RequiresReplaceIf(func(_ context.Context, request planmodifier.StringRequest, response *stringplanmodifier.RequiresReplaceIfFuncResponse) {
		response.RequiresReplace = request.PlanValue.ValueString() != "VARCHAR(512)"
	}, "Narrowing replaces.", "Narrowing replaces.")
	attributes := map[string]schema.Attribute{
		"name":        schema.StringAttribute{Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"column_type": schema.StringAttribute{Required: true, PlanModifiers: []planmodifier.String{widening}},
		"comment":     schema.StringAttribute{Optional: true},
		"id":          schema.StringAttribute{Computed: true},
	}
	policy := map[string]replaceRule{
		"name": replaceAlways, "column_type": replaceConditional("TestAlterCoverageFollowsPolicy"), "comment": replaceNever,
	}
	blocks := map[string]schema.Block{
		"column":       schema.ListNestedBlock{PlanModifiers: []planmodifier.List{listplanmodifier.RequiresReplace()}},
		"distribution": schema.SingleNestedBlock{Attributes: map[string]schema.Attribute{"style": schema.StringAttribute{Optional: true}}},
	}
	s := schema.Schema{Attributes: attributes, Blocks: blocks}
	policy["column"] = replaceConditional("TestAlterCoverageFollowsPolicy")
	assert.Equal(t, map[string]bool{"name": false, "column_type": true, "comment": true, "column": true, "distribution": true}, inPlaceInputs(t, s, policy))
	assert.Equal(t, map[string]bool{"name": false, "column_type": false, "comment": true, "column": false, "distribution": true}, inPlaceInputs(t, s, nil),
		"without a policy the generic sample decides")
}

// TestAlterCoverageWithBlocks requires an alter step for every block updated in place and refuses one for a
// replacing block.
func TestAlterCoverageWithBlocks(t *testing.T) {
	step := func(attribute string) alterStep[struct{}] { return alterStep[struct{}]{attribute: attribute} }
	missing, unexpected := alterCoverageGaps(t, newBlockTestResource(), []alterStep[struct{}]{step("owner"), step("unique")})
	assert.Equal(t, []string{"distribution"}, missing)
	assert.Empty(t, unexpected)
	missing, unexpected = alterCoverageGaps(t, newBlockTestResource(), []alterStep[struct{}]{step("owner"), step("unique"), step("distribution"), step("column"), step(timeoutsBlockName)})
	assert.Empty(t, missing, "the timeouts block needs no alter step")
	assert.Equal(t, []string{"column", timeoutsBlockName}, unexpected, "a timeouts change runs no SQL, so a step for it is refused")
}
