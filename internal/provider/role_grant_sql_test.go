package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// TestRoleGrantSQL pins the membership read, GRANT ROLE, and REVOKE ROLE for both recipient kinds.
func TestRoleGrantSQL(t *testing.T) {
	render := func(role, toRole, toUser string) func() ([]string, error) {
		data := roleGrantModel{Role: types.StringValue(role), ToRole: types.StringNull(), ToUser: types.StringNull()}
		if toUser != "" {
			data.ToUser = types.StringValue(toUser)
		} else {
			data.ToRole = types.StringValue(toRole)
		}
		return func() ([]string, error) {
			read, _, err := readRoleGrantQuery(data).Build()
			if err != nil {
				return nil, err
			}
			return []string{read, createRoleGrantStatement(data), dropRoleGrantStatement(data)}, nil
		}
	}
	checkSQL(t, "role_grant", []sqlCase{
		{"to_role", render("sys:dba", "example:readers", "")},
		{"to_user", render("sys:monitor", "", "grafana")},
		{"quoted_to_role", render(`example:Odd"Role`, `Odd"Readers`, "")},
		{"quoted_to_user", render(`example:Odd"Role`, "", `Odd"User`)},
		{"empty_role", render("", "example:readers", "")},
	})
}
