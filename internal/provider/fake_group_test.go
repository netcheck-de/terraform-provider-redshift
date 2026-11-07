package provider

import (
	"strings"

	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

var _ = registerFakeFamily("group", func() fakeFamily { return &groupFamily{} })

// groupFamily answers the group read with its ID and members from the legacy group and membership flags, which
// keep owning CREATE GROUP, DROP GROUP, and ALTER GROUP.
type groupFamily struct{}

// populate has nothing to add; fullCatalog sets the legacy flags.
func (f *groupFamily) populate() {}

// query answers the member-listing group read and the membership listing. The listings bind no group name and
// report the legacy group "readers" with its member "grafana".
func (f *groupFamily) query(c *catalog, _ sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, bool, error) {
	switch {
	case strings.HasPrefix(sql, "SELECT g.groname, g.grosysid, u.usename"):
		if !c.group {
			return nil, true, nil
		}

		member := ""
		if c.groupMember {
			member = "grafana"
		}

		name := parameters["name"]
		if name == "" {
			name = "readers"
		}

		return []sqlclient.Row{{"groname": name, "grosysid": "101", "usename": member}}, true, nil
	case strings.HasPrefix(sql, "SELECT g.groname, u.usename FROM"):
		if !c.group || !c.groupMember || !fakeFilterMatches(parameters, "group", "readers") || !fakeFilterMatches(parameters, "user", "grafana") {
			return nil, true, nil
		}

		return []sqlclient.Row{{"groname": "readers", "usename": "grafana"}}, true, nil
	}

	return nil, false, nil
}

// fakeFilterMatches reports whether an optional listing filter is unbound or names value.
func fakeFilterMatches(parameters map[string]string, name, value string) bool {
	bound, ok := parameters[name]
	return !ok || bound == value
}
