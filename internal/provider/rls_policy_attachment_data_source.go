package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newRlsPolicyAttachmentDataSource)

// newRlsPolicyAttachmentDataSource checks one attachment; a missing one reports exists = false.
func newRlsPolicyAttachmentDataSource() datasource.DataSource {
	return newCatalogDataSource(catalogSpec{name: "rls_policy_attachment", factory: newRlsPolicyAttachmentResource, exists: true, identityFields: []string{"policy", "schema", "relation", "grantee", "grantee_type"}, identityDatabase: "database", lookup: func(ctx context.Context, client *resourceClient, data *types.Object) (bool, error) {
		attributes := data.Attributes()
		model := rlsPolicyAttachmentModel{
			Policy: attributes["policy"].(types.String), Database: attributes["database"].(types.String), Schema: attributes["schema"].(types.String),
			Relation: attributes["relation"].(types.String), Grantee: attributes["grantee"].(types.String), GranteeType: attributes["grantee_type"].(types.String),
		}
		return (&rlsPolicyAttachmentResource{*client}).read(ctx, model)
	}})
}
