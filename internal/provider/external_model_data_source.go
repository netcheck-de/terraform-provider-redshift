package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newExternalModelDataSource)

// externalModelLookupSettings are the outputs SHOW MODEL may report; each is null when its key is absent.
var externalModelLookupSettings = []string{"function", "iam_role", "model_id", "prompt", "suffix", "request_type", "response_type"}

// newExternalModelDataSource reads what SHOW MODEL reports about a model without managing it.
func newExternalModelDataSource() datasource.DataSource {
	return lookupDescriptions(newCatalogDataSource(catalogSpec{name: "external_model", factory: newExternalModelResource, computed: externalModelLookupSettings, identityFields: []string{"schema", "name"}, identityDatabase: "database", lookup: func(ctx context.Context, client *resourceClient, data *types.Object) (bool, error) {
		attributes := data.Attributes()
		model := externalModelModel{
			ID: types.StringNull(), Database: attributes["database"].(types.String), Schema: attributes["schema"].(types.String), Name: attributes["name"].(types.String),
			Function: types.StringNull(), IAMRole: types.StringNull(), ModelID: types.StringNull(), Prompt: types.StringNull(), Suffix: types.StringNull(),
			RequestType: types.StringNull(), ResponseType: types.StringNull(), Owner: types.StringNull(),
		}

		found, err := (&externalModelResource{*client}).read(ctx, &model)
		if err == nil && found {
			for name, value := range map[string]attr.Value{
				"function": model.Function, "iam_role": model.IAMRole, "model_id": model.ModelID, "prompt": model.Prompt, "suffix": model.Suffix,
				"request_type": model.RequestType, "response_type": model.ResponseType, "owner": model.Owner,
			} {
				lookupValue(data, name, value)
			}
		}

		return found, err
	}}), map[string]string{
		"function":      "Name of the model's inference function, without its schema; null when `SHOW MODEL` does not report it.",
		"iam_role":      "IAM role the model uses, as `SHOW MODEL` reports it to the model's owner; null otherwise.",
		"model_id":      "Amazon Bedrock model ID; null when `SHOW MODEL` does not report it.",
		"prompt":        "Static prompt prefix; null when unset or not reported.",
		"suffix":        "Static prompt suffix; null when unset or not reported.",
		"request_type":  "Request format, `UNIFIED` or `RAW`; null when `SHOW MODEL` does not report it.",
		"response_type": "Response format, `VARCHAR` or `SUPER`; null when `SHOW MODEL` does not report it.",
	})
}
