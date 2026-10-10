package provider

import (
	"errors"
	"fmt"
	"strings"

	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// roleGrantFakeFamily emulates svv_user_grants.admin_option on top of the legacy role-to-user grant flag.
// Role-to-role grants stay in the legacy switch, because svv_role_grants has no admin option.
type roleGrantFakeFamily struct {
	// admin records whether the user holds the granted role WITH ADMIN OPTION.
	admin bool
}

var _ = registerFakeFamily("role_grant", func() fakeFamily { return &roleGrantFakeFamily{} })

// query answers the user membership read and applies role grants to users, including the admin option.
func (f *roleGrantFakeFamily) query(c *catalog, _ dataapi.Connection, sql string, parameters map[string]string) ([]dataapi.Row, bool, error) {
	switch {
	case strings.HasPrefix(sql, "SELECT role_name, admin_option FROM svv_user_grants"):
		if c.user && c.userGrant {
			return []dataapi.Row{{"role_name": parameters["role"], "admin_option": fmt.Sprint(f.admin)}}, true, nil
		}
		return nil, true, nil
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
