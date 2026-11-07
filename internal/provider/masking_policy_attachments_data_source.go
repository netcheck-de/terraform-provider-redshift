package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newMaskingPolicyAttachmentsDataSource)

// maskingAttachmentOutputDescriptions describe the listed values instead of the resource's configuration advice.
var maskingAttachmentOutputDescriptions = map[string]string{
	"database":      "Database containing the policy and the relation.",
	"policy":        "Attached masking policy.",
	"schema":        "Schema of the relation.",
	"relation":      "Table or view whose columns are masked.",
	"grantee":       "User or role the policy applies to, or `public`.",
	"grantee_type":  "`USER`, `ROLE`, or `PUBLIC`.",
	"columns":       "Ordered output columns that the policy masks.",
	"input_columns": "Ordered relation columns passed to the policy's input columns.",
	"priority":      "Priority among the policies that apply to a column; the highest wins.",
}

// newMaskingPolicyAttachmentsDataSource lists the masking policy attachments of one database with one catalog query.
func newMaskingPolicyAttachmentsDataSource() datasource.DataSource {
	return newCollectionDataSource(collectionSpec{
		name:          "masking_policy_attachments",
		databaseField: "database",
		description: "Lists the dynamic data masking policy attachments of one database from `SVV_ATTACHED_MASKING_POLICY`, including attachments Terraform does not manage. " +
			"Superusers and users with the `sys:secadmin` role see every attachment; other users see none. " + discoveryLocalScope,
		filters: map[string]schema.Attribute{
			"database": discoveryDatabaseFilter("attachments"),
			"policy":   discoveryFilter("Only attachments of this policy; all policies when omitted."),
			"schema":   discoveryFilter("Only attachments to relations of this schema; all schemas when omitted."),
			"relation": discoveryFilter("Only attachments to relations of this name; all relations when omitted."),
		},
		element: describedOutputs(collectionElement(newMaskingPolicyAttachmentResource, "database", "policy", "schema", "relation", "grantee", "grantee_type", "columns", "input_columns", "priority"), maskingAttachmentOutputDescriptions),
		list: func(ctx context.Context, client *resourceClient, filters types.Object) ([]map[string]attr.Value, error) {
			database, local, err := discoveryLocalDatabase(ctx, client, filters, "database")
			if err != nil || !local {
				return []map[string]attr.Value{}, err
			}

			query := listMaskingAttachmentsQuery(objectString(filters, "policy"), objectString(filters, "schema"), objectString(filters, "relation"))

			rows, err := client.selectRows(ctx, database, query)
			if err != nil {
				return nil, err
			}

			items := make([]map[string]attr.Value, 0, len(rows))
			for _, row := range rows {
				priority, err := maskingAttachmentPriority(row["priority"])
				if err != nil {
					return nil, err
				}

				outputs, err := maskingAttachmentParseNames(row["output_columns"])
				if err != nil {
					return nil, err
				}

				inputs, err := maskingAttachmentParseNames(row["input_columns"])
				if err != nil {
					return nil, err
				}

				grantee, kind := maskingAttachmentRecipient(row["grantee"], row["grantee_type"])
				items = append(items, map[string]attr.Value{
					"database": types.StringValue(database), "policy": types.StringValue(row["policy_name"]),
					"schema": types.StringValue(row["schema_name"]), "relation": types.StringValue(row["table_name"]),
					"grantee": types.StringValue(grantee), "grantee_type": types.StringValue(kind),
					"columns": maskingAttachmentNameList(outputs), "input_columns": maskingAttachmentNameList(inputs),
					"priority": types.Int64Value(priority),
				})
			}

			return items, nil
		},
	})
}
