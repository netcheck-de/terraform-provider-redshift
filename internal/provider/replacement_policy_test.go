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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
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
	ctx := context.Background()
	attributeType := attribute.GetType()
	before, after := sampleValues(t, attributeType)
	if !changed {
		after = before
	}
	objectType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{"value": attributeType.TerraformType(ctx)}}
	beforeValue, err := before.ToTerraformValue(ctx)
	require.NoError(t, err)
	afterValue, err := after.ToTerraformValue(ctx)
	require.NoError(t, err)
	single := schema.Schema{Attributes: map[string]schema.Attribute{"value": attribute}}
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
	switch attribute := attribute.(type) {
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
	default:
		t.Fatalf("add replacement coverage for attribute type %T", attribute)
		return false
	}
}

// replacingNestedAttributes lists the nested attributes, at any depth and as dotted paths, whose own modifiers
// request replacement. The framework runs those modifiers too, so a replacing child would replace the resource
// behind the top-level policy's back; nested values replace only through the top-level RequiresReplaceIf, which
// sees the whole value.
func replacingNestedAttributes(t *testing.T, attribute schema.Attribute) []string {
	t.Helper()
	var nested map[string]schema.Attribute
	switch attribute := attribute.(type) {
	case schema.ListNestedAttribute:
		nested = attribute.NestedObject.Attributes
	case schema.SetNestedAttribute:
		nested = attribute.NestedObject.Attributes
	case schema.MapNestedAttribute:
		nested = attribute.NestedObject.Attributes
	case schema.SingleNestedAttribute:
		nested = attribute.Attributes
	default:
		return nil
	}
	var replacing []string
	for name, child := range nested {
		if attributeReplacement(t, child, true, true) {
			replacing = append(replacing, name)
		}
		for _, inner := range replacingNestedAttributes(t, child) {
			replacing = append(replacing, name+"."+inner)
		}
	}
	slices.Sort(replacing)
	return replacing
}

// checkReplacementRule checks one attribute against its rule; a conditional rule delegates the changed-value
// check to its named test.
func checkReplacementRule(t *testing.T, attribute schema.Attribute, rule replaceRule) {
	t.Helper()
	if rule.kind == replaceKindConditional {
		require.NotEmpty(t, rule.test, "conditional replacement needs a named per-resource test")
		requireTestFunction(t, rule.test)
	} else {
		assert.Equal(t, rule.kind == replaceKindAlways, attributeReplacement(t, attribute, true, true), "changed input")
	}
	assert.False(t, attributeReplacement(t, attribute, false, true), "unchanged input")
	assert.False(t, attributeReplacement(t, attribute, true, false), "initial creation")
	assert.Empty(t, replacingNestedAttributes(t, attribute), "nested attributes replace only through the top-level attribute")
}

// TestEveryResourceAttributeReplacementPolicy covers every input and computed attribute across all resources.
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
			inputs := map[string]bool{}
			for name, attribute := range response.Schema.Attributes {
				t.Run(name, func(t *testing.T) {
					rule := replaceNever
					if attribute.IsRequired() || attribute.IsOptional() {
						inputs[name] = true
						var found bool
						rule, found = policy[name]
						require.True(t, found, "input needs an explicit replacement policy")
					}
					checkReplacementRule(t, attribute, rule)
				})
			}
			for name := range policy {
				assert.True(t, inputs[name], "policy must not contain stale attribute name %s", name)
			}
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
			rule := replaceNever
			switch {
			case strings.HasSuffix(name, "_replaces"):
				rule = replaceAlways
			case strings.HasSuffix(name, "_conditional"):
				rule = replaceConditional("TestReplacementSamplesCoverEveryAttributeKind")
			}
			checkReplacementRule(t, attribute, rule)
		})
	}
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
	deep := schema.SingleNestedAttribute{Optional: true, Attributes: map[string]schema.Attribute{
		"inner": schema.ListNestedAttribute{Optional: true, NestedObject: object},
	}}
	assert.Equal(t, []string{"inner.child"}, replacingNestedAttributes(t, deep))
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
