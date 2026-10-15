package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newMaskingPoliciesDataSource)

// newMaskingPoliciesDataSource lists masking policies, optionally of one database.
func newMaskingPoliciesDataSource() datasource.DataSource {
	return newCollectionDataSource(collectionSpec{
		name:        "masking_policies",
		description: "Lists dynamic data masking policies from `SVV_MASKING_POLICY`, which covers every database of the warehouse. Only superusers and `sys:secadmin` members see policies.",
		filters: map[string]schema.Attribute{
			"database": schema.StringAttribute{Optional: true, MarkdownDescription: "Only policies of this database; all databases when omitted."},
		},
		element: describedOutputs(collectionElement(newMaskingPolicyResource, "database", "name", "input_column", "expression", "definition_fingerprint"), maskingPolicyOutputDescriptions),
		list: func(ctx context.Context, client *resourceClient, filters types.Object) ([]map[string]attr.Value, error) {
			rows, err := client.selectRows(ctx, client.database.ValueString(), listMaskingPoliciesQuery(objectString(filters, "database")))
			if err != nil {
				return nil, err
			}
			items := make([]map[string]attr.Value, 0, len(rows))
			for _, row := range rows {
				columns, err := maskingPolicyParseColumns(row["input_columns"])
				if err != nil {
					return nil, err
				}
				text := maskingPolicyExpressionText(row["policy_expression"])
				items = append(items, map[string]attr.Value{
					"database": types.StringValue(row["policy_database"]), "name": types.StringValue(row["policy_name"]),
					"input_column": maskingPolicyColumnList(columns), "expression": types.StringValue(text),
					"definition_fingerprint": types.StringValue(definitionFingerprint(text)),
				})
			}
			return items, nil
		},
	})
}
