package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	datasourceschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFunctionArgumentsRejectNullElements rejects null entries in arguments for both the resource and the lookup, since
// a null cannot name an argument type.
func TestFunctionArgumentsRejectNullElements(t *testing.T) {
	ctx := context.Background()
	var lookup datasource.SchemaResponse
	newFunctionDataSource().Schema(ctx, datasource.SchemaRequest{}, &lookup)
	var managed resource.SchemaResponse
	newFunctionResource().Schema(ctx, resource.SchemaRequest{}, &managed)
	for name, checks := range map[string][]validator.List{
		"resource": managed.Schema.Attributes["arguments"].(resourceschema.ListAttribute).Validators,
		"lookup":   lookup.Schema.Attributes["arguments"].(datasourceschema.ListAttribute).Validators,
	} {
		t.Run(name, func(t *testing.T) {
			for _, value := range []types.List{
				types.ListValueMust(types.StringType, []attr.Value{types.StringValue("integer"), types.StringNull()}),
				types.ListValueMust(types.StringType, []attr.Value{types.StringNull()}),
			} {
				var response validator.ListResponse
				for _, check := range checks {
					check.ValidateList(ctx, validator.ListRequest{Path: path.Root("arguments"), ConfigValue: value}, &response)
				}
				assert.True(t, response.Diagnostics.HasError(), "%s", value)
			}
		})
	}
}

var _ = registerParity(parityCase{source: newFunctionDataSource, resource: newFunctionResource, selectors: []string{"database", "schema", "name", "arguments"}})

// TestFunctionLookup observes the populated overload by its input types, with the resource's identity.
func TestFunctionLookup(t *testing.T) {
	source := newFunctionDataSource()
	data := catalogLookupObject(t, source, map[string]string{"database": "admin", "schema": "public", "name": "f_example"})
	lookupValue(&data, "arguments", types.ListValueMust(types.StringType, []attr.Value{types.StringValue("int4")}))
	c := fullCatalog()
	state, diagnostics := readSource(t, source, data, c)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	var observed types.Object
	require.False(t, state.Get(context.Background(), &observed).HasError())
	attributes := observed.Attributes()
	for name, expected := range map[string]string{"signature": "INTEGER", "return_type": "INTEGER", "volatility": "IMMUTABLE", "language": "SQL", "body": "SELECT $1 + 1", "owner": "admin"} {
		assert.Equal(t, types.StringValue(expected), attributes[name], name)
	}
	assert.Equal(t, types.StringValue(definitionFingerprint("SELECT $1 + 1")), attributes["definition_fingerprint"])
	assertLookupIdentity(t, attributes["id"].(types.String), "admin", map[string]string{"schema": "public", "name": "f_example", "arguments": "INTEGER"})
	assert.Empty(t, c.writes)

	// Another overload of the same name is not found, which is an error for an object lookup.
	lookupValue(&data, "arguments", types.ListNull(types.StringType))
	_, diagnostics = readSource(t, source, data, fullCatalog())
	assert.True(t, diagnostics.HasError())
}
