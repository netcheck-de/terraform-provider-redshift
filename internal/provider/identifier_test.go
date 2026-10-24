package provider

import (
	"context"
	"slices"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFoldedNameValidator rejects known names with uppercase letters and leaves lowercase, principal-form, null, and
// unknown values alone.
func TestFoldedNameValidator(t *testing.T) {
	for value, invalid := range map[types.String]bool{
		types.StringValue("Analytics"):  true,
		types.StringValue("analytics"):  false,
		types.StringValue("sales_2026"): false,
		types.StringValue("aad:Sales"):  false,
		types.StringNull():              false,
		types.StringUnknown():           false,
	} {
		var resp validator.StringResponse
		foldedNameValidator{}.ValidateString(context.Background(), validator.StringRequest{Path: path.Root("name"), ConfigValue: value}, &resp)
		assert.Equal(t, invalid, resp.Diagnostics.HasError(), "%s", value)
	}
}

// identifierAttributes name objects that Redshift folds to lowercase wherever a resource declares them.
var identifierAttributes = []string{
	"database", "database_name", "schema", "schema_name", "name", "datashare", "datashare_name", "table", "relation",
	"policy", "policy_name", "object_name", "column_name", "constraint_name", "function", "group",
}

// foldedNameExemptions are identifier attributes that compare without case or keep their case in Redshift.
var foldedNameExemptions = map[string][]string{
	// User names and identity-provider principals keep their case.
	"redshift_user": {"name"},
	// Remote database@catalog aliases are resolved by the remote catalog.
	"redshift_debug_grant": {"database"},
	// These compare names without case or validate them in ValidateConfig.
	"redshift_external_table":     {"schema", "name"},
	"redshift_external_partition": {"schema", "table"},
	"redshift_table":              {"schema", "name"},
	"redshift_template":           {"schema", "name"},
}

// TestEveryIdentifierRejectsUppercase keeps new resources from accepting mixed-case object names that Redshift
// would create under another spelling than every catalog read binds.
func TestEveryIdentifierRejectsUppercase(t *testing.T) {
	for _, factory := range registeredResources() {
		r := factory()

		var metadata resource.MetadataResponse
		r.Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "redshift"}, &metadata)

		var response resource.SchemaResponse
		r.Schema(context.Background(), resource.SchemaRequest{}, &response)

		for name, attribute := range response.Schema.Attributes {
			text, ok := attribute.(schema.StringAttribute)
			if !ok || !slices.Contains(identifierAttributes, name) || text.Computed && !text.Optional {
				continue
			}

			folded := slices.ContainsFunc(text.Validators, func(v validator.String) bool { _, ok := v.(foldedNameValidator); return ok })
			exempt := slices.Contains(foldedNameExemptions[metadata.TypeName], name)
			assert.NotEqual(t, exempt, folded, "%s.%s: exempt=%v", metadata.TypeName, name, exempt)
		}
	}
}

// TestLookupRejectsUppercase applies the same check to lookup inputs, which copy the resource validators.
func TestLookupRejectsUppercase(t *testing.T) {
	d := newFunctionDataSource()

	var response datasource.SchemaResponse
	d.Schema(context.Background(), datasource.SchemaRequest{}, &response)
	attribute, ok := response.Schema.Attributes["name"].(interface{ StringValidators() []validator.String })
	require.True(t, ok)
	assert.True(t, slices.ContainsFunc(attribute.StringValidators(), func(v validator.String) bool { _, ok := v.(foldedNameValidator); return ok }))
}
