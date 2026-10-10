package provider

import (
	"maps"
	"testing"

	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// datasharePrivilegeFields is a representative datashare permission tuple for a role.
var datasharePrivilegeFields = map[string]string{"database_name": "warehouse", "datashare_name": "producer", "grantee_type": "ROLE", "grantee": "share_admins"}

// TestDatasharePrivilegeSQL pins the share and grantee checks, the SVV_DATASHARE_PRIVILEGES read, and
// GRANT/REVOKE … ON DATASHARE for every grantee form, including quoting of names with quotes and case.
func TestDatasharePrivilegeSQL(t *testing.T) {
	render := func(changes map[string]string, privilege sqlclient.Keyword) func() ([]string, error) {
		fields := maps.Clone(datasharePrivilegeFields)
		maps.Copy(fields, changes)
		return privilegeTargetCase(t, newDatasharePrivilegeResource, fields, privilege)
	}
	checkSQL(t, "datashare_privilege", []sqlCase{
		{"role_alter", render(nil, "ALTER")},
		{"user_share", render(map[string]string{"grantee_type": "USER", "grantee": "loader"}, "SHARE")},
		{"group_alter", render(map[string]string{"grantee_type": "GROUP", "grantee": "readers"}, "ALTER")},
		{"public_share", render(map[string]string{"grantee_type": "PUBLIC", "grantee": "public"}, "SHARE")},
		{"quoted_identifiers", render(map[string]string{"datashare_name": `Odd"Share`, "grantee": `Odd"Role`}, "ALTER")},
		{"public_with_other_name", render(map[string]string{"grantee_type": "PUBLIC", "grantee": "everyone"}, "SHARE")},
		{"unsupported_grantee_type", render(map[string]string{"grantee_type": "NAMESPACE"}, "ALTER")},
		{"empty_database_name", render(map[string]string{"database_name": ""}, "ALTER")},
		{"empty_datashare_name", render(map[string]string{"datashare_name": ""}, "ALTER")},
		{"empty_grantee", render(map[string]string{"grantee": ""}, "ALTER")},
	})
}

// TestDatasharePrivilegeBindings checks that names with quotes and backslashes travel as bound values and that
// the tuple routes statements to the producer database.
func TestDatasharePrivilegeBindings(t *testing.T) {
	r := newDatasharePrivilegeResource().(*privilegeResource)
	fields := maps.Clone(datasharePrivilegeFields)
	fields["datashare_name"], fields["grantee"], fields["database_name"] = `It's\"Share`, `O'Brien\x`, "Producer"
	target, err := r.prepare(privilegeObject(t, r, fields))
	require.NoError(t, err)
	assert.Equal(t, "Producer", target.database)
	assert.Equal(t, map[string]string{"share": `It's\"Share`, "database": "Producer"}, target.checks[0].parameters)
	assert.Equal(t, map[string]string{"name": `O'Brien\x`}, target.checks[1].parameters)
	assert.Equal(t, map[string]string{"share": `It's\"Share`, "type": "role", "grantee": `O'Brien\x`}, target.query.parameters)
	assert.Equal(t, `GRANT ALTER ON DATASHARE "It's\""Share" TO ROLE "O'Brien\x"`, target.grant.statement(true, "ALTER"))
}
