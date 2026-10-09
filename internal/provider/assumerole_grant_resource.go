package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// newAssumeroleGrantResource defines per-identity IAM-role command permissions.
func newAssumeroleGrantResource() resource.Resource {
	attributes := privilegeAttributes()
	granteeAttributes(attributes)
	attributes["iam_role_arn"] = privilegeString("IAM role ARN, or default for the namespace default role.", false)
	return &privilegeResource{name: "assumerole_grant", attributes: attributes, fields: []string{"iam_role_arn", "grantee", "grantee_type"}, prepare: assumeroleGrantTarget}
}
