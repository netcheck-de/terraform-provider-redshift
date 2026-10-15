package provider

import (
	"maps"
	"regexp"
	"slices"
	"strings"

	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// columnGrantFixture is the relation the column grant fake knows: warehouse.serving.events.
var columnGrantFixture = struct{ database, schema, object string }{"warehouse", "serving", "events"}

// columnGrantStatement parses the column-level GRANT/REVOKE statements the renderer emits.
var columnGrantStatement = regexp.MustCompile(`^(GRANT|REVOKE) (SELECT|UPDATE) \((.+)\) ON TABLE (\S+) (?:TO|FROM) (.+)$`)

// columnGrantFake emulates the explicit column privileges on the fixture relation, keyed by rendered grantee.
type columnGrantFake struct {
	// relation records whether the fixture table exists.
	relation bool
	// grants maps a rendered grantee to privilege to granted columns.
	grants map[string]map[string]map[string]bool
}

var _ = registerFakeFamily("column_grant", func() fakeFamily { return &columnGrantFake{grants: map[string]map[string]map[string]bool{}} })

// columnGrantFakeGrantee renders the catalog identity as the GRANT statement names it.
func columnGrantFakeGrantee(name, kind string) string {
	switch kind {
	case "role":
		return "ROLE " + dataapi.Identifier(name)
	case "group":
		return "GROUP " + dataapi.Identifier(name)
	case "public":
		return "PUBLIC"
	default:
		return dataapi.Identifier(name)
	}
}

// columnGrantFakeIdentity reverses columnGrantFakeGrantee for catalog rows.
func columnGrantFakeIdentity(grantee string) (name, kind string) {
	switch {
	case grantee == "PUBLIC":
		return "public", "public"
	case strings.HasPrefix(grantee, "ROLE "):
		return columnGrantFakeUnquote(strings.TrimPrefix(grantee, "ROLE ")), "role"
	case strings.HasPrefix(grantee, "GROUP "):
		return columnGrantFakeUnquote(strings.TrimPrefix(grantee, "GROUP ")), "group"
	default:
		return columnGrantFakeUnquote(grantee), "user"
	}
}

// columnGrantFakeUnquote reverses sqlclient.Identifier.
func columnGrantFakeUnquote(identifier string) string {
	return strings.ReplaceAll(strings.TrimSuffix(strings.TrimPrefix(identifier, `"`), `"`), `""`, `"`)
}

// set replaces one grantee's privileges, for test setup.
func (f *columnGrantFake) set(grantee string, columns map[string][]string) {
	f.grants[grantee] = map[string]map[string]bool{}
	for privilege, names := range columns {
		f.grants[grantee][privilege] = map[string]bool{}
		for _, name := range names {
			f.grants[grantee][privilege][name] = true
		}
	}
}

// query answers the fixture relation check and column privilege reads, and applies column grants.
func (f *columnGrantFake) query(_ *catalog, _ dataapi.Connection, sql string, parameters map[string]string) ([]dataapi.Row, bool, error) {
	switch {
	case strings.HasPrefix(sql, "SELECT table_name FROM svv_all_tables"):
		if parameters["database"] != columnGrantFixture.database || parameters["schema"] != columnGrantFixture.schema || parameters["name"] != columnGrantFixture.object {
			return nil, false, nil
		}
		if f.relation {
			return []dataapi.Row{{"table_name": columnGrantFixture.object}}, true, nil
		}
		return nil, true, nil
	case strings.Contains(sql, " FROM svv_column_privileges"):
		var rows []dataapi.Row
		if !f.relation {
			return rows, true, nil
		}
		for _, grantee := range slices.Sorted(maps.Keys(f.grants)) {
			name, kind := columnGrantFakeIdentity(grantee)
			for _, privilege := range slices.Sorted(maps.Keys(f.grants[grantee])) {
				for _, column := range slices.Sorted(maps.Keys(f.grants[grantee][privilege])) {
					row := dataapi.Row{"namespace_name": columnGrantFixture.schema, "relation_name": columnGrantFixture.object, "column_name": column, "privilege_type": privilege, "identity_name": name, "identity_type": kind}
					if columnGrantFakeMatches(row, parameters) {
						rows = append(rows, row)
					}
				}
			}
		}
		return rows, true, nil
	}
	match := columnGrantStatement.FindStringSubmatch(sql)
	if match == nil {
		return nil, false, nil
	}
	grantee := match[5]
	if f.grants[grantee] == nil {
		f.grants[grantee] = map[string]map[string]bool{}
	}
	if f.grants[grantee][match[2]] == nil {
		f.grants[grantee][match[2]] = map[string]bool{}
	}
	for column := range strings.SplitSeq(match[3], ", ") {
		if match[1] == "GRANT" {
			f.grants[grantee][match[2]][columnGrantFakeUnquote(column)] = true
		} else {
			delete(f.grants[grantee][match[2]], columnGrantFakeUnquote(column))
		}
	}
	return nil, true, nil
}

// columnGrantFakeMatches applies the bound filters of a column privilege read.
func columnGrantFakeMatches(row dataapi.Row, parameters map[string]string) bool {
	for parameter, column := range map[string]string{"schema": "namespace_name", "object": "relation_name", "grantee": "identity_name", "identity_type": "identity_type"} {
		if value, ok := parameters[parameter]; ok && row[column] != value {
			return false
		}
	}
	return true
}

// populate makes the fixture relation exist with the lifecycle model's grants.
func (f *columnGrantFake) populate() {
	f.relation = true
	f.set(columnGrantFakeGrantee("example:readers", "role"), map[string][]string{"SELECT": {"id", "label"}, "UPDATE": {"label"}})
}
