package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newCommentsDataSource)

// newCommentsDataSource lists the annotated objects of one database.
func newCommentsDataSource() datasource.DataSource {
	return newCollectionDataSource(collectionSpec{
		name:          "comments",
		databaseField: "database_name",
		description: "Lists the comments on the database itself and on its schemas, tables, views, columns, and constraints, from `PG_DESCRIPTION` joined to the catalog of each object type. " +
			"Objects without a comment are not listed. With metadata security enabled, objects the connection may not access are hidden from the catalog and their comments are not listed. " + discoveryLocalScope,
		filters: map[string]schema.Attribute{
			"database_name": discoveryFilter("Local database whose comments are listed; defaults to the provider's `database`."),
			"object_type": schema.StringAttribute{
				Optional: true, Validators: []validator.String{stringvalidator.OneOf(commentListKinds...)},
				MarkdownDescription: "Only comments on objects of this type: `DATABASE`, `SCHEMA`, `TABLE`, `VIEW`, `COLUMN`, or `CONSTRAINT`; every type when omitted.",
			},
			"schema_name": discoveryFilter("Only comments on tables, views, columns, and constraints in this schema; database and schema comments, which have no `schema_name`, are then left out."),
		},
		element: describedOutputs(collectionElement(newCommentResource, "database_name", "object_type", "schema_name", "object_name", "column_name", "constraint_name", "text"), map[string]string{
			"database_name":   "Database containing the annotated object.",
			"object_type":     "`DATABASE`, `SCHEMA`, `TABLE`, `VIEW`, `COLUMN`, or `CONSTRAINT`.",
			"schema_name":     "Schema of the table, view, column, or constraint; null for database and schema comments.",
			"object_name":     "Name of the database or schema itself; for `TABLE` and `VIEW` the relation; for `COLUMN` and `CONSTRAINT` the table that holds it.",
			"column_name":     "Annotated column of a `COLUMN` comment; null otherwise.",
			"constraint_name": "Annotated constraint of a `CONSTRAINT` comment; null otherwise.",
			"text":            "Comment text.",
		}),
		list: commentsList,
	})
}

// commentsList runs one query per selected object type in the selected database, at most one per supported type
// however many objects carry comments, and concatenates the results in type order.
func commentsList(ctx context.Context, client *resourceClient, filters types.Object) ([]map[string]attr.Value, error) {
	database, local, err := discoveryLocalDatabase(ctx, client, filters, "database_name")
	if err != nil || !local {
		return []map[string]attr.Value{}, err
	}

	schemaName := objectString(filters, "schema_name")

	kinds := commentListKinds
	if kind := objectString(filters, "object_type"); kind != "" {
		kinds = []string{kind}
	}

	items := []map[string]attr.Value{}

	for _, kind := range kinds {
		// Database and schema comments have no schema_name, so a schema filter never matches them.
		if schemaName != "" && (kind == "DATABASE" || kind == "SCHEMA") {
			continue
		}

		query, err := listCommentsQuery(kind, database, schemaName)
		if err != nil {
			return nil, err
		}

		rows, err := client.selectRows(ctx, database, query)
		if err != nil {
			return nil, err
		}

		for _, row := range rows {
			item := map[string]attr.Value{
				"database_name": types.StringValue(database), "object_type": types.StringValue(kind),
				"schema_name": types.StringNull(), "object_name": types.StringValue(row["object_name"]),
				"column_name": types.StringNull(), "constraint_name": types.StringNull(), "text": types.StringValue(row["text"]),
			}
			if kind != "DATABASE" && kind != "SCHEMA" {
				item["schema_name"] = types.StringValue(row["schema_name"])
			}

			if kind == "COLUMN" {
				item["column_name"] = types.StringValue(row["column_name"])
			}

			if kind == "CONSTRAINT" {
				item["constraint_name"] = types.StringValue(row["constraint_name"])
			}

			items = append(items, item)
		}
	}

	return items, nil
}
