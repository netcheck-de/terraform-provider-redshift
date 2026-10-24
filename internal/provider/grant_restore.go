package provider

import (
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// restoreDefaultAttribute is deliberately not an identity field: changing cleanup policy never replaces a grant.
func restoreDefaultAttribute() schema.BoolAttribute {
	return schema.BoolAttribute{Optional: true, MarkdownDescription: "Opt in to restoring the built-in PUBLIC privilege on destroy instead of leaving it revoked. Defaults to false. Only valid for a PUBLIC language grant or database-wide PUBLIC FUNCTIONS default privileges, respectively. This can widen access after Terraform stops managing the tuple; it does not restore historical grants."}
}

// restoreGrantDefault restricts opt-in cleanup to the exact tuple with a documented built-in privilege.
func restoreGrantDefault(data types.Object, eligible bool, privilege string) ([]string, error) {
	value, ok := data.Attributes()["restore_default_on_destroy"].(types.Bool)
	if !ok || !value.ValueBool() {
		return nil, nil
	}

	if !eligible {
		return nil, fmt.Errorf("restore_default_on_destroy requires a PUBLIC language grant or database-wide PUBLIC FUNCTIONS default privileges")
	}

	return []string{privilege}, nil
}

// languageGrantDestroyPrivileges restores PUBLIC's default USAGE on destroy when requested.
func languageGrantDestroyPrivileges(data types.Object) ([]string, error) {
	return restoreGrantDefault(data, objectString(data, "grantee_type") == "PUBLIC", "USAGE")
}

// defaultPrivilegesDestroyPrivileges restores PUBLIC's implicit default EXECUTE on destroy when requested.
func defaultPrivilegesDestroyPrivileges(data types.Object) ([]string, error) {
	return restoreGrantDefault(data, defaultPrivilegesImplicit(data), "EXECUTE")
}
