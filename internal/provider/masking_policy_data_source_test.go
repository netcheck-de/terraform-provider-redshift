package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newMaskingPolicyDataSource, resource: newMaskingPolicyResource, selectors: []string{"database", "name"}})

var _ = registerParity(parityCase{source: newMaskingPolicyAttachmentDataSource, resource: newMaskingPolicyAttachmentResource, selectors: []string{"database", "policy", "schema", "relation", "columns", "grantee", "grantee_type"}})

var _ = registerParity(parityCase{source: newMaskingPoliciesDataSource, resource: newMaskingPolicyResource, collection: true, filters: []string{"database"}})

// TestMaskingPolicyLookup observes the catalog inputs and expression without owning the policy.
func TestMaskingPolicyLookup(t *testing.T) {
	text := "CAST('***' AS TEXT)"
	exerciseCatalogLookup(t, newMaskingPolicyDataSource, map[string]string{"database": "admin", "name": "mask_email"}, map[string]attr.Value{
		"input_column":           maskingPolicyColumnList([]maskingPolicyColumn{{Name: "email", Type: "character varying(256)"}}),
		"expression":             types.StringValue(text),
		"definition_fingerprint": types.StringValue(definitionFingerprint(text)),
	}, fullCatalog())
}

// TestMaskingPolicyAttachmentLookup reports an attachment with the paired resource's identity, including columns,
// and a missing one as exists = false without an identity.
func TestMaskingPolicyAttachmentLookup(t *testing.T) {
	model := maskingTestAttachment(nil)
	model.Priority, model.InputColumns = types.Int64Null(), types.ListNull(types.StringType)
	for _, attached := range []bool{true, false} {
		c := fullCatalog()
		fakeState[*maskingFake](c, maskingFakeFamily).attached = attached
		source := newMaskingPolicyAttachmentDataSource()
		state, diagnostics := readSource(t, source, maskingAttachmentLookupConfig(t, model), c)
		require.False(t, diagnostics.HasError(), "%v", diagnostics)
		var data types.Object
		require.False(t, state.Get(context.Background(), &data).HasError())
		assert.Equal(t, types.BoolValue(attached), data.Attributes()["exists"])
		assert.Empty(t, c.writes, "lookups never mutate")
		if !attached {
			assert.True(t, data.Attributes()["id"].IsNull())
			continue
		}
		client := testResourceClient(nil)
		assert.Equal(t, client.identity("admin", maskingAttachmentIdentity(model)), data.Attributes()["id"])
		assert.Equal(t, types.Int64Value(10), data.Attributes()["priority"])
		assert.Equal(t, []string{"email"}, maskingAttachmentNames(data.Attributes()["input_columns"].(types.List)))
	}
}

// maskingAttachmentLookupConfig renders the lookup configuration of model with null computed values.
func maskingAttachmentLookupConfig(t *testing.T, model maskingPolicyAttachmentModel) types.Object {
	t.Helper()
	source := newMaskingPolicyAttachmentDataSource()
	data := catalogLookupObject(t, source, map[string]string{
		"database": model.Database.ValueString(), "policy": model.Policy.ValueString(), "schema": model.Schema.ValueString(),
		"relation": model.Relation.ValueString(), "grantee": model.Grantee.ValueString(), "grantee_type": model.GranteeType.ValueString(),
	})
	lookupValue(&data, "columns", model.Columns)
	return data
}

// TestMaskingPoliciesListing lists policies with the documented JSON forms decoded, filtered by database.
func TestMaskingPoliciesListing(t *testing.T) {
	for _, filters := range []map[string]string{{}, {"database": "analytics"}} {
		source := newMaskingPoliciesDataSource()
		state, diagnostics := readSource(t, source, collectionConfig(t, source, filters), fullCatalog())
		require.False(t, diagnostics.HasError(), "%v", diagnostics)
		var data types.Object
		require.False(t, state.Get(context.Background(), &data).HasError())
		items := collectionResult(source, data).Elements()
		require.Len(t, items, 1)
		item := items[0].(types.Object)
		assert.Equal(t, "mask_email", objectString(item, "name"))
		assert.Equal(t, "CAST('***' AS TEXT)", objectString(item, "expression"))
		database := "admin"
		if selected, ok := filters["database"]; ok {
			database = selected
		}
		assert.Equal(t, database, objectString(item, "database"))
	}
	empty := fullCatalog()
	fakeState[*maskingFake](empty, maskingFakeFamily).policy = false
	source := newMaskingPoliciesDataSource()
	state, diagnostics := readSource(t, source, collectionConfig(t, source, nil), empty)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	var data types.Object
	require.False(t, state.Get(context.Background(), &data).HasError())
	assert.Empty(t, collectionResult(source, data).Elements())
}

// TestMaskingPoliciesListingFailures reports catalog errors and undecodable rows instead of a partial list.
func TestMaskingPoliciesListingFailures(t *testing.T) {
	for name, client := range map[string]sqlclient.Client{
		"catalog error": queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
			return nil, errors.New("catalog unavailable")
		}),
		"malformed inputs": queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
			return []sqlclient.Row{{"policy_database": "admin", "policy_name": "p", "input_columns": "email", "policy_expression": "x"}}, nil
		}),
	} {
		t.Run(name, func(t *testing.T) {
			source := newMaskingPoliciesDataSource()
			_, diagnostics := readSource(t, source, collectionConfig(t, source, nil), client)
			assert.True(t, diagnostics.HasError())
		})
	}
}

// TestMaskingPolicyOutputsDescribeCatalogSpelling gives the lookup and the listing element the same catalog-facing
// descriptions of the inputs, instead of the resource's rules for configured spellings.
func TestMaskingPolicyOutputsDescribeCatalogSpelling(t *testing.T) {
	inputs := func(source datasource.DataSource, collection string) schema.ListNestedAttribute {
		var response datasource.SchemaResponse
		source.Schema(context.Background(), datasource.SchemaRequest{}, &response)
		attributes := response.Schema.Attributes
		if collection != "" {
			attributes = attributes[collection].(schema.ListNestedAttribute).NestedObject.Attributes
		}
		return attributes["input_column"].(schema.ListNestedAttribute)
	}
	for name, column := range map[string]schema.ListNestedAttribute{
		"lookup":  inputs(newMaskingPolicyDataSource(), ""),
		"listing": inputs(newMaskingPoliciesDataSource(), "masking_policies"),
	} {
		assert.Equal(t, maskingPolicyOutputDescriptions["input_column"], column.MarkdownDescription, name)
		assert.Equal(t, maskingPolicyOutputDescriptions["input_column.type"], column.NestedObject.Attributes["type"].GetMarkdownDescription(), name)
	}
}
