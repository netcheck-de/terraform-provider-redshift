package provider

import (
	"errors"
	"fmt"
	"strings"

	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// roleGrantFakeFamily emulates svv_user_grants.admin_option on top of the legacy role-to-user grant flag.
// Role-to-role grants stay in the legacy switch, because svv_role_grants has no admin option.
type roleGrantFakeFamily struct {
	// admin records whether the user holds the granted role WITH ADMIN OPTION.
	admin bool
}

var _ = registerFakeFamily("role_grant", func() fakeFamily { return &roleGrantFakeFamily{} })

// query answers the user membership read and applies role grants to users, including the admin option.
func (f *roleGrantFakeFamily) query(c *catalog, _ sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, bool, error) {
	switch {
	case strings.HasPrefix(sql, "SELECT role_name, admin_option FROM svv_user_grants"):
		if c.user && c.userGrant {
			return []sqlclient.Row{{"role_name": parameters["role"], "admin_option": fmt.Sprint(f.admin)}}, true, nil
		}

		return nil, true, nil
	case strings.HasPrefix(sql, "SELECT role_name, user_name, admin_option FROM svv_user_grants"):
		// The listing reports the legacy grant of sys:monitor to grafana.
		if !c.user || !c.userGrant || !fakeFilterMatches(parameters, "role", "sys:monitor") || !fakeFilterMatches(parameters, "user", "grafana") {
			return nil, true, nil
		}

		return []sqlclient.Row{{"role_name": "sys:monitor", "user_name": "grafana", "admin_option": fmt.Sprint(f.admin)}}, true, nil
	case strings.HasPrefix(sql, "SELECT granted_role_name, role_name FROM svv_role_grants"):
		// The listing reports the legacy grant of sys:dba to the test role.
		recipient := c.roleName
		if recipient == "" {
			recipient = "example:readers"
		}

		if !c.role || !c.membership || !fakeFilterMatches(parameters, "role", "sys:dba") || !fakeFilterMatches(parameters, "recipient", recipient) {
			return nil, true, nil
		}

		return []sqlclient.Row{{"granted_role_name": "sys:dba", "role_name": recipient}}, true, nil
	case strings.HasPrefix(sql, "GRANT ROLE") && !strings.Contains(sql, " TO ROLE "):
		// Granting again without the option keeps an option the user already holds, as in Redshift.
		c.userGrant, f.admin = true, f.admin || strings.HasSuffix(sql, " WITH ADMIN OPTION")
	case strings.HasPrefix(sql, "REVOKE ROLE") && !strings.Contains(sql, " FROM ROLE "):
		c.userGrant, f.admin = false, false
	case strings.HasPrefix(sql, "REVOKE ADMIN OPTION FOR ROLE"):
		if !c.userGrant {
			return nil, true, errors.New("the user does not hold the role")
		}

		f.admin = false
	default:
		return nil, false, nil
	}

	return nil, true, nil
}

// populate leaves the admin option off; the legacy flag decides membership.
func (f *roleGrantFakeFamily) populate() {}
