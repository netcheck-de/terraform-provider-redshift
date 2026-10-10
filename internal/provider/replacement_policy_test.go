package provider

import (
	"context"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/float64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	testresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
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
	// test names the per-resource test that covers both branches of a conditional rule, which a single
	// generic before/after sample cannot.
	test string
}

var (
	// replaceNever expects an in-place update.
	replaceNever = replaceRule{kind: replaceKindNever}
	// replaceAlways expects a replacement.
	replaceAlways = replaceRule{kind: replaceKindAlways}
)

// replaceConditional expects value-dependent replacement covered by the named Test function.
func replaceConditional(test string) replaceRule {
	return replaceRule{kind: replaceKindConditional, test: test}
}

// replacementPolicies maps each resource type name to the rule of every input attribute.
var replacementPolicies testRegistry[map[string]replaceRule]

// registerReplacementPolicy declares a resource's replacement rules from its own test file.
func registerReplacementPolicy(typeName string, policy map[string]replaceRule) bool {
	return replacementPolicies.add(typeName, policy)
}

// sampleValues returns two distinct known values of a framework type, recursing into collections and objects.
func sampleValues(t *testing.T, attributeType attr.Type) (before, after attr.Value) {
	t.Helper()
	switch typed := attributeType.(type) {
	case basetypes.StringType:
		return types.StringValue("before"), types.StringValue("after")
	case basetypes.BoolType:
		return types.BoolValue(false), types.BoolValue(true)
	case basetypes.Int64Type:
		return types.Int64Value(0), types.Int64Value(1)
	case basetypes.Float64Type:
		return types.Float64Value(0), types.Float64Value(1)
	case basetypes.NumberType:
		return types.NumberValue(big.NewFloat(0)), types.NumberValue(big.NewFloat(1))
	case basetypes.ListType:
		_, element := sampleValues(t, typed.ElemType)
		return types.ListValueMust(typed.ElemType, []attr.Value{}), types.ListValueMust(typed.ElemType, []attr.Value{element})
	case basetypes.SetType:
		_, element := sampleValues(t, typed.ElemType)
		return types.SetValueMust(typed.ElemType, []attr.Value{}), types.SetValueMust(typed.ElemType, []attr.Value{element})
	case basetypes.MapType:
		_, element := sampleValues(t, typed.ElemType)
		return types.MapValueMust(typed.ElemType, map[string]attr.Value{}), types.MapValueMust(typed.ElemType, map[string]attr.Value{"key": element})
	case basetypes.ObjectType:
		befores, afters := map[string]attr.Value{}, map[string]attr.Value{}
		for name, field := range typed.AttrTypes {
			befores[name], afters[name] = sampleValues(t, field)
		}
		return types.ObjectValueMust(typed.AttrTypes, befores), types.ObjectValueMust(typed.AttrTypes, afters)
	default:
		t.Fatalf("add replacement coverage for attribute type %T", attributeType)
		return nil, nil
	}
}

// modifierReplacement runs every plan modifier of one attribute and reports whether any requests replacement.
func modifierReplacement[M any](t *testing.T, modifiers []M, run func(M) (bool, diag.Diagnostics)) bool {
	t.Helper()
	replace := false
	for _, modifier := range modifiers {
		requires, diagnostics := run(modifier)
		require.False(t, diagnostics.HasError(), "%v", diagnostics)
		replace = replace || requires
	}
	return replace
}

// attributeReplacement invokes the actual schema modifiers with a changed or unchanged input.
func attributeReplacement(t *testing.T, attribute schema.Attribute, changed, existing bool) bool {
	t.Helper()
	return inputReplacement(t, attribute, changed, existing)
}

// blockReplacement invokes a block's own plan modifiers, which the framework runs like those of the nested
// attribute of the same type.
func blockReplacement(t *testing.T, block schema.Block, changed, existing bool) bool {
	t.Helper()
	return inputReplacement(t, block, changed, existing)
}

// inputReplacement plans a schema.Attribute or schema.Block as the only input of a schema and reports whether a
// changed or unchanged value requests replacement.
func inputReplacement(t *testing.T, input any, changed, existing bool) bool {
	t.Helper()
	ctx := context.Background()
	var single schema.Schema
	var attributeType attr.Type
	switch input := input.(type) {
	case schema.Attribute:
		single, attributeType = schema.Schema{Attributes: map[string]schema.Attribute{"value": input}}, input.GetType()
	case schema.Block:
		single, attributeType = schema.Schema{Blocks: map[string]schema.Block{"value": input}}, input.Type()
	default:
		t.Fatalf("replacement input must be a schema attribute or block, got %T", input)
	}
	// A custom object type, such as the timeouts block's, plans like the object it wraps, and plan modifiers receive
	// that plain object.
	if custom, ok := attributeType.(attr.TypeWithAttributeTypes); ok {
		attributeType = types.ObjectType{AttrTypes: custom.AttributeTypes()}
	}
	before, after := sampleValues(t, attributeType)
	if !changed {
		after = before
	}
	objectType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{"value": attributeType.TerraformType(ctx)}}
	beforeValue, err := before.ToTerraformValue(ctx)
	require.NoError(t, err)
	afterValue, err := after.ToTerraformValue(ctx)
	require.NoError(t, err)
	state := tfsdk.State{Schema: single, Raw: tftypes.NewValue(objectType, map[string]tftypes.Value{"value": beforeValue})}
	if !existing {
		state.Raw = tftypes.NewValue(objectType, nil)
		before, err = attributeType.ValueFromTerraform(ctx, tftypes.NewValue(attributeType.TerraformType(ctx), nil))
		require.NoError(t, err)
	}
	plan := tfsdk.Plan{Schema: single, Raw: tftypes.NewValue(objectType, map[string]tftypes.Value{"value": afterValue})}
	config := tfsdk.Config{Schema: single, Raw: plan.Raw}
	root := path.Root("value")
	list := func(modifiers []planmodifier.List) bool {
		return modifierReplacement(t, modifiers, func(modifier planmodifier.List) (bool, diag.Diagnostics) {
			request := planmodifier.ListRequest{Path: root, Config: config, Plan: plan, State: state, ConfigValue: after.(types.List), PlanValue: after.(types.List), StateValue: before.(types.List)}
			response := planmodifier.ListResponse{PlanValue: request.PlanValue}
			modifier.PlanModifyList(ctx, request, &response)
			return response.RequiresReplace, response.Diagnostics
		})
	}
	set := func(modifiers []planmodifier.Set) bool {
		return modifierReplacement(t, modifiers, func(modifier planmodifier.Set) (bool, diag.Diagnostics) {
			request := planmodifier.SetRequest{Path: root, Config: config, Plan: plan, State: state, ConfigValue: after.(types.Set), PlanValue: after.(types.Set), StateValue: before.(types.Set)}
			response := planmodifier.SetResponse{PlanValue: request.PlanValue}
			modifier.PlanModifySet(ctx, request, &response)
			return response.RequiresReplace, response.Diagnostics
		})
	}
	mapping := func(modifiers []planmodifier.Map) bool {
		return modifierReplacement(t, modifiers, func(modifier planmodifier.Map) (bool, diag.Diagnostics) {
			request := planmodifier.MapRequest{Path: root, Config: config, Plan: plan, State: state, ConfigValue: after.(types.Map), PlanValue: after.(types.Map), StateValue: before.(types.Map)}
			response := planmodifier.MapResponse{PlanValue: request.PlanValue}
			modifier.PlanModifyMap(ctx, request, &response)
			return response.RequiresReplace, response.Diagnostics
		})
	}
	object := func(modifiers []planmodifier.Object) bool {
		return modifierReplacement(t, modifiers, func(modifier planmodifier.Object) (bool, diag.Diagnostics) {
			request := planmodifier.ObjectRequest{Path: root, Config: config, Plan: plan, State: state, ConfigValue: after.(types.Object), PlanValue: after.(types.Object), StateValue: before.(types.Object)}
			response := planmodifier.ObjectResponse{PlanValue: request.PlanValue}
			modifier.PlanModifyObject(ctx, request, &response)
			return response.RequiresReplace, response.Diagnostics
		})
	}
	switch attribute := input.(type) {
	case schema.StringAttribute:
		return modifierReplacement(t, attribute.PlanModifiers, func(modifier planmodifier.String) (bool, diag.Diagnostics) {
			request := planmodifier.StringRequest{Path: root, Config: config, Plan: plan, State: state, ConfigValue: after.(types.String), PlanValue: after.(types.String), StateValue: before.(types.String)}
			response := planmodifier.StringResponse{PlanValue: request.PlanValue}
			modifier.PlanModifyString(ctx, request, &response)
			return response.RequiresReplace, response.Diagnostics
		})
	case schema.BoolAttribute:
		return modifierReplacement(t, attribute.PlanModifiers, func(modifier planmodifier.Bool) (bool, diag.Diagnostics) {
			request := planmodifier.BoolRequest{Path: root, Config: config, Plan: plan, State: state, ConfigValue: after.(types.Bool), PlanValue: after.(types.Bool), StateValue: before.(types.Bool)}
			response := planmodifier.BoolResponse{PlanValue: request.PlanValue}
			modifier.PlanModifyBool(ctx, request, &response)
			return response.RequiresReplace, response.Diagnostics
		})
	case schema.Int64Attribute:
		return modifierReplacement(t, attribute.PlanModifiers, func(modifier planmodifier.Int64) (bool, diag.Diagnostics) {
			request := planmodifier.Int64Request{Path: root, Config: config, Plan: plan, State: state, ConfigValue: after.(types.Int64), PlanValue: after.(types.Int64), StateValue: before.(types.Int64)}
			response := planmodifier.Int64Response{PlanValue: request.PlanValue}
			modifier.PlanModifyInt64(ctx, request, &response)
			return response.RequiresReplace, response.Diagnostics
		})
	case schema.Float64Attribute:
		return modifierReplacement(t, attribute.PlanModifiers, func(modifier planmodifier.Float64) (bool, diag.Diagnostics) {
			request := planmodifier.Float64Request{Path: root, Config: config, Plan: plan, State: state, ConfigValue: after.(types.Float64), PlanValue: after.(types.Float64), StateValue: before.(types.Float64)}
			response := planmodifier.Float64Response{PlanValue: request.PlanValue}
			modifier.PlanModifyFloat64(ctx, request, &response)
			return response.RequiresReplace, response.Diagnostics
		})
	case schema.NumberAttribute:
		return modifierReplacement(t, attribute.PlanModifiers, func(modifier planmodifier.Number) (bool, diag.Diagnostics) {
			request := planmodifier.NumberRequest{Path: root, Config: config, Plan: plan, State: state, ConfigValue: after.(types.Number), PlanValue: after.(types.Number), StateValue: before.(types.Number)}
			response := planmodifier.NumberResponse{PlanValue: request.PlanValue}
			modifier.PlanModifyNumber(ctx, request, &response)
			return response.RequiresReplace, response.Diagnostics
		})
	case schema.ListAttribute:
		return list(attribute.PlanModifiers)
	case schema.ListNestedAttribute:
		return list(attribute.PlanModifiers)
	case schema.SetAttribute:
		return set(attribute.PlanModifiers)
	case schema.SetNestedAttribute:
		return set(attribute.PlanModifiers)
	case schema.MapAttribute:
		return mapping(attribute.PlanModifiers)
	case schema.MapNestedAttribute:
		return mapping(attribute.PlanModifiers)
	case schema.ObjectAttribute:
		return object(attribute.PlanModifiers)
	case schema.SingleNestedAttribute:
		return object(attribute.PlanModifiers)
	case schema.ListNestedBlock:
		return list(attribute.PlanModifiers)
	case schema.SetNestedBlock:
		return set(attribute.PlanModifiers)
	case schema.SingleNestedBlock:
		return object(attribute.PlanModifiers)
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

// replacingNestedAttributes lists the nested attributes and blocks of an attribute or block, at any depth and as
// dotted paths, whose own modifiers request replacement. The framework runs those modifiers too, so a replacing
// child would replace the resource behind the top-level policy's back; nested values replace only through the
// top-level RequiresReplaceIf, which sees the whole value.
func replacingNestedAttributes(t *testing.T, input any) []string {
	t.Helper()
	var replacing []string
	for name, child := range nestedInputs(input) {
		if inputReplacement(t, child, true, true) {
			replacing = append(replacing, name)
		}
		for _, inner := range replacingNestedAttributes(t, child) {
			replacing = append(replacing, name+"."+inner)
		}
	}
	slices.Sort(replacing)
	return replacing
}

// checkReplacementRule checks one attribute or block against its rule; a conditional rule delegates the
// changed-value check to its named test.
func checkReplacementRule(t *testing.T, input any, rule replaceRule) {
	t.Helper()
	if rule.kind == replaceKindConditional {
		require.NotEmpty(t, rule.test, "conditional replacement needs a named per-resource test")
		requireTestFunction(t, rule.test)
	} else {
		assert.Equal(t, rule.kind == replaceKindAlways, inputReplacement(t, input, true, true), "changed input")
	}
	assert.False(t, inputReplacement(t, input, false, true), "unchanged input")
	assert.False(t, inputReplacement(t, input, true, false), "initial creation")
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

// TestReplacementSamplesCoverEveryAttributeKind checks the sampler and modifier dispatch for kinds that no
// current resource uses, so new resources do not first discover a gap in the shared test.
func TestReplacementSamplesCoverEveryAttributeKind(t *testing.T) {
	nested := schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{"name": schema.StringAttribute{Required: true}, "size": schema.Int64Attribute{Optional: true}}}
	element := map[string]attr.Type{"name": types.StringType, "weight": types.Float64Type}
	for name, attribute := range map[string]schema.Attribute{
		"float64":             schema.Float64Attribute{Optional: true},
		"number":              schema.NumberAttribute{Optional: true},
		"list":                schema.ListAttribute{Optional: true, ElementType: types.StringType},
		"set_of_lists":        schema.SetAttribute{Optional: true, ElementType: types.ListType{ElemType: types.Int64Type}},
		"map":                 schema.MapAttribute{Optional: true, ElementType: types.BoolType},
		"object":              schema.ObjectAttribute{Optional: true, AttributeTypes: element},
		"list_nested":         schema.ListNestedAttribute{Optional: true, NestedObject: nested},
		"set_nested":          schema.SetNestedAttribute{Optional: true, NestedObject: nested},
		"map_nested":          schema.MapNestedAttribute{Optional: true, NestedObject: nested},
		"single_nested":       schema.SingleNestedAttribute{Optional: true, Attributes: nested.Attributes},
		"list_replaces":       schema.ListAttribute{Optional: true, ElementType: types.StringType, PlanModifiers: []planmodifier.List{listplanmodifier.RequiresReplace()}},
		"map_nested_replaces": schema.MapNestedAttribute{Optional: true, NestedObject: nested, PlanModifiers: []planmodifier.Map{mapplanmodifier.RequiresReplace()}},
		"float64_replaces":    schema.Float64Attribute{Optional: true, PlanModifiers: []planmodifier.Float64{float64planmodifier.RequiresReplace()}},
		"string_conditional": schema.StringAttribute{Optional: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplaceIf(func(_ context.Context, request planmodifier.StringRequest, response *stringplanmodifier.RequiresReplaceIfFuncResponse) {
			response.RequiresReplace = strings.HasPrefix(request.PlanValue.ValueString(), "narrow")
		}, "Narrowing replaces.", "Narrowing replaces.")}},
	} {
		t.Run(name, func(t *testing.T) {
			before, after := sampleValues(t, attribute.GetType())
			assert.False(t, before.Equal(after), "samples must differ")
			checkReplacementRule(t, attribute, sampleRule(name))
		})
	}
	block := schema.NestedBlockObject{Attributes: nested.Attributes, Blocks: map[string]schema.Block{"inner": schema.SingleNestedBlock{Attributes: nested.Attributes}}}
	for name, block := range map[string]schema.Block{
		"list_block":            schema.ListNestedBlock{NestedObject: block},
		"set_block":             schema.SetNestedBlock{NestedObject: block},
		"single_block":          schema.SingleNestedBlock{Attributes: nested.Attributes, Blocks: block.Blocks},
		"list_block_replaces":   schema.ListNestedBlock{NestedObject: block, PlanModifiers: []planmodifier.List{listplanmodifier.RequiresReplace()}},
		"set_block_replaces":    schema.SetNestedBlock{NestedObject: block, PlanModifiers: []planmodifier.Set{setplanmodifier.RequiresReplace()}},
		"single_block_replaces": schema.SingleNestedBlock{Attributes: nested.Attributes, PlanModifiers: []planmodifier.Object{objectplanmodifier.RequiresReplace()}},
	} {
		t.Run(name, func(t *testing.T) {
			before, after := sampleValues(t, block.Type())
			assert.False(t, before.Equal(after), "samples must differ")
			checkReplacementRule(t, block, sampleRule(name))
		})
	}
}

// sampleRule derives the expected rule of a sample input from its name's suffix.
func sampleRule(name string) replaceRule {
	switch {
	case strings.HasSuffix(name, "_replaces"):
		return replaceAlways
	case strings.HasSuffix(name, "_conditional"):
		return replaceConditional("TestReplacementSamplesCoverEveryAttributeKind")
	}
	return replaceNever
}

// TestBlockReplacementPolicy checks a schema with every block shape against its policy, and that every block counts
// as an input even though blocks have no required or optional flag.
func TestBlockReplacementPolicy(t *testing.T) {
	var response resource.SchemaResponse
	newBlockTestResource().Schema(context.Background(), resource.SchemaRequest{}, &response)
	assert.Equal(t, []string{"column", "database", "distribution", "name", "owner", "unique"}, policyInputs(response.Schema))
	assertReplacementPolicy(t, response.Schema, blockTestPolicy)
	column := response.Schema.Blocks["column"].(schema.ListNestedBlock)
	assert.True(t, blockReplacement(t, column, true, true))
	assert.False(t, blockReplacement(t, response.Schema.Blocks["unique"], true, true))
	assert.Empty(t, replacingNestedAttributes(t, column), "the identity block and the column attributes replace only through column")
}

// TestReplacingNestedAttributesFound finds a replacing child in every nested shape, including a single nested
// object, whose children the framework plans like collection elements.
func TestReplacingNestedAttributesFound(t *testing.T) {
	replacing := map[string]schema.Attribute{
		"child": schema.StringAttribute{Optional: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"other": schema.StringAttribute{Optional: true},
	}
	object := schema.NestedAttributeObject{Attributes: replacing}
	for name, attribute := range map[string]schema.Attribute{
		"single": schema.SingleNestedAttribute{Optional: true, Attributes: replacing},
		"list":   schema.ListNestedAttribute{Optional: true, NestedObject: object},
		"set":    schema.SetNestedAttribute{Optional: true, NestedObject: object},
		"map":    schema.MapNestedAttribute{Optional: true, NestedObject: object},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, []string{"child"}, replacingNestedAttributes(t, attribute))
		})
	}
	blockObject := schema.NestedBlockObject{Attributes: replacing}
	for name, block := range map[string]schema.Block{
		"single block": schema.SingleNestedBlock{Attributes: replacing},
		"list block":   schema.ListNestedBlock{NestedObject: blockObject},
		"set block":    schema.SetNestedBlock{NestedObject: blockObject},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, []string{"child"}, replacingNestedAttributes(t, block))
		})
	}
	deep := schema.SingleNestedAttribute{Optional: true, Attributes: map[string]schema.Attribute{
		"inner": schema.ListNestedAttribute{Optional: true, NestedObject: object},
	}}
	assert.Equal(t, []string{"inner.child"}, replacingNestedAttributes(t, deep))
	// A replacing child block is reported by name, and its own replacing children by path.
	parent := schema.ListNestedBlock{NestedObject: schema.NestedBlockObject{Blocks: map[string]schema.Block{
		"inner": schema.SingleNestedBlock{Attributes: replacing, PlanModifiers: []planmodifier.Object{objectplanmodifier.RequiresReplace()}},
	}}}
	assert.Equal(t, []string{"inner", "inner.child"}, replacingNestedAttributes(t, parent))
	assert.Empty(t, replacingNestedAttributes(t, schema.StringAttribute{Optional: true}))
}

// TestReferencedRoleNameChangeReplacesMembership verifies Terraform propagates names without caller lifecycle triggers.
func TestReferencedRoleNameChangeReplacesMembership(t *testing.T) {
	c := &catalog{identity: true, privileges: map[string]bool{}}
	configuration := func(name string) string {
		return fmt.Sprintf(`
provider "redshift" {
  region = "eu-central-1"
  workgroup_name = "warehouse"
  database = "admin"
}
resource "redshift_role" "reader" { name = %q }
resource "redshift_role_grant" "reader" {
  role = "sys:dba"
  to_role = redshift_role.reader.name
}`, name)
	}
	testresource.UnitTest(t, testresource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(&redshiftProvider{version: "test", client: c})},
		Steps: []testresource.TestStep{
			{Config: configuration("before")},
			{Config: configuration("after"), ConfigPlanChecks: testresource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
				plancheck.ExpectResourceAction("redshift_role.reader", plancheck.ResourceActionDestroyBeforeCreate),
				plancheck.ExpectResourceAction("redshift_role_grant.reader", plancheck.ResourceActionDestroyBeforeCreate),
			}}, Check: testresource.ComposeTestCheckFunc(
				testresource.TestCheckResourceAttr("redshift_role.reader", "name", "after"),
				testresource.TestCheckResourceAttr("redshift_role_grant.reader", "to_role", "after"),
			)},
			{Config: configuration("after"), PlanOnly: true},
		},
	})
}
