package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newModelsDataSource)

// newModelsDataSource lists the machine learning models of one database, trained and external alike.
func newModelsDataSource() datasource.DataSource {
	return newCollectionDataSource(collectionSpec{
		name:        "models",
		description: "Lists the machine learning models of one database that the provider's SQL identity can use, trained and external models alike, from `SHOW MODEL ALL`.",
		filters: map[string]schema.Attribute{
			"database": discoveryDatabaseFilter("models"),
			"schema":   discoveryFilter("Only models in this schema."),
		},
		element: map[string]schema.Attribute{
			"database": discoveryComputed("string", "Database containing the model."),
			"schema":   discoveryComputed("string", "Schema containing the model and its prediction or inference function."),
			"name":     discoveryComputed("string", "Model name."),
		},
		list: modelsList,
	})
}

// modelsList runs SHOW MODEL ALL in the selected database, which it covers alone, and filters the schema in Go
// because SHOW MODEL has no filter clause.
func modelsList(ctx context.Context, client *resourceClient, filters types.Object) ([]map[string]attr.Value, error) {
	database, schemaName := discoveryDatabase(client, filters), objectString(filters, "schema")

	rows, err := client.queryDatabase(ctx, database, listModelsStatement(), nil)
	if err != nil {
		return nil, err
	}

	models, err := parseShowModels(rows)
	if err != nil {
		return nil, err
	}

	items := make([]map[string]attr.Value, 0, len(models))
	for _, model := range models {
		if schemaName != "" && model.schema != schemaName {
			continue
		}

		items = append(items, map[string]attr.Value{
			"database": types.StringValue(database), "schema": types.StringValue(model.schema), "name": types.StringValue(model.name),
		})
	}

	return items, nil
}
