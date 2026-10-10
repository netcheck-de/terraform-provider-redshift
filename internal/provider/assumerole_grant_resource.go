package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

var _ = registerResource(newAssumeroleGrantResource)

// newAssumeroleGrantResource defines per-identity IAM-role command permissions.
func newAssumeroleGrantResource() resource.Resource {
	attributes := privilegeAttributes()
	grantRecipientAttributes(attributes)
	attributes["iam_role_arn"] = privilegeString("IAM role ARN, `default` for the namespace default IAM role, or `ALL` for every IAM role. Redshift reports grants on `default` and on `ALL` under one catalog entry, so manage a grantee through only one of them. Changing it replaces the grant.", false)
	attributes["privileges"] = grantPrivilegesAttribute("Exact set of commands the grantee may run with the role: `COPY`, `UNLOAD`, `EXTERNAL FUNCTION`, `CREATE MODEL`; all four together are `FOR ALL`. An empty set revokes owned commands. `PUBLIC` holds every command `ON ALL` until revoked, which the `ALL`/`PUBLIC` tuple reports; deleting that tuple grants them back.")
	return &privilegeResource{name: "assumerole_grant", attributes: attributes, fields: []string{"iam_role_arn", "grantee", "grantee_type"}, prepare: assumeroleGrantTarget}
}
