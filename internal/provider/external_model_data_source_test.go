package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newExternalModelDataSource, resource: newExternalModelResource, selectors: []string{"database", "schema", "name"}})

// TestExternalModelLookup reports the settings SHOW MODEL prints to the model owner without mutating.
func TestExternalModelLookup(t *testing.T) {
	c, _ := externalModelFake()
	exerciseCatalogLookup(t, newExternalModelDataSource, map[string]string{"database": "admin", "schema": fakeModelSchema, "name": fakeModelName}, map[string]attr.Value{
		"function":     types.StringValue(fakeModelFunction),
		"iam_role":     types.StringValue("DEFAULT"),
		"model_id":     types.StringValue(fakeModelID),
		"prompt":       types.StringValue(fakeModelPrompt),
		"suffix":       types.StringNull(),
		"request_type": types.StringValue("UNIFIED"),
		// SHOW MODEL never prints the response type, and the lookup does not guess it.
		"response_type": types.StringNull(),
		"owner":         types.StringValue(fakeModelOwner),
	}, c)
	assert.Empty(t, c.writes)
}

// TestExternalModelLookupUnreportedSettings leaves settings SHOW MODEL does not print null.
func TestExternalModelLookupUnreportedSettings(t *testing.T) {
	c, models := externalModelFake()
	models.hideSettings = true
	source := newExternalModelDataSource()
	state, diagnostics := readSource(t, source, catalogLookupObject(t, source, map[string]string{"database": "admin", "schema": fakeModelSchema, "name": fakeModelName}), c)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)

	var observed types.Object
	require.False(t, state.Get(context.Background(), &observed).HasError())

	for _, name := range externalModelLookupSettings {
		assert.True(t, observed.Attributes()[name].IsNull(), name)
	}

	assert.Equal(t, types.StringValue(fakeModelOwner), observed.Attributes()["owner"])
}

// TestExternalModelLookupDescriptions keeps the resource's replacement and state wording out of the lookup.
func TestExternalModelLookupDescriptions(t *testing.T) {
	var response datasource.SchemaResponse
	newExternalModelDataSource().Schema(context.Background(), datasource.SchemaRequest{}, &response)

	for name, description := range nestedDescriptions(response.Schema.Attributes) {
		for _, resourceOnly := range []string{"replaces the model", "recorded in place", "must not manage"} {
			assert.NotContains(t, description, resourceOnly, "%s", name)
		}
	}
}
