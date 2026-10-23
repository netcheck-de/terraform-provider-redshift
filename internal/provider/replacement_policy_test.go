package provider

import (
	"context"
	"slices"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// replaceKind classifies how changing one input attribute plans.
type replaceKind int

const (
	// replaceKindNever updates the attribute in place.
	replaceKindNever replaceKind = iota
	// replaceKindAlways replaces the resource whenever the attribute changes.
	replaceKindAlways
	// replaceKindConditional replaces only for some changes, such as narrowing a column type.
	replaceKindConditional
)

// replaceRule is one input attribute's expected replacement behavior.
type replaceRule struct {
	// kind selects in-place, replacing, or value-dependent planning.
	kind replaceKind
}

var (
	// replaceNever expects an in-place update.
	replaceNever = replaceRule{kind: replaceKindNever}
	// replaceAlways expects a replacement.
	replaceAlways = replaceRule{kind: replaceKindAlways}
	// replaceConditional expects value-dependent replacement, which the resource's own tests cover.
	replaceConditional = replaceRule{kind: replaceKindConditional}
)

// replacementPolicies maps each resource type name to the rule of every input attribute.
var replacementPolicies testRegistry[map[string]replaceRule]

// registerReplacementPolicy declares a resource's replacement rules from its own test file.
func registerReplacementPolicy(typeName string, policy map[string]replaceRule) bool {
	return replacementPolicies.add(typeName, policy)
}

// anyReplaces runs each modifier and reports whether one requests replacement.
func anyReplaces[M any](modifiers []M, run func(M) bool) bool {
	for _, modifier := range modifiers {
		if run(modifier) {
			return true
		}
	}
	return false
}

// changeReplaces reports whether an input's plan modifiers replace an existing resource when its value changes.
// RequiresReplace and RequiresReplaceIf return early only for creation, destruction, and equal values, so one
// null-to-unknown change under a non-null state and plan exercises the wiring. Modifiers that read sibling values
// find none in the empty schema; their diagnostics do not matter here.
func changeReplaces(t *testing.T, input any) bool {
	t.Helper()
	ctx := context.Background()
	var valueType attr.Type
	switch input := input.(type) {
	case schema.Attribute:
		valueType = input.GetType()
	case schema.Block:
		valueType = input.Type()
	default:
		t.Fatalf("replacement input must be a schema attribute or block, got %T", input)
	}
	existing := tftypes.NewValue(tftypes.Object{}, map[string]tftypes.Value{})
	state, plan, config := tfsdk.State{Schema: schema.Schema{}, Raw: existing}, tfsdk.Plan{Schema: schema.Schema{}, Raw: existing}, tfsdk.Config{Schema: schema.Schema{}, Raw: existing}
	switch input := input.(type) {
	case interface{ StringPlanModifiers() []planmodifier.String }:
		return anyReplaces(input.StringPlanModifiers(), func(modifier planmodifier.String) bool {
			response := planmodifier.StringResponse{PlanValue: types.StringUnknown()}
			modifier.PlanModifyString(ctx, planmodifier.StringRequest{State: state, Plan: plan, Config: config, StateValue: types.StringNull(), PlanValue: types.StringUnknown(), ConfigValue: types.StringUnknown()}, &response)
			return response.RequiresReplace
		})
	case interface{ BoolPlanModifiers() []planmodifier.Bool }:
		return anyReplaces(input.BoolPlanModifiers(), func(modifier planmodifier.Bool) bool {
			response := planmodifier.BoolResponse{PlanValue: types.BoolUnknown()}
			modifier.PlanModifyBool(ctx, planmodifier.BoolRequest{State: state, Plan: plan, Config: config, StateValue: types.BoolNull(), PlanValue: types.BoolUnknown(), ConfigValue: types.BoolUnknown()}, &response)
			return response.RequiresReplace
		})
	case interface{ Int64PlanModifiers() []planmodifier.Int64 }:
		return anyReplaces(input.Int64PlanModifiers(), func(modifier planmodifier.Int64) bool {
			response := planmodifier.Int64Response{PlanValue: types.Int64Unknown()}
			modifier.PlanModifyInt64(ctx, planmodifier.Int64Request{State: state, Plan: plan, Config: config, StateValue: types.Int64Null(), PlanValue: types.Int64Unknown(), ConfigValue: types.Int64Unknown()}, &response)
			return response.RequiresReplace
		})
	case interface{ ListPlanModifiers() []planmodifier.List }:
		element := valueType.(attr.TypeWithElementType).ElementType()
		return anyReplaces(input.ListPlanModifiers(), func(modifier planmodifier.List) bool {
			response := planmodifier.ListResponse{PlanValue: types.ListUnknown(element)}
			modifier.PlanModifyList(ctx, planmodifier.ListRequest{State: state, Plan: plan, Config: config, StateValue: types.ListNull(element), PlanValue: types.ListUnknown(element), ConfigValue: types.ListUnknown(element)}, &response)
			return response.RequiresReplace
		})
	case interface{ SetPlanModifiers() []planmodifier.Set }:
		element := valueType.(attr.TypeWithElementType).ElementType()
		return anyReplaces(input.SetPlanModifiers(), func(modifier planmodifier.Set) bool {
			response := planmodifier.SetResponse{PlanValue: types.SetUnknown(element)}
			modifier.PlanModifySet(ctx, planmodifier.SetRequest{State: state, Plan: plan, Config: config, StateValue: types.SetNull(element), PlanValue: types.SetUnknown(element), ConfigValue: types.SetUnknown(element)}, &response)
			return response.RequiresReplace
		})
	case interface{ MapPlanModifiers() []planmodifier.Map }:
		element := valueType.(attr.TypeWithElementType).ElementType()
		return anyReplaces(input.MapPlanModifiers(), func(modifier planmodifier.Map) bool {
			response := planmodifier.MapResponse{PlanValue: types.MapUnknown(element)}
			modifier.PlanModifyMap(ctx, planmodifier.MapRequest{State: state, Plan: plan, Config: config, StateValue: types.MapNull(element), PlanValue: types.MapUnknown(element), ConfigValue: types.MapUnknown(element)}, &response)
			return response.RequiresReplace
		})
	case interface{ ObjectPlanModifiers() []planmodifier.Object }:
		fields := valueType.(attr.TypeWithAttributeTypes).AttributeTypes()
		return anyReplaces(input.ObjectPlanModifiers(), func(modifier planmodifier.Object) bool {
			response := planmodifier.ObjectResponse{PlanValue: types.ObjectUnknown(fields)}
			modifier.PlanModifyObject(ctx, planmodifier.ObjectRequest{State: state, Plan: plan, Config: config, StateValue: types.ObjectNull(fields), PlanValue: types.ObjectUnknown(fields), ConfigValue: types.ObjectUnknown(fields)}, &response)
			return response.RequiresReplace
		})
	default:
		t.Fatalf("add replacement coverage for input type %T", input)
		return false
	}
}

// nestedInputs returns the child attributes and blocks of a nested attribute or block, keyed by name; attributes
// and blocks of one object never share a name.
func nestedInputs(input any) map[string]any {
	var attributes map[string]schema.Attribute
	var blocks map[string]schema.Block
	switch input := input.(type) {
	case schema.ListNestedAttribute:
		attributes = input.NestedObject.Attributes
	case schema.SetNestedAttribute:
		attributes = input.NestedObject.Attributes
	case schema.MapNestedAttribute:
		attributes = input.NestedObject.Attributes
	case schema.SingleNestedAttribute:
		attributes = input.Attributes
	case schema.ListNestedBlock:
		attributes, blocks = input.NestedObject.Attributes, input.NestedObject.Blocks
	case schema.SetNestedBlock:
		attributes, blocks = input.NestedObject.Attributes, input.NestedObject.Blocks
	case schema.SingleNestedBlock:
		attributes, blocks = input.Attributes, input.Blocks
	}
	nested := map[string]any{}
	for name, attribute := range attributes {
		nested[name] = attribute
	}
	for name, block := range blocks {
		nested[name] = block
	}
	return nested
}

// replacingNestedAttributes lists, as dotted paths, the nested attributes and blocks whose own modifiers request
// replacement. Nested values replace only through the top-level input, whose RequiresReplaceIf sees the whole value.
func replacingNestedAttributes(t *testing.T, input any) []string {
	t.Helper()
	var replacing []string
	for name, child := range nestedInputs(input) {
		if changeReplaces(t, child) {
			replacing = append(replacing, name)
		}
		for _, inner := range replacingNestedAttributes(t, child) {
			replacing = append(replacing, name+"."+inner)
		}
	}
	slices.Sort(replacing)
	return replacing
}

// checkReplacementRule checks one attribute or block against its rule; value-dependent rules are left to the
// resource's own tests.
func checkReplacementRule(t *testing.T, input any, rule replaceRule) {
	t.Helper()
	switch rule.kind {
	case replaceKindAlways:
		assert.True(t, changeReplaces(t, input), "a change must replace")
	case replaceKindNever:
		assert.False(t, changeReplaces(t, input), "a change must update in place")
	}
	assert.Empty(t, replacingNestedAttributes(t, input), "nested attributes replace only through the top-level input")
}

// policyInputs lists the inputs a replacement policy must name: configurable attributes and every block, since a
// block has no computed-only form. The timeouts block is left out: it bounds operations rather than describing the
// object, so like a computed-only attribute it is checked to never replace without being named.
func policyInputs(s schema.Schema) []string {
	var inputs []string
	for name, attribute := range s.Attributes {
		if attribute.IsRequired() || attribute.IsOptional() {
			inputs = append(inputs, name)
		}
	}
	for name := range s.Blocks {
		if name != timeoutsBlockName {
			inputs = append(inputs, name)
		}
	}
	slices.Sort(inputs)
	return inputs
}

// assertReplacementPolicy checks every attribute and block of a resource schema against its policy.
func assertReplacementPolicy(t *testing.T, s schema.Schema, policy map[string]replaceRule) {
	t.Helper()
	inputs := policyInputs(s)
	for _, name := range inputs {
		_, found := policy[name]
		assert.True(t, found, "input %s needs an explicit replacement policy", name)
	}
	for name := range policy {
		assert.Contains(t, inputs, name, "policy must not contain stale input name %s", name)
	}
	children := map[string]any{}
	for name, attribute := range s.Attributes {
		children[name] = attribute
	}
	for name, block := range s.Blocks {
		children[name] = block
	}
	for name, child := range children {
		rule, found := policy[name]
		if !found && slices.Contains(inputs, name) {
			continue
		}
		// Computed-only attributes and the timeouts block must never replace, which the zero rule, replaceNever, checks.
		t.Run(name, func(t *testing.T) { checkReplacementRule(t, child, rule) })
	}
}

// TestEveryResourceAttributeReplacementPolicy covers every input, block, and computed attribute across all resources.
func TestEveryResourceAttributeReplacementPolicy(t *testing.T) {
	require.Empty(t, replacementPolicies.duplicates, "duplicate replacement policies")
	resources, _ := registeredTypeNames()
	registered := map[string]bool{}
	for _, factory := range New("test")().Resources(context.Background()) {
		instance := factory()
		var metadata resource.MetadataResponse
		instance.Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "redshift"}, &metadata)
		registered[metadata.TypeName] = true
		t.Run(metadata.TypeName, func(t *testing.T) {
			policy, found := replacementPolicies.entries[metadata.TypeName]
			require.True(t, found, "resource needs an explicit replacement policy")
			var response resource.SchemaResponse
			instance.Schema(context.Background(), resource.SchemaRequest{}, &response)
			assertReplacementPolicy(t, response.Schema, policy)
		})
	}
	require.Len(t, registered, len(resources))
	for _, name := range replacementPolicies.keys() {
		assert.True(t, registered[name], "replacement policy for unregistered resource %s", name)
	}
}
