package provider

import (
	"errors"
	"regexp"
	"strings"

	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// roleFakeIdentifier matches a quoted SQL identifier, preserving doubled quote escapes.
var roleFakeIdentifier = regexp.MustCompile(`"((?:[^"]|"")*)"`)

// roleFakeIdentifiers returns the unquoted identifiers of sql in order.
func roleFakeIdentifiers(sql string) []string {
	var names []string
	for _, match := range roleFakeIdentifier.FindAllStringSubmatch(sql, -1) {
		names = append(names, strings.ReplaceAll(match[1], `""`, `"`))
	}
	return names
}

// roleFakeFamily emulates the svv_roles owner and external ID on top of the legacy role existence flag and name.
type roleFakeFamily struct {
	// owner is the role owner; empty reports the creating administrator.
	owner string
	// externalID is the identity-provider external ID; empty is NULL.
	externalID string
}

var _ = registerFakeFamily("role", func() fakeFamily { return &roleFakeFamily{} })

// query answers the extended role read and applies CREATE ROLE and ALTER ROLE; DROP ROLE stays in the legacy switch,
// which checks the role's dependencies.
func (f *roleFakeFamily) query(c *catalog, _ dataapi.Connection, sql string, _ map[string]string) ([]dataapi.Row, bool, error) {
	names := roleFakeIdentifiers(sql)
	switch {
	case strings.HasPrefix(sql, "SELECT role_id, role_name, role_owner, external_id FROM svv_roles"):
		if !c.role {
			return nil, true, nil
		}
		name, owner := c.roleName, f.owner
		if name == "" {
			name = "example:readers"
		}
		if owner == "" {
			owner = "admin"
		}
		return []dataapi.Row{{"role_id": "100", "role_name": name, "role_owner": owner, "external_id": f.externalID}}, true, nil
	case strings.HasPrefix(sql, "CREATE ROLE"):
		if c.role || !c.identity || len(names) == 0 {
			return nil, true, errors.New("role exists or identity provider is absent")
		}
		c.role, c.roleName, f.owner, f.externalID = true, names[0], "", ""
		if len(names) > 1 {
			f.externalID = names[1]
		}
	case strings.HasPrefix(sql, "ALTER ROLE"):
		if !c.role || len(names) != 2 {
			return nil, true, errors.New("role is absent or the statement has no target")
		}
		switch {
		case strings.Contains(sql, " OWNER TO "):
			f.owner = names[1]
		case strings.Contains(sql, " EXTERNALID TO "):
			f.externalID = names[1]
		default:
			return nil, true, errors.New("unsupported ALTER ROLE: " + sql)
		}
	default:
		return nil, false, nil
	}
	return nil, true, nil
}

// populate keeps the defaults: the legacy flag decides existence, and the administrator owns the role.
func (f *roleFakeFamily) populate() {}
