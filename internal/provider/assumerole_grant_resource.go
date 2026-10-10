package provider

import (
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

var _ = registerResource(newAssumeroleGrantResource)

// newAssumeroleGrantResource defines per-identity IAM-role command permissions.
func newAssumeroleGrantResource() resource.Resource {
	attributes := privilegeAttributes()
	grantRecipientAttributes(attributes)
	attributes["iam_role_arn"] = schema.StringAttribute{
		Required: true,
		MarkdownDescription: "IAM role ARN, `DEFAULT` for the namespace default IAM role, or `ALL` for every IAM role. `DEFAULT` and `ALL` are keywords accepted in any case, and another case of the same one is recorded in place; an ARN is case-sensitive and compared exactly. " +
			"Redshift reports grants on `DEFAULT` and on `ALL` under one catalog entry, so manage a grantee through only one of them. Changing it replaces the grant.",
		PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplaceIf(assumeroleGrantRoleChanged, "Changing the IAM role replaces the grant.", "Changing the IAM role replaces the grant.")},
		Validators:    []validator.String{stringvalidator.LengthAtLeast(1)},
	}
	attributes["privileges"] = grantPrivilegesAttribute("Exact set of commands the grantee may run with the role: `COPY`, `UNLOAD`, `EXTERNAL FUNCTION`, `CREATE MODEL`; all four together are `FOR ALL`. An empty set revokes owned commands. `PUBLIC` holds every command `ON ALL` until revoked, which the `ALL`/`PUBLIC` tuple reports; deleting that tuple revokes what it holds and never grants the default back.")
	return &privilegeResource{name: "assumerole_grant", attributes: attributes, fields: []string{"iam_role_arn", "grantee", "grantee_type"}, prepare: assumeroleGrantTarget, canonical: assumeroleGrantCanonical}
}
