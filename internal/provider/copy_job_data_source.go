package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newCopyJobDataSource)

// newCopyJobDataSource reads one copy job's AUTO setting and IAM role without owning it.
func newCopyJobDataSource() datasource.DataSource {
	return lookupDescriptions(newCatalogDataSource(catalogSpec{
		name: "copy_job", factory: newCopyJobResource, computed: []string{"iam_role", "auto", "source", "format", "format_argument", "parameters"},
		identityFields: []string{"name"}, identityDatabase: "database",
		lookup: func(ctx context.Context, client *resourceClient, data *types.Object) (bool, error) {
			attributes := data.Attributes()
			model := copyJobModel{Database: attributes["database"].(types.String), Name: attributes["name"].(types.String), IAMRole: types.StringNull()}
			found, err := readCopyJob(ctx, client, &model)
			if err == nil && found {
				lookupValue(data, "iam_role", model.IAMRole)
				lookupValue(data, "auto", model.Auto)
			}
			return found, err
		},
	}), copyJobOutputDescriptions)
}

// copyJobUnreadable describes the parts of the COPY command that SYS_COPY_JOB does not report in a readable form.
const copyJobUnreadable = " Not readable from the catalog, so the lookup reports null."

// copyJobOutputDescriptions describe what the lookup can observe instead of the resource's apply-time behavior.
var copyJobOutputDescriptions = map[string]string{
	"name":            "Job name.",
	"iam_role":        "IAM role ARN the job's COPY command uses, as `SYS_COPY_JOB` reports it.",
	"auto":            "Whether Redshift loads new files automatically (`AUTO ON`).",
	"source":          "Amazon S3 prefix the job monitors." + copyJobUnreadable,
	"format":          "Data format of the COPY command." + copyJobUnreadable,
	"format_argument": "Argument of the format." + copyJobUnreadable,
	"parameters":      "COPY parameters of the job." + copyJobUnreadable,
	"table":           "Target table of the COPY command." + copyJobUnreadable,
}
