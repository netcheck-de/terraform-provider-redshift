package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

var _ = registerResource(newLanguageGrantResource)

// newLanguageGrantResource defines USAGE on one procedural language for one SQL grantee, with grant options for users.
func newLanguageGrantResource() resource.Resource {
	attributes := privilegeAttributes()
	attributes["restore_default_on_destroy"] = restoreDefaultAttribute()
	attributes["database_name"] = privilegeString("Local database whose language privileges are managed; language privileges are per database.", false)
	attributes["language_name"] = privilegeKeyword("`SQL` (SQL user-defined functions) or `PLPGSQL` (stored procedures), in any case; another case of the same language is recorded in place.", privilegeNames(languageGrantLanguages)...)
	attributes["grantee"] = privilegeString("Receiving identity name; use `public` for `PUBLIC`.", false)
	attributes["grantee_type"] = privilegeString("`ROLE`, `USER`, `GROUP`, or `PUBLIC`.", false, "ROLE", "USER", "GROUP", "PUBLIC")
	return &privilegeResource{
		name: "language_grant", attributes: attributes, grantOptions: true, prepare: languageGrantTarget, canonical: languageGrantCanonical, partialAllowed: fixedPrivilegeAllowlist(languageGrantPrivileges),
		fields: []string{"database_name", "language_name", "grantee", "grantee_type"}, destroyPrivileges: languageGrantDestroyPrivileges,
		folded: []string{"database_name"},
	}
}
