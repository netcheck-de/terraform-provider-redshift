package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newRlsPolicyAttachmentsDataSource)

// rlsPolicyAttachmentOutputDescriptions describe the listed values instead of the resource's configuration advice.
var rlsPolicyAttachmentOutputDescriptions = map[string]string{
	"database":     "Database containing the policy and the relation.",
	"policy":       "Attached RLS policy.",
	"schema":       "Schema of the relation.",
	"relation":     "Table or view the policy filters.",
	"grantee":      "User or role the policy applies to, or `public`.",
	"grantee_type": "`USER`, `ROLE`, or `PUBLIC`.",
}

// newRlsPolicyAttachmentsDataSource lists the RLS policy attachments of one database with one catalog query.
func newRlsPolicyAttachmentsDataSource() datasource.DataSource {
	return newCollectionDataSource(collectionSpec{
		name:          "rls_policy_attachments",
		databaseField: "database",
		description:   "Lists the row-level security policy attachments of one database from `svv_rls_attached_policy`, including attachments Terraform does not manage. Only superusers and `sys:secadmin` holders can query the view.",
		filters: map[string]schema.Attribute{
			"database": schema.StringAttribute{Optional: true, MarkdownDescription: "Database whose attachments are listed; defaults to the provider database."},
			"policy":   schema.StringAttribute{Optional: true, MarkdownDescription: "Only attachments of this policy; all policies when omitted."},
			"schema":   schema.StringAttribute{Optional: true, MarkdownDescription: "Only attachments to relations of this schema; all schemas when omitted."},
			"relation": schema.StringAttribute{Optional: true, MarkdownDescription: "Only attachments to relations of this name; all relations when omitted."},
		},
		element: describedOutputs(collectionElement(newRlsPolicyAttachmentResource, "database", "policy", "schema", "relation", "grantee", "grantee_type"), rlsPolicyAttachmentOutputDescriptions),
		list: func(ctx context.Context, client *resourceClient, filters types.Object) ([]map[string]attr.Value, error) {
			database := discoveryDatabase(client, filters)
			query := listRlsPolicyAttachmentsQuery(objectString(filters, "policy"), objectString(filters, "schema"), objectString(filters, "relation"))

			rows, err := client.selectRows(ctx, database, query)
			if err != nil {
				return nil, err
			}

			items := make([]map[string]attr.Value, 0, len(rows))
			for _, row := range rows {
				grantee, kind := rlsPolicyAttachmentRecipient(row["grantee"], row["granteekind"])
				items = append(items, map[string]attr.Value{
					"database": types.StringValue(database), "policy": types.StringValue(row["polname"]),
					"schema": types.StringValue(row["relschema"]), "relation": types.StringValue(row["relname"]),
					"grantee": types.StringValue(grantee), "grantee_type": types.StringValue(kind),
				})
			}

			return items, nil
		},
	})
}
