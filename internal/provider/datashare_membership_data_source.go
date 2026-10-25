package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newDatashareMembershipDataSource)

// newDatashareMembershipDataSource reports whether a share holds every current member of one kind in a schema.
func newDatashareMembershipDataSource() datasource.DataSource {
	return lookupDescriptions(newCatalogDataSource(catalogSpec{name: "datashare_membership", factory: newDatashareMembershipResource, computed: []string{"all"}, exists: true, identityFields: []string{"datashare", "schema", "kind"}, identityDatabase: "database", lookup: func(ctx context.Context, client *resourceClient, data *types.Object) (bool, error) {
		attributes := data.Attributes()
		model := datashareMembershipModel{Database: attributes["database"].(types.String), Datashare: attributes["datashare"].(types.String), Schema: attributes["schema"].(types.String), Kind: attributes["kind"].(types.String)}

		found, _, _, err := (&datashareMembershipResource{*client}).read(ctx, &model)
		if !found {
			model.All = types.BoolValue(false)
		}

		lookupValue(data, "all", model.All)

		return found, err
	}}), map[string]string{
		"schema": "Producer schema whose visible membership is observed. Periods in schema or visible source member names are unsupported because the catalog does not quote qualification. Future-object inclusion may be enabled or disabled.",
		"kind":   "TABLES observes visible tables and views; FUNCTIONS observes visible SQL UDF overloads.",
		"all":    "Whether every currently visible source object of the selected kind has a visible share membership. True for an empty visible source set; false when the schema membership is not visible. This observation does not prove complete coverage under restricted catalog visibility and does not require a SQL superuser.",
	})
}
