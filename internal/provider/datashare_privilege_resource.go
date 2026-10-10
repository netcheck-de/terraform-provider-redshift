package provider

import (
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerResource(newDatasharePrivilegeResource)

// newDatasharePrivilegeResource defines ALTER and SHARE on one producer datashare for a SQL identity. Grant options
// stay off: REVOKE ON DATASHARE documents no GRANT OPTION FOR form that would remove only the option.
func newDatasharePrivilegeResource() resource.Resource {
	attributes := privilegeAttributes()
	attributes["database_name"] = privilegeString("Local producer database that owns the datashare; grants run in it. Changing it replaces the grant.", false)
	attributes["datashare_name"] = privilegeString("Outbound datashare receiving the permissions. Changing it replaces the grant.", false)
	attributes["grantee"] = privilegeString("Receiving user, role, or group name; use `public` with `PUBLIC`. Changing it replaces the grant.", false)
	attributes["grantee_type"] = privilegeString("`USER`, `ROLE`, `GROUP`, or `PUBLIC`. Changing it replaces the grant.", false, "USER", "ROLE", "GROUP", "PUBLIC")
	attributes["privileges"] = schema.SetAttribute{
		Required: true, ElementType: types.StringType,
		Validators:          []validator.Set{setvalidator.ValueStringsAre(stringvalidator.OneOf(privilegeNames(datasharePrivileges)...))},
		MarkdownDescription: "Exact explicit datashare permissions: `ALTER` adds or removes objects and sets `PUBLICACCESSIBLE`; `SHARE` grants consumers usage. Updated in place; an empty set revokes the owned permissions.",
	}
	return &privilegeResource{
		name: "datashare_privilege", attributes: attributes, prepare: datasharePrivilegeTarget,
		fields: []string{"database_name", "datashare_name", "grantee", "grantee_type"},
	}
}
