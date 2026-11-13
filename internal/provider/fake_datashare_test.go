package provider

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
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
	// objects preserves catalog names verbatim, including function overload spellings unknown to the provider.
	objects          []sqlclient.Row
	bulkCandidates   map[string][]string
	bulkMembers      map[string][]string
	bulkOtherMembers map[string][]sqlclient.Row
	// restricted represents a connection identity without guaranteed full catalog visibility.
	restricted bool
}

// populate gives the representative share an owner and an ALTER permission for a role.
func (f *fakeDatashares) populate() {
	f.owner = "admin"
	f.privileges["role:example:readers"] = map[string]bool{"ALTER": true}
	// Live Redshift reports duplicate name-only rows for shared overloads.
	// A schema name containing a period shows how unquoted member names resolve against the shared schemas.
	f.objects = []sqlclient.Row{
		{"object_name": "odd.schema", "object_type": "schema", "include_new": "false"},
		{"object_name": "odd.schema.events", "object_type": "view"},
		{"object_name": "serving", "object_type": "schema", "include_new": "true"},
		{"object_name": "serving.table", "object_type": "table"},
		{"object_name": "serving.function", "object_type": "function"},
		{"object_name": "serving.function", "object_type": "function"},
	}
	f.bulkCandidates = map[string][]string{"TABLES": {"table"}, "FUNCTIONS": {"f_shared", "f_shared"}}
	f.bulkMembers = map[string][]string{"TABLES": {"table"}, "FUNCTIONS": {"f_shared", "f_shared"}}
}

// query answers the share metadata and listing reads, the share check of datashare permissions, and their
// catalog reads and GRANT/REVOKE statements.
func (f *fakeDatashares) query(c *catalog, connection sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, bool, error) {
	switch {
	case sql == "SELECT usesuper FROM pg_user WHERE usename = current_user":
		return []sqlclient.Row{{"usesuper": fmt.Sprint(!f.restricted)}}, true, nil
	case strings.HasPrefix(sql, "SELECT table_name AS member_name FROM svv_all_tables"), strings.HasPrefix(sql, "SELECT p.proname AS member_name FROM pg_proc_info"):
		kind := "TABLES"
		if strings.Contains(sql, "pg_proc_info") {
			kind = "FUNCTIONS"
		}

		var rows []sqlclient.Row
		for _, name := range f.bulkCandidates[kind] {
			rows = append(rows, sqlclient.Row{"member_name": name})
		}

		return rows, true, nil
	case strings.HasPrefix(sql, "SELECT object_name FROM svv_datashare_objects") && parameters["object"] == "":
		kind := "TABLES"
		if strings.Contains(sql, "object_type = 'function'") {
			kind = "FUNCTIONS"
		}

		var rows []sqlclient.Row

		if c.share && c.shareSchema {
			for _, name := range f.bulkMembers[kind] {
				rows = append(rows, sqlclient.Row{"object_name": "serving." + name})
			}

			for _, row := range f.bulkOtherMembers[kind] {
				rows = append(rows, maps.Clone(row))
			}
		}

		return rows, true, nil
	case strings.Contains(sql, " ADD ALL TABLES IN SCHEMA "), strings.Contains(sql, " ADD ALL FUNCTIONS IN SCHEMA "), strings.Contains(sql, " REMOVE ALL TABLES IN SCHEMA "), strings.Contains(sql, " REMOVE ALL FUNCTIONS IN SCHEMA "):
		kind := "TABLES"
		if strings.Contains(sql, "FUNCTIONS") {
			kind = "FUNCTIONS"
		}

		if f.bulkMembers == nil {
			f.bulkMembers = map[string][]string{}
		}

		if strings.Contains(sql, " ADD ALL ") {
			f.bulkMembers[kind] = slices.Clone(f.bulkCandidates[kind])
		} else {
			f.bulkMembers[kind] = nil
		}

		return nil, true, nil
	case strings.HasPrefix(sql, "SELECT share_name, object_type, object_name, include_new FROM svv_datashare_objects"):
		return f.members(c, sql, parameters), true, nil
	case strings.HasPrefix(sql, "SELECT schema_name, table_name AS member_name FROM svv_all_tables"), strings.HasPrefix(sql, "SELECT n.nspname AS schema_name, p.proname AS member_name FROM pg_proc_info"):
		kind := "TABLES"
		if strings.Contains(sql, "pg_proc_info") {
			kind = "FUNCTIONS"
		}

		var rows []sqlclient.Row

		if parameters["schema"] == "" || parameters["schema"] == "serving" {
			for _, name := range f.bulkCandidates[kind] {
				rows = append(rows, sqlclient.Row{"schema_name": "serving", "member_name": name})
			}
		}

		return rows, true, nil
	case strings.HasPrefix(sql, "SELECT BTRIM(d.source_database) AS source_database, c.share_name"):
		return f.consumers(c, connection, parameters), true, nil
	case strings.HasPrefix(sql, "SELECT BTRIM(d.source_database) AS database_name, p.datashare_name"):
		return f.permissions(c, connection, sql, parameters), true, nil
	case strings.HasPrefix(sql, "SELECT d.share_name") && strings.Contains(sql, "FROM svv_datashares d"):
		return f.shares(c, connection, sql, parameters), true, nil
	case strings.HasPrefix(sql, "SELECT share_name FROM svv_datashares") && strings.Contains(sql, "source_database"):
		if c.share && parameters["share"] == "producer" {
			return []sqlclient.Row{{"share_name": "producer"}}, true, nil
		}

		return nil, true, nil
	case strings.HasPrefix(sql, "SELECT privilege_type, admin_option FROM svv_datashare_privileges"):
		key := "public"
		if parameters["type"] != "" {
			key = parameters["type"] + ":" + parameters["grantee"]
		}

		var rows []sqlclient.Row

		if c.share && parameters["share"] == "producer" {
			for _, privilege := range slices.Sorted(maps.Keys(f.privileges[key])) {
				rows = append(rows, sqlclient.Row{"privilege_type": privilege, "admin_option": fmt.Sprint(f.adminOption)})
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
func (f *fakeDatashares) shares(c *catalog, connection sqlclient.Connection, sql string, parameters map[string]string) []sqlclient.Row {
	var rows []sqlclient.Row
	if c.share {
		rows = append(rows, sqlclient.Row{
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

		rows = append(rows, sqlclient.Row{
			"share_name": "source", "share_type": "INBOUND", "source_database": "", "consumer_database": consumer,
			"is_publicaccessible": "false", "managed_by": "", "share_id": "200", "owner": "",
			"producer_account": "123456789012", "producer_namespace": "11111111-2222-3333-4444-555555555555", "created_at": "",
		})
	}

	outboundOnly, inboundOnly := strings.Contains(sql, "d.share_type = 'OUTBOUND'"), strings.Contains(sql, "d.share_type = 'INBOUND'")

	return slices.DeleteFunc(rows, func(row sqlclient.Row) bool {
		return (outboundOnly && row["share_type"] != "OUTBOUND") || (inboundOnly && row["share_type"] != "INBOUND") ||
			(parameters["name"] != "" && row["share_name"] != parameters["name"])
	})
}

// members lists the representative share's objects whose type the query's object_type list names, ordered by
// name as the listing query orders them.
func (f *fakeDatashares) members(c *catalog, sql string, parameters map[string]string) []sqlclient.Row {
	var rows []sqlclient.Row

	if c.share && (parameters["share"] == "" || parameters["share"] == "producer") {
		for _, row := range f.objects {
			if strings.Contains(sql, "'"+row["object_type"]+"'") {
				item := maps.Clone(row)
				item["share_name"] = "producer"
				rows = append(rows, item)
			}
		}
	}

	slices.SortStableFunc(rows, func(a, b sqlclient.Row) int { return strings.Compare(a["object_name"], b["object_name"]) })

	return rows
}

// consumers lists the account and namespace usage grants of the representative share.
func (f *fakeDatashares) consumers(c *catalog, connection sqlclient.Connection, parameters map[string]string) []sqlclient.Row {
	var rows []sqlclient.Row

	if !c.share || (parameters["share"] != "" && parameters["share"] != "producer") {
		return rows
	}

	if c.shareGrant {
		rows = append(rows, sqlclient.Row{"source_database": connection.Database, "share_name": "producer", "consumer_account": "123456789012", "consumer_namespace": ""})
	}

	if c.shareNamespaceGrant {
		rows = append(rows, sqlclient.Row{"source_database": connection.Database, "share_name": "producer", "consumer_account": "123456789012", "consumer_namespace": "12345678-1234-1234-1234-123456789abc"})
	}

	return rows
}

// permissions lists the explicit permissions on the representative share, one row per grantee and privilege,
// narrowed by the grantee condition the query carries.
func (f *fakeDatashares) permissions(c *catalog, connection sqlclient.Connection, sql string, parameters map[string]string) []sqlclient.Row {
	var rows []sqlclient.Row

	if !c.share || (parameters["share"] != "" && parameters["share"] != "producer") {
		return rows
	}

	for _, key := range slices.Sorted(maps.Keys(f.privileges)) {
		kind, name, _ := strings.Cut(key, ":")
		if key == "public" {
			kind, name = "public", ""
		}

		if (strings.Contains(sql, "p.identity_type = 'public'") && kind != "public") || (parameters["grantee"] != "" && name != parameters["grantee"]) {
			continue
		}

		for _, privilege := range slices.Sorted(maps.Keys(f.privileges[key])) {
			rows = append(rows, sqlclient.Row{"database_name": connection.Database, "datashare_name": "producer", "identity_type": kind, "identity_name": name, "privilege_type": privilege})
		}
	}

	return rows
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
