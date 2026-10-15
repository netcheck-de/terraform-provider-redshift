package provider

import (
	"maps"
	"testing"

	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// TestAssumeroleGrantSQL pins the DEFAULT and ALL keywords, accepted in any case, ARN literals including quote and
// backslash escaping, and the per-command GRANT/REVOKE ASSUMEROLE statements.
func TestAssumeroleGrantSQL(t *testing.T) {
	defaults := map[string]string{"iam_role_arn": "DEFAULT", "grantee_type": "ROLE", "grantee": "readers"}
	render := func(changes map[string]string, command sqlclient.Keyword) func() ([]string, error) {
		fields := maps.Clone(defaults)
		maps.Copy(fields, changes)
		return privilegeTargetCase(t, newAssumeroleGrantResource, fields, command)
	}
	checkSQL(t, "assumerole_grant", []sqlCase{
		{"default_role", render(nil, "COPY")},
		{"default_public", render(map[string]string{"grantee_type": "PUBLIC", "grantee": "public"}, "EXTERNAL FUNCTION")},
		{"default_group", render(map[string]string{"grantee_type": "GROUP"}, "CREATE MODEL")},
		{"arn_user", render(map[string]string{"iam_role_arn": "arn:aws:iam::123456789012:role/loader", "grantee_type": "USER", "grantee": "analyst"}, "UNLOAD")},
		{"quoted_arn_and_grantee", render(map[string]string{"iam_role_arn": `arn:aws:iam::123456789012:role/it's\Loader`, "grantee_type": "USER", "grantee": `Odd"User`}, "UNLOAD")},
		{"all_roles_public", render(map[string]string{"iam_role_arn": "ALL", "grantee_type": "PUBLIC", "grantee": "public"}, "COPY")},
		{"all_roles_quoted_user", render(map[string]string{"iam_role_arn": "ALL", "grantee_type": "USER", "grantee": `Odd"User`}, "CREATE MODEL")},
		{"lowercase_all", render(map[string]string{"iam_role_arn": "all"}, "COPY")},
		{"lowercase_default", render(map[string]string{"iam_role_arn": "default"}, "COPY")},
		{"case_sensitive_arn", render(map[string]string{"iam_role_arn": "ARN:AWS:IAM::123456789012:ROLE/Loader"}, "COPY")},
		{"invalid_arn", render(map[string]string{"iam_role_arn": "loader"}, "COPY")},
		{"non_iam_arn", render(map[string]string{"iam_role_arn": "arn:aws:s3:::bucket"}, "COPY")},
		{"unsupported_grantee_type", render(map[string]string{"grantee_type": "DATASHARE"}, "COPY")},
	})
}
