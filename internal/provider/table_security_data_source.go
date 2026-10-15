package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newTableSecurityDataSource)

// newTableSecurityDataSource reads a relation's row-level security settings without owning them.
func newTableSecurityDataSource() datasource.DataSource {
	return newCatalogDataSource(catalogSpec{name: "table_security", factory: newTableSecurityResource, computed: []string{"row_level_security"}, identityFields: []string{"schema", "relation"}, identityDatabase: "database", lookup: func(ctx context.Context, client *resourceClient, data *types.Object) (bool, error) {
		attributes := data.Attributes()
		model := tableSecurityModel{Database: attributes["database"].(types.String), Schema: attributes["schema"].(types.String), Relation: attributes["relation"].(types.String)}
		found, err := (&tableSecurityResource{*client}).read(ctx, &model)
		if err == nil && found {
			lookupValue(data, "row_level_security", model.RowLevelSecurity)
			lookupValue(data, "conjunction_type", model.ConjunctionType)
			lookupValue(data, "datashare_row_level_security", model.DatashareRowLevelSecurity)
		}
		return found, err
	}})
}
