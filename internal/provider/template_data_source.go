package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newTemplateDataSource)

// newTemplateDataSource reads one COPY template's options and owner without owning it.
func newTemplateDataSource() datasource.DataSource {
	return lookupDescriptions(newCatalogDataSource(catalogSpec{
		name: "template", factory: newTemplateResource, computed: []string{"owner", "format", "format_argument", "parameters"},
		identityFields: []string{"schema", "name"}, identityDatabase: "database",
		lookup: func(ctx context.Context, client *resourceClient, data *types.Object) (bool, error) {
			attributes := data.Attributes()
			model := templateModel{
				ID: types.StringNull(), Database: attributes["database"].(types.String), Schema: attributes["schema"].(types.String), Name: attributes["name"].(types.String),
				Owner: types.StringNull(), Format: types.StringNull(), FormatArgument: types.StringNull(), Parameters: types.MapNull(types.StringType),
			}
			entry, found, err := readTemplateCatalog(ctx, client, model)
			if err == nil && found {
				entry.applyCatalog(&model)
				lookupValue(data, "owner", model.Owner)
				lookupValue(data, "format", model.Format)
				lookupValue(data, "format_argument", model.FormatArgument)
				lookupValue(data, "parameters", model.Parameters)
			}
			return found, err
		},
	}), templateOutputDescriptions)
}

// templateOutputDescriptions describe the catalog values instead of the resource's apply-time behavior.
var templateOutputDescriptions = map[string]string{
	"name":            "Template name.",
	"owner":           "SQL user owning the template.",
	"format":          "Data format of `FORMAT AS`, such as `JSON`; null for pipe-delimited text.",
	"format_argument": "Argument of the format, such as `auto` for `JSON 'auto'`; null without one.",
	"parameters":      "COPY parameters keyed by their UPPERCASE keyword, as `SHOW TEMPLATE` prints them, with `\"\"` for keywords without an argument; null when the template has none.",
}
