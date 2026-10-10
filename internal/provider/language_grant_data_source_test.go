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

var _ = registerParity(parityCase{source: newLanguageGrantDataSource, resource: newLanguageGrantResource, selectors: []string{"database_name", "language_name", "grantee", "grantee_type"}})

// TestLanguageGrantLookup observes a role's language USAGE and a user's grant option without reconciling them.
func TestLanguageGrantLookup(t *testing.T) {
	usage := types.SetValueMust(types.StringType, []attr.Value{types.StringValue("USAGE")})
	c := fullCatalog()
	c.localDB = true
	exerciseCatalogLookup(t, newLanguageGrantDataSource, languageGrantBaseFields, map[string]attr.Value{"privileges": usage, "grant_option_privileges": types.SetValueMust(types.StringType, nil)}, c)
	user := map[string]string{"database_name": "warehouse", "language_name": "sql", "grantee": "analyst", "grantee_type": "USER"}
	exerciseCatalogLookup(t, newLanguageGrantDataSource, user, map[string]attr.Value{"privileges": usage, "grant_option_privileges": usage}, &privilegeCatalog{values: map[string]bool{"USAGE": true}, admin: "t", grantee: "analyst", kind: "user"})
}

// TestLanguageGrantLookupSchema describes the observed grant options as read-only, not as the resource's argument.
func TestLanguageGrantLookupSchema(t *testing.T) {
	var resp datasource.SchemaResponse
	newLanguageGrantDataSource().Schema(context.Background(), datasource.SchemaRequest{}, &resp)
	options, ok := resp.Schema.Attributes["grant_option_privileges"]
	require.True(t, ok)
	assert.True(t, options.IsComputed())
	assert.False(t, options.IsOptional())
	assert.NotContains(t, options.GetMarkdownDescription(), "Defaults to")
	assert.NotContains(t, options.GetMarkdownDescription(), "Removing")
}
