package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

var _ = registerResource(newLanguageGrantResource)

// newLanguageGrantResource defines USAGE on one procedural language for one SQL grantee, with grant options for users.
func newLanguageGrantResource() resource.Resource {
	attributes := privilegeAttributes()
	attributes["database_name"] = privilegeString("Local database whose language privileges are managed; language privileges are per database. Changing it replaces the grant.", false)
	attributes["language_name"] = privilegeKeyword("`SQL` (SQL user-defined functions) or `PLPGSQL` (stored procedures), in any case; another case of the same language is recorded in place. Changing it replaces the grant.", privilegeNames(languageGrantLanguages)...)
	attributes["grantee"] = privilegeString("Receiving identity name; use `public` for `PUBLIC`. Changing it replaces the grant.", false)
	attributes["grantee_type"] = privilegeString("`ROLE`, `USER`, `GROUP`, or `PUBLIC`. Changing it replaces the grant.", false, "ROLE", "USER", "GROUP", "PUBLIC")
	return &privilegeResource{
		name: "language_grant", attributes: attributes, grantOptions: true, prepare: languageGrantTarget, canonical: languageGrantCanonical,
		fields: []string{"database_name", "language_name", "grantee", "grantee_type"},
	}
}
