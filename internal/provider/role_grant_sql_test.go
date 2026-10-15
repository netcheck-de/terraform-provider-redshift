package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// roleGrantFixture builds a role grant to a role (toUser empty) or a user, with the admin option.
func roleGrantFixture(role, toRole, toUser string, admin bool) roleGrantModel {
	data := roleGrantModel{Role: types.StringValue(role), ToRole: types.StringNull(), ToUser: types.StringNull(), AdminOption: types.BoolValue(admin)}
	if toUser != "" {
		data.ToUser = types.StringValue(toUser)
	} else {
		data.ToRole = types.StringValue(toRole)
	}
	return data
}

// TestRoleGrantSQL pins the membership read, GRANT ROLE [WITH ADMIN OPTION], REVOKE ROLE, and REVOKE ADMIN OPTION FOR
// against the role forms of r_GRANT and r_REVOKE, for both recipient kinds.
func TestRoleGrantSQL(t *testing.T) {
	render := func(data roleGrantModel) func() ([]string, error) {
		return func() ([]string, error) {
			read, _, err := readRoleGrantQuery(data).Build()
			if err != nil {
				return nil, err
			}
			create, err := createRoleGrantStatement(data)
			if err != nil {
				return nil, err
			}
			return []string{read, create, dropRoleGrantStatement(data)}, nil
		}
	}
	alter := func(prev, plan roleGrantModel) func() ([]string, error) {
		return func() ([]string, error) { return alterRoleGrantStatements(prev, plan) }
	}
	user, admin := roleGrantFixture("sys:monitor", "", "grafana", false), roleGrantFixture("sys:monitor", "", "grafana", true)
	legacy := user
	legacy.AdminOption = types.BoolNull()
	checkSQL(t, "role_grant", []sqlCase{
		{"to_role", render(roleGrantFixture("sys:dba", "example:readers", "", false))},
		{"to_user", render(user)},
		{"to_user_admin_option", render(admin)},
		{"quoted_to_role", render(roleGrantFixture(`example:Odd"Role`, `Odd"Readers`, "", false))},
		{"quoted_to_user", render(roleGrantFixture(`example:Odd"Role`, "", `Odd"User`, false))},
		{"quoted_to_user_admin_option", render(roleGrantFixture(`example:Odd"Role`, "", `Odd"User`, true))},
		{"empty_role", render(roleGrantFixture("", "example:readers", "", false))},
		{"admin_option_to_role", render(roleGrantFixture("sys:dba", "example:readers", "", true))},
		{"alter_grant_admin_option", alter(user, admin)},
		{"alter_revoke_admin_option", alter(admin, user)},
		{"alter_revoke_admin_option_quoted", alter(roleGrantFixture(`example:Odd"Role`, "", `Odd"User`, true), roleGrantFixture(`example:Odd"Role`, "", `Odd"User`, false))},
		{"alter_unchanged", alter(admin, admin)},
		{"alter_null_prior_is_no_option", alter(legacy, user)},
		{"alter_admin_option_to_role", alter(roleGrantFixture("sys:dba", "example:readers", "", false), roleGrantFixture("sys:dba", "example:readers", "", true))},
	})
}
