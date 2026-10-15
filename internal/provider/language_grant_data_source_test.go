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
	user := map[string]string{"database_name": "warehouse", "language_name": "SQL", "grantee": "analyst", "grantee_type": "USER"}
	exerciseCatalogLookup(t, newLanguageGrantDataSource, user, map[string]attr.Value{"privileges": usage, "grant_option_privileges": usage}, &privilegeCatalog{values: map[string]bool{"USAGE": true}, admin: "t", grantee: "analyst", kind: "user"})
}

// TestLanguageGrantLookupCanonicalIdentity accepts a language in any case, keeps it as configured, and records the
// canonical uppercase spelling in the identity, as Create does.
func TestLanguageGrantLookupCanonicalIdentity(t *testing.T) {
	fields := map[string]string{"database_name": "warehouse", "language_name": "sql", "grantee": "analyst", "grantee_type": "USER"}
	source := newLanguageGrantDataSource()
	state, diagnostics := readSource(t, source, catalogLookupObject(t, source, fields), &privilegeCatalog{values: map[string]bool{"USAGE": true}, grantee: "analyst", kind: "user"})
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	var observed types.Object
	require.False(t, state.Get(context.Background(), &observed).HasError())
	assert.Equal(t, types.StringValue("sql"), observed.Attributes()["language_name"])
	fields["language_name"] = "SQL"
	assertLookupIdentity(t, observed.Attributes()["id"].(types.String), "admin", fields)
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
