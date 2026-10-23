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

// query answers the member-listing group read.
func (f *groupFamily) query(c *catalog, _ sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, bool, error) {
	if !strings.HasPrefix(sql, "SELECT g.groname, g.grosysid, u.usename") {
		return nil, false, nil
	}
	if !c.group {
		return nil, true, nil
	}
	member := ""
	if c.groupMember {
		member = "grafana"
	}
	return []sqlclient.Row{{"groname": parameters["name"], "grosysid": "101", "usename": member}}, true, nil
}
