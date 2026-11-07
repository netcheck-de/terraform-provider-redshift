package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newIdentityProvidersDataSource)

// identityProvidersOutputDescriptions describe the listed provider attributes as the lookup reports them.
var identityProvidersOutputDescriptions = map[string]string{
	"type":                         "Identity provider type: `AWSIDC` or `AZURE`.",
	"namespace":                    "Prefix of federated users and group roles.",
	"application_arn":              "Identity Center managed application ARN; null for `AZURE`.",
	"iam_role_arn":                 "Identity Center integration IAM role ARN; null for `AZURE`.",
	"issuer":                       "Microsoft Entra ID token issuer URL; null for `AWSIDC`.",
	"client_id":                    "Microsoft Entra ID application (client) ID; null for `AWSIDC`.",
	"audience":                     "Accepted Microsoft Entra ID token audiences; null for `AWSIDC` or when none are set.",
	"enabled":                      "Whether the provider is enabled.",
	"provider_id":                  "Catalog ID of the identity provider (`svv_identity_providers.uid`).",
	"instance_id":                  "Catalog instance identifier (`svv_identity_providers.instanceid`): the application ARN for `AWSIDC` and the tenant ID for `AZURE`.",
	"identity_center_instance_arn": "IAM Identity Center instance ARN of an `AWSIDC` provider; null for `AZURE`.",
}

// newIdentityProvidersDataSource lists every SQL identity provider with its catalog settings.
func newIdentityProvidersDataSource() datasource.DataSource {
	return newCollectionDataSource(collectionSpec{
		name: "identity_providers",
		description: "Lists every SQL identity provider (`AWSIDC` or `AZURE`) from `SVV_IDENTITY_PROVIDERS`, which only superusers can read. " +
			"The client secret, automatic role creation, and its group filters are not in the catalog and are not listed.",
		element: describedOutputs(collectionElement(newIdentityProviderResource, "name", "type", "namespace", "application_arn", "iam_role_arn",
			"issuer", "client_id", "audience", "enabled", "provider_id", "instance_id", "identity_center_instance_arn"), identityProvidersOutputDescriptions),
		list: func(ctx context.Context, client *resourceClient, _ types.Object) ([]map[string]attr.Value, error) {
			rows, err := client.selectRows(ctx, client.database.ValueString(), listIdentityProvidersQuery())
			if err != nil {
				return nil, err
			}

			items := make([]map[string]attr.Value, 0, len(rows))
			for _, row := range rows {
				var provider identityProviderModel
				if err := identityProviderCatalog(&provider, row); err != nil {
					return nil, err
				}

				audience := types.SetNull(types.StringType)
				if provider.Audience != nil {
					audience = types.SetValueMust(types.StringType, grantStringValues(provider.Audience))
				}

				items = append(items, map[string]attr.Value{
					"name": provider.Name, "type": provider.Type, "namespace": provider.Namespace,
					"application_arn": provider.ApplicationARN, "iam_role_arn": provider.IAMRoleARN, "issuer": provider.Issuer,
					"client_id": provider.ClientID, "audience": audience, "enabled": provider.Enabled, "provider_id": provider.ProviderID,
					"instance_id": provider.InstanceID, "identity_center_instance_arn": provider.IdentityCenterInstanceARN,
				})
			}

			return items, nil
		},
	})
}
