package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ = registerDataSource(newRlsPolicyDataSource)
	_ = registerDataSource(newRlsPoliciesDataSource)
)

// rlsPolicyLookupDescriptions replaces the resource's configuration and replacement advice, which does not apply to
// the read-only columns and alias that the lookup and the listing observe.
var rlsPolicyLookupDescriptions = map[string]string{
	"column":      "Ordered `WITH` columns of the policy in the spelling that `svv_rls_policy` reports. Null when the policy has no `WITH` clause.",
	"column.type": "Redshift data type in the catalog's canonical form, in uppercase, such as `CHARACTER VARYING(64)`.",
	"alias":       "Relation alias of the `WITH` clause (`AS alias`) that the predicate may use to qualify columns, or null.",
	"predicate":   "Filter expression of the `USING ( ... )` clause in the rewritten form that Redshift stores.",
}

// newRlsPolicyDataSource reads one RLS policy definition without taking ownership of it.
func newRlsPolicyDataSource() datasource.DataSource {
	return lookupDescriptions(newCatalogDataSource(catalogSpec{name: "rls_policy", factory: newRlsPolicyResource, computed: []string{"alias", "predicate"}, identityFields: []string{"name"}, identityDatabase: "database", lookup: func(ctx context.Context, client *resourceClient, data *types.Object) (bool, error) {
		attributes := data.Attributes()
		model := rlsPolicyModel{Database: attributes["database"].(types.String), Name: attributes["name"].(types.String), Column: types.ListNull(rlsPolicyColumnType), Alias: types.StringNull(), Predicate: types.StringNull()}
		found, predicate, err := (&rlsPolicyResource{*client}).read(ctx, &model)
		if err == nil && found {
			model.Predicate, model.DefinitionFingerprint = reconcileDefinition(model.Predicate, types.StringNull(), predicate)
			lookupValue(data, "column", model.Column)
			lookupValue(data, "alias", model.Alias)
			lookupValue(data, "predicate", model.Predicate)
			lookupValue(data, "definition_fingerprint", model.DefinitionFingerprint)
		}
		return found, err
	}}), rlsPolicyLookupDescriptions)
}

// newRlsPoliciesDataSource lists the RLS policies of one database.
func newRlsPoliciesDataSource() datasource.DataSource {
	return newCollectionDataSource(collectionSpec{
		name:        "rls_policies",
		description: "Lists the row-level security policies of one database from `svv_rls_policy`, including policies Terraform does not manage. Only superusers and `sys:secadmin` holders see every policy.",
		filters: map[string]schema.Attribute{
			"database": schema.StringAttribute{Optional: true, MarkdownDescription: "Database whose policies are listed; defaults to the provider database."},
		},
		element: describedOutputs(collectionElement(newRlsPolicyResource, "database", "name", "column", "alias", "predicate", "definition_fingerprint"), rlsPolicyLookupDescriptions),
		list: func(ctx context.Context, client *resourceClient, filters types.Object) ([]map[string]attr.Value, error) {
			database := client.database.ValueString()
			if selected := objectString(filters, "database"); selected != "" {
				database = selected
			}
			rows, err := client.selectRows(ctx, database, listRlsPoliciesQuery(database))
			if err != nil {
				return nil, err
			}
			items := make([]map[string]attr.Value, 0, len(rows))
			for _, row := range rows {
				policy, err := rlsPolicyFromRow(row)
				if err != nil {
					return nil, err
				}
				items = append(items, map[string]attr.Value{
					"database": policy.Database, "name": policy.Name, "column": policy.Column, "alias": policy.Alias,
					"predicate": policy.Predicate, "definition_fingerprint": policy.DefinitionFingerprint,
				})
			}
			return items, nil
		},
	})
}
