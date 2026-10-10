package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	datasourceschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newProcedureDataSource, resource: newProcedureResource, selectors: []string{"database", "schema", "name"}, lookupSelectors: []string{"arguments"}})

// procedureLookupSelector builds the lookup's arguments selector; nil means omitted.
func procedureLookupSelector(inputs ...string) types.List {
	if inputs == nil {
		return types.ListNull(types.StringType)
	}
	values := make([]attr.Value, len(inputs))
	for i, input := range inputs {
		values[i] = types.StringValue(input)
	}
	return types.ListValueMust(types.StringType, values)
}

// TestProcedureLookup observes the populated procedure selected by its input types alone, reports every argument
// including the OUT argument the selector omits, and leaves the settings the catalog does not report null.
func TestProcedureLookup(t *testing.T) {
	source := newProcedureDataSource()
	data := catalogLookupObject(t, source, map[string]string{"database": "admin", "schema": "public", "name": "sp_example"})
	lookupValue(&data, "arguments", procedureLookupSelector("int"))
	c := fullCatalog()
	state, diagnostics := readSource(t, source, data, c)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	var observed types.Object
	require.False(t, state.Get(context.Background(), &observed).HasError())
	attributes := observed.Attributes()
	for name, expected := range map[string]string{"signature": "integer", "security": "INVOKER", "owner": "admin", "body": "BEGIN total := min_id * 2; END;"} {
		assert.Equal(t, types.StringValue(expected), attributes[name], name)
	}
	assert.True(t, attributes["nonatomic"].IsNull())
	assert.True(t, attributes["configuration"].IsNull())
	assert.Equal(t, procedureLookupSelector("int"), attributes["arguments"], "the configured selector is kept")
	assert.Equal(t, []procedureArgumentModel{procedureTestArgument("min_id", "", "integer"), procedureTestArgument("total", "OUT", "bigint")},
		procedureArguments(attributes["argument"].(types.List)))
	assertLookupIdentity(t, attributes["id"].(types.String), "admin", map[string]string{"schema": "public", "name": "sp_example", "arguments": "integer"})
	assert.Empty(t, c.writes)

	lookupValue(&data, "name", types.StringValue("sp_missing"))
	_, diagnostics = readSource(t, source, data, fullCatalog())
	assert.True(t, diagnostics.HasError())
	lookupValue(&data, "arguments", procedureLookupSelector("money"))
	_, diagnostics = readSource(t, source, data, fullCatalog())
	assert.True(t, diagnostics.HasError(), "an invalid selector type")

	var schema datasource.SchemaResponse
	source.Schema(context.Background(), datasource.SchemaRequest{}, &schema)
	withNull := types.ListValueMust(types.StringType, []attr.Value{types.StringNull(), types.StringValue("integer")})
	var validation validator.ListResponse
	for _, check := range schema.Schema.Attributes["arguments"].(datasourceschema.ListAttribute).Validators {
		check.ValidateList(context.Background(), validator.ListRequest{Path: path.Root("arguments"), ConfigValue: withNull}, &validation)
	}
	assert.True(t, validation.Diagnostics.HasError(), "a null selector element")
}

// TestProcedureLookupWithoutArguments selects the overload without input arguments when the selector is omitted or
// empty, and reports its arguments as an empty list, as the resource's absent blocks read in Terraform.
func TestProcedureLookupWithoutArguments(t *testing.T) {
	for name, selector := range map[string]types.List{"omitted": procedureLookupSelector(), "empty": procedureLookupSelector([]string{}...)} {
		t.Run(name, func(t *testing.T) {
			source := newProcedureDataSource()
			data := catalogLookupObject(t, source, map[string]string{"database": "admin", "schema": "public", "name": "sp_refresh"})
			lookupValue(&data, "arguments", selector)
			c := fullCatalog()
			c.family("procedure").(*routineFamily).routines[fakeRoutineKey("public", "sp_refresh", "")] = &fakeRoutine{body: "BEGIN NULL; END;", owner: "admin"}
			state, diagnostics := readSource(t, source, data, c)
			require.False(t, diagnostics.HasError(), "%v", diagnostics)
			var observed types.Object
			require.False(t, state.Get(context.Background(), &observed).HasError())
			arguments := observed.Attributes()["argument"].(types.List)
			assert.False(t, arguments.IsNull())
			assert.Empty(t, arguments.Elements())
			assert.Equal(t, types.StringValue(""), observed.Attributes()["signature"])
			assertLookupIdentity(t, observed.Attributes()["id"].(types.String), "admin", map[string]string{"schema": "public", "name": "sp_refresh", "arguments": ""})
		})
	}
}
