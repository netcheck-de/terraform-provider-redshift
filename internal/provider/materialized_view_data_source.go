package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newMaterializedViewDataSource)

// materializedViewLookupStorageDescription explains why the storage outputs stay null: SVV_TABLE_INFO, the view
// ALTER MATERIALIZED VIEW points to, is superuser-only, omits empty relations, and names only the first sort key.
// https://docs.aws.amazon.com/redshift/latest/dg/r_SVV_TABLE_INFO.html
const materializedViewLookupStorageDescription = "Always null: no catalog view that every user can read reports it."

// newMaterializedViewDataSource reads a materialized view's definition, refresh setting, and owner without
// managing it. Storage options have no catalog source every user can read, so they stay null.
func newMaterializedViewDataSource() datasource.DataSource {
	return lookupDescriptions(newCatalogDataSource(catalogSpec{name: "materialized_view", factory: newMaterializedViewResource, computed: []string{"query", "backup"}, identityFields: []string{"schema", "name"}, identityDatabase: "database", lookup: func(ctx context.Context, client *resourceClient, data *types.Object) (bool, error) {
		attributes := data.Attributes()
		model := materializedViewModel{ID: types.StringNull(), Database: attributes["database"].(types.String), Schema: attributes["schema"].(types.String), Name: attributes["name"].(types.String)}
		observed, found, err := (&materializedViewResource{*client}).read(ctx, model)
		if err == nil && found {
			lookupValue(data, "query", types.StringValue(observed.view.definition))
			if !observed.refreshHidden {
				lookupValue(data, "auto_refresh", types.BoolValue(observed.autoRefresh))
			}
			lookupValue(data, "owner", types.StringValue(observed.view.owner))
			lookupValue(data, "definition_fingerprint", types.StringValue(definitionFingerprint(observed.view.definition)))
		}
		return found, err
	}}), map[string]string{
		"query":                  "Catalog definition from `pg_views`: the complete `CREATE MATERIALIZED VIEW` statement as Redshift prints it.",
		"backup":                 materializedViewLookupStorageDescription,
		"distribution":           materializedViewLookupStorageDescription,
		"distribution.style":     "Distribution style: `EVEN`, `ALL`, or `KEY`.",
		"distribution.key":       "Distribution key column.",
		"sort_key":               materializedViewLookupStorageDescription,
		"sort_key.columns":       "Compound sort key columns in order.",
		"auto_refresh":           "Whether the materialized view refreshes automatically, from `SVV_MV_INFO`. Null when `SVV_MV_INFO` hides the view from a provider identity that neither owns it nor is a superuser.",
		"owner":                  "SQL user owning the materialized view.",
		"definition_fingerprint": viewLookupFingerprintDescription,
	})
}
