package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newCopyJobDataSource)

// newCopyJobDataSource reads one copy job's definition, AUTO setting and IAM role without owning it.
func newCopyJobDataSource() datasource.DataSource {
	return lookupDescriptions(newCatalogDataSource(catalogSpec{
		name: "copy_job", factory: newCopyJobResource, computed: []string{"iam_role", "auto", "source", "format", "format_argument", "parameters"},
		identityFields: []string{"name"}, identityDatabase: "database",
		lookup: func(ctx context.Context, client *resourceClient, data *types.Object) (bool, error) {
			attributes := data.Attributes()
			model := copyJobModel{Database: attributes["database"].(types.String), Name: attributes["name"].(types.String), IAMRole: types.StringNull(), Parameters: types.MapNull(types.StringType)}

			found, err := readCopyJob(ctx, client, &model)
			if err == nil && found {
				lookupValue(data, "iam_role", model.IAMRole)
				lookupValue(data, "auto", model.Auto)
				lookupValue(data, "table", model.Table)
				lookupValue(data, "source", model.Source)
				lookupValue(data, "format", model.Format)
				lookupValue(data, "format_argument", model.FormatArgument)
				lookupValue(data, "parameters", model.Parameters)
			}

			return found, err
		},
	}), copyJobOutputDescriptions)
}

// copyJobDefinitionReadback makes the engine-specific definition requirement explicit on lookup outputs.
const copyJobDefinitionReadback = " Read from SYS_COPY_JOB.copy_query; unavailable or unsupported definitions return an error."

// copyJobOutputDescriptions describe what the lookup can observe instead of the resource's apply-time behavior.
var copyJobOutputDescriptions = map[string]string{
	"name":            "Job name.",
	"iam_role":        "IAM role ARN the job's COPY command uses, as `SYS_COPY_JOB` reports it.",
	"auto":            "Whether Redshift loads new files automatically (`AUTO ON`).",
	"source":          "Amazon S3 prefix the job monitors." + copyJobDefinitionReadback,
	"format":          "Data format of the COPY command." + copyJobDefinitionReadback,
	"format_argument": "Argument of the format." + copyJobDefinitionReadback,
	"parameters":      "COPY parameters of the job." + copyJobDefinitionReadback,
	"table":           "Target table of the COPY command." + copyJobDefinitionReadback,
}
