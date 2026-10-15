package provider

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

var _ = registerFakeFamily("datashare", func() fakeFamily { return &fakeDatashares{privileges: map[string]map[string]bool{}} })

// fakeDatashares extends the legacy share flags with the catalog metadata and the ALTER/SHARE permissions of
// SVV_DATASHARE_PRIVILEGES. The outbound share itself stays the legacy c.share flag, which other families and the
// legacy switch already maintain.
type fakeDatashares struct {
	// owner is the user reported as the share owner.
	owner string
	// privileges maps a grantee key (kind:name, or public) to its explicit datashare permissions.
	privileges map[string]map[string]bool
	// adminOption marks every reported permission as held with grant option.
	adminOption bool
}

// populate gives the representative share an owner and an ALTER permission for a role.
func (f *fakeDatashares) populate() {
	f.owner = "admin"
	f.privileges["role:example:readers"] = map[string]bool{"ALTER": true}
}

// query answers the share metadata and listing reads, the share check of datashare permissions, and their
// catalog reads and GRANT/REVOKE statements.
func (f *fakeDatashares) query(c *catalog, connection dataapi.Connection, sql string, parameters map[string]string) ([]dataapi.Row, bool, error) {
	switch {
	case strings.HasPrefix(sql, "SELECT d.share_name") && strings.Contains(sql, "FROM svv_datashares d"):
		return f.shares(c, connection, sql, parameters), true, nil
	case strings.HasPrefix(sql, "SELECT share_name FROM svv_datashares") && strings.Contains(sql, "source_database"):
		if c.share && parameters["share"] == "producer" {
			return []dataapi.Row{{"share_name": "producer"}}, true, nil
		}
		return nil, true, nil
	case strings.HasPrefix(sql, "SELECT privilege_type, admin_option FROM svv_datashare_privileges"):
		key := "public"
		if parameters["type"] != "" {
			key = parameters["type"] + ":" + parameters["grantee"]
		}
		var rows []dataapi.Row
		if c.share && parameters["share"] == "producer" {
			for _, privilege := range slices.Sorted(maps.Keys(f.privileges[key])) {
				rows = append(rows, dataapi.Row{"privilege_type": privilege, "admin_option": fmt.Sprint(f.adminOption)})
			}
		}
		return rows, true, nil
	case datashareFakePermission(sql, "GRANT"):
		key, privilege, err := datashareFakeTuple(sql, " TO ")
		if err != nil {
			return nil, true, err
		}
		if !c.share {
			return nil, true, fmt.Errorf("datashare does not exist: %q", sql)
		}
		if f.privileges[key] == nil {
			f.privileges[key] = map[string]bool{}
		}
		f.privileges[key][privilege] = true
		return nil, true, nil
	case datashareFakePermission(sql, "REVOKE"):
		key, privilege, err := datashareFakeTuple(sql, " FROM ")
		if err != nil {
			return nil, true, err
		}
		delete(f.privileges[key], privilege)
		return nil, true, nil
	}
	return nil, false, nil
}

// shares lists the outbound share and, when the consumer database exists, the inbound share it came from, then
// applies the filters the query carries.
func (f *fakeDatashares) shares(c *catalog, connection dataapi.Connection, sql string, parameters map[string]string) []dataapi.Row {
	var rows []dataapi.Row
	if c.share {
		rows = append(rows, dataapi.Row{
			"share_name": "producer", "share_type": "OUTBOUND", "source_database": connection.Database, "consumer_database": "",
			"is_publicaccessible": fmt.Sprint(c.public), "managed_by": "", "share_id": "100", "owner": f.owner,
			"producer_account": "123456789012", "producer_namespace": "11111111-2222-3333-4444-555555555555", "created_at": "2026-01-02 03:04:05",
		})
	}
	if c.database {
		consumer := c.databaseName
		if consumer == "" {
			consumer = "analytics"
		}
		rows = append(rows, dataapi.Row{
			"share_name": "source", "share_type": "INBOUND", "source_database": "", "consumer_database": consumer,
			"is_publicaccessible": "false", "managed_by": "", "share_id": "200", "owner": "",
			"producer_account": "123456789012", "producer_namespace": "11111111-2222-3333-4444-555555555555", "created_at": "",
		})
	}
	outboundOnly := strings.Contains(sql, "d.share_type = 'OUTBOUND'")
	return slices.DeleteFunc(rows, func(row dataapi.Row) bool {
		return (outboundOnly && row["share_type"] != "OUTBOUND") ||
			(parameters["share_type"] != "" && row["share_type"] != parameters["share_type"]) ||
			(parameters["name"] != "" && row["share_name"] != parameters["name"])
	})
}

// datashareFakePermission recognizes GRANT or REVOKE of ALTER or SHARE on a datashare; USAGE stays with the
// legacy consumer grant handling.
func datashareFakePermission(sql, verb string) bool {
	return strings.HasPrefix(sql, verb+" ALTER ON DATASHARE ") || strings.HasPrefix(sql, verb+" SHARE ON DATASHARE ")
}

// datashareFakeTuple extracts the grantee key and privilege from a rendered datashare permission statement.
func datashareFakeTuple(sql, separator string) (string, string, error) {
	_, grantee, found := strings.Cut(sql, separator)
	if !found {
		return "", "", fmt.Errorf("fake datashare permission without grantee: %q", sql)
	}
	unquote := func(name string) string { return strings.ReplaceAll(strings.Trim(name, `"`), `""`, `"`) }
	key := "user:" + unquote(grantee)
	switch {
	case grantee == "PUBLIC":
		key = "public"
	case strings.HasPrefix(grantee, "ROLE "):
		key = "role:" + unquote(strings.TrimPrefix(grantee, "ROLE "))
	case strings.HasPrefix(grantee, "GROUP "):
		key = "group:" + unquote(strings.TrimPrefix(grantee, "GROUP "))
	}
	return key, strings.Fields(sql)[1], nil
}
