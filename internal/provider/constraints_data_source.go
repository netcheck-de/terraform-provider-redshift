package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newConstraintsDataSource)

// newConstraintsDataSource lists primary key, unique, and foreign key constraints.
func newConstraintsDataSource() datasource.DataSource {
	return newCollectionDataSource(collectionSpec{
		name:        "constraints",
		description: "Lists the primary key, unique, and foreign key constraints of one database's tables from `PG_CONSTRAINT`, with key columns in key order. Redshift does not enforce these constraints, but the query planner relies on them.",
		filters: map[string]schema.Attribute{
			"database":        discoveryDatabaseFilter("constraints"),
			"schema":          discoveryFilter("Only constraints of tables in this schema."),
			"table":           discoveryFilter("Only constraints of tables with this name."),
			"constraint_type": discoveryFilter("Only constraints of this type: `PRIMARY KEY`, `UNIQUE`, or `FOREIGN KEY`.", constraintsNames...),
		},
		element: map[string]schema.Attribute{
			"database":           discoveryComputed("string", "Database containing the table."),
			"schema":             discoveryComputed("string", "Schema containing the table."),
			"table":              discoveryComputed("string", "Table the constraint belongs to."),
			"name":               discoveryComputed("string", "Constraint name, as used by `redshift_comment` with `object_type = \"CONSTRAINT\"`."),
			"constraint_type":    discoveryComputed("string", "`PRIMARY KEY`, `UNIQUE`, or `FOREIGN KEY`."),
			"columns":            discoveryComputed("list", "Constrained columns in key order."),
			"referenced_schema":  discoveryComputed("string", "Schema of the table a foreign key references; null for other constraints."),
			"referenced_table":   discoveryComputed("string", "Table a foreign key references; null for other constraints."),
			"referenced_columns": discoveryComputed("list", "Referenced columns of a foreign key, paired with `columns` by position; null for other constraints."),
			"definition":         discoveryComputed("string", "Constraint definition as `pg_get_constraintdef` renders it, such as `PRIMARY KEY (id)`."),
		},
		list: constraintsList,
	})
}

// constraintsList reads constraints inside the selected database, because pg_constraint is per database.
func constraintsList(ctx context.Context, client *resourceClient, filters types.Object) ([]map[string]attr.Value, error) {
	database := discoveryDatabase(client, filters)
	query, err := readConstraintsQuery(objectString(filters, "schema"), objectString(filters, "table"), objectString(filters, "constraint_type"))
	if err != nil {
		return nil, err
	}
	rows, err := client.selectRows(ctx, database, query)
	if err != nil {
		return nil, err
	}
	items := make([]map[string]attr.Value, 0, len(rows))
	for _, row := range rows {
		kind := row["constraint_type"]
		if _, ok := constraintsTypes[kind]; !ok || row["schema_name"] == "" || row["table_name"] == "" || row["constraint_name"] == "" {
			return nil, fmt.Errorf("pg_constraint returned an incomplete constraint row in database %q", database)
		}
		foreign := kind == "FOREIGN KEY"
		columns, referenced, err := constraintsKeyColumns(row["definition"], foreign)
		if err != nil {
			return nil, err
		}
		item := map[string]attr.Value{
			"database":           types.StringValue(database),
			"schema":             types.StringValue(row["schema_name"]),
			"table":              types.StringValue(row["table_name"]),
			"name":               types.StringValue(row["constraint_name"]),
			"constraint_type":    types.StringValue(kind),
			"columns":            constraintsStrings(columns),
			"referenced_schema":  types.StringNull(),
			"referenced_table":   types.StringNull(),
			"referenced_columns": types.ListNull(types.StringType),
			"definition":         types.StringValue(row["definition"]),
		}
		if foreign {
			item["referenced_schema"], item["referenced_table"] = discoveryText(row["referenced_schema"]), discoveryText(row["referenced_table"])
			item["referenced_columns"] = constraintsStrings(referenced)
		}
		items = append(items, item)
	}
	return items, nil
}

// constraintsStrings converts parsed column names to a Terraform list.
func constraintsStrings(names []string) types.List {
	values := make([]attr.Value, 0, len(names))
	for _, name := range names {
		values = append(values, types.StringValue(name))
	}
	return types.ListValueMust(types.StringType, values)
}
