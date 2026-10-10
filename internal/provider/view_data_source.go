package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newViewDataSource)

// viewLookupFingerprintDescription replaces the resource's drift wording, since a lookup only reports the hash.
const viewLookupFingerprintDescription = "SHA-256 of the catalog definition with whitespace collapsed; equals the paired resource's `definition_fingerprint` for the same definition."

// viewLookupDescriptions replaces the paired resource's descriptions of the given outputs. The resource text
// covers apply, import, and state behavior that a read-only lookup does not have.
func viewLookupDescriptions(source datasource.DataSource, descriptions map[string]string) datasource.DataSource {
	lookup := source.(*catalogDataSource)
	for name, description := range descriptions {
		switch attribute := lookup.attributes[name].(type) {
		case schema.StringAttribute:
			attribute.MarkdownDescription = description
			lookup.attributes[name] = attribute
		case schema.BoolAttribute:
			attribute.MarkdownDescription = description
			lookup.attributes[name] = attribute
		case schema.ListAttribute:
			attribute.MarkdownDescription = description
			lookup.attributes[name] = attribute
		default:
			// A misspelled or retyped output must fail at schema construction rather than keep the resource text.
			panic("viewLookupDescriptions: unsupported or missing attribute " + name)
		}
	}
	return lookup
}

// newViewDataSource reads an ordinary or late-binding view's definition, binding mode, and owner without
// managing the view.
func newViewDataSource() datasource.DataSource {
	return viewLookupDescriptions(newCatalogDataSource(catalogSpec{name: "view", factory: newViewResource, computed: []string{"query"}, identityFields: []string{"schema", "name"}, identityDatabase: "database", lookup: func(ctx context.Context, client *resourceClient, data *types.Object) (bool, error) {
		attributes := data.Attributes()
		model := viewModel{ID: types.StringNull(), Database: attributes["database"].(types.String), Schema: attributes["schema"].(types.String), Name: attributes["name"].(types.String)}
		entry, found, err := (&viewResource{*client}).read(ctx, model)
		if err == nil && found {
			lookupValue(data, "query", types.StringValue(entry.definition))
			lookupValue(data, "late_binding", types.BoolValue(entry.lateBinding))
			lookupValue(data, "owner", types.StringValue(entry.owner))
			lookupValue(data, "definition_fingerprint", types.StringValue(definitionFingerprint(entry.definition)))
		}
		return found, err
	}}), map[string]string{
		"query":                  "Catalog definition from `pg_views`, as Redshift prints it.",
		"late_binding":           "Whether the view is late-binding (`WITH NO SCHEMA BINDING`).",
		"owner":                  "SQL user owning the view.",
		"definition_fingerprint": viewLookupFingerprintDescription,
	})
}
