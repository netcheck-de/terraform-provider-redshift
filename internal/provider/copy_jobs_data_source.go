package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newCopyJobsDataSource)

// newCopyJobsDataSource lists the copy jobs SYS_COPY_JOB shows in one database.
func newCopyJobsDataSource() datasource.DataSource {
	return newCollectionDataSource(collectionSpec{
		name:          "copy_jobs",
		databaseField: "database",
		description:   "Lists copy jobs from `SYS_COPY_JOB`. Superusers see every job; other users see only their own.",
		filters: map[string]schema.Attribute{
			"database": schema.StringAttribute{Optional: true, MarkdownDescription: "Database whose jobs are listed; the provider database when omitted."},
		},
		element: describedOutputs(collectionElement(newCopyJobResource, "database", "name", "iam_role", "auto"), map[string]string{
			"name":     copyJobOutputDescriptions["name"],
			"iam_role": copyJobOutputDescriptions["iam_role"],
			"auto":     copyJobOutputDescriptions["auto"],
		}),
		list: func(ctx context.Context, client *resourceClient, filters types.Object) ([]map[string]attr.Value, error) {
			database := objectString(filters, "database")
			if database == "" {
				database = client.database.ValueString()
			}

			rows, err := client.selectRows(ctx, database, listCopyJobsQuery())
			if err != nil {
				return nil, err
			}

			items := make([]map[string]attr.Value, 0, len(rows))
			for _, row := range rows {
				auto, err := copyJobAutoValue(row["is_auto"])
				if err != nil {
					return nil, err
				}

				items = append(items, map[string]attr.Value{
					"database": types.StringValue(database), "name": types.StringValue(strings.TrimSpace(row["job_name"])),
					"iam_role": types.StringValue(strings.TrimSpace(row["iam_role"])), "auto": types.BoolValue(auto),
				})
			}

			return items, nil
		},
	})
}
