package provider

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// debugGrantTestDatabase is the federated database name the DEBUG grant tests use; the fake answers SHOW GRANTS only
// for databases it holds DEBUG grants for, so the object grant fakes keep their own SHOW GRANTS ON DATABASE rows.
const debugGrantTestDatabase = "sales_db@finance-catalog"

// debugGrantKey identifies one DEBUG grantee as SHOW GRANTS reports it.
type debugGrantKey struct {
	// identityType is global_user, global_group, or admins_of_account.
	identityType string
	// name is the identity_name column.
	name string
}

// debugGrantFakeFamily emulates DEBUG grants per database and their SHOW GRANTS rows.
type debugGrantFakeFamily struct {
	// grants maps a database to its DEBUG grantees; a database is answered only while it has an entry.
	grants map[string]map[debugGrantKey]bool
}

var _ = registerFakeFamily("debug_grant", func() fakeFamily { return &debugGrantFakeFamily{grants: map[string]map[debugGrantKey]bool{}} })

// debugGrantStatement parses the GRANT and REVOKE statements the renderers produce.
var debugGrantStatement = regexp.MustCompile(`^(GRANT|REVOKE) DEBUG ON DATABASE ("(?:[^"]|"")*") (?:TO|FROM) (?:ROLE ("(?:[^"]|"")*")|ADMINS OF ACCOUNT '([0-9]+)'|("(?:[^"]|"")*"))$`)

// debugGrantForStatement parses a scoped remote SHOW GRANTS FOR read.
var debugGrantForStatement = regexp.MustCompile(`^SHOW GRANTS FOR (ROLE )?("(?:[^"]|"")*") FROM DATABASE ("(?:[^"]|"")*")$`)

// grant records or removes one grantee on database.
func (f *debugGrantFakeFamily) grant(database string, key debugGrantKey, granted bool) {
	if f.grants[database] == nil {
		f.grants[database] = map[debugGrantKey]bool{}
	}
	if granted {
		f.grants[database][key] = true
	} else {
		delete(f.grants[database], key)
	}
}

// clear revokes every DEBUG grant but keeps answering for the databases.
func (f *debugGrantFakeFamily) clear() {
	for database := range f.grants {
		f.grants[database] = map[debugGrantKey]bool{}
	}
}

// query answers SHOW GRANTS ON DATABASE for databases with DEBUG state and applies GRANT and REVOKE DEBUG.
func (f *debugGrantFakeFamily) query(_ *catalog, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, bool, error) {
	if sql == readDebugGrantAuthStatement() {
		return []sqlclient.Row{{"auth_method": "IAM AssumeUser"}}, true, nil
	}
	if match := debugGrantForStatement.FindStringSubmatch(sql); match != nil {
		name, principal := fakeUnquote(match[3]), fakeUnquote(match[2])
		grants, found := f.grants[name]
		if !found {
			return nil, false, nil
		}
		identityType, sourceType := "user", "global_user"
		if match[1] != "" {
			identityType, sourceType = "role", "global_group"
		}
		var rows []sqlclient.Row
		for key := range grants {
			if key.identityType == sourceType && strings.EqualFold(key.name, principal) {
				rows = append(rows, sqlclient.Row{"database_name": name, "object_type": "DATABASE", "privilege_scope": "DATABASE", "privilege_type": "DEBUG", "identity_name": key.name, "identity_type": identityType})
			}
		}
		return rows, true, nil
	}
	if database, ok := strings.CutPrefix(sql, "SHOW GRANTS ON DATABASE "); ok && !strings.Contains(database, " FOR ") {
		name := fakeUnquote(database)
		grantees, found := f.grants[name]
		if !found {
			return nil, false, nil
		}
		if strings.Contains(name, "@") {
			return nil, true, nil
		}
		// A database privilege of another identity shows that rows are matched by privilege and grantee.
		rows := []sqlclient.Row{{"database_name": name, "privilege_type": "TEMP", "identity_id": "0", "identity_name": "public", "identity_type": "public", "admin_option": "f", "privilege_scope": "DATABASE", "grantor_name": "admin"}}
		keys := slices.SortedFunc(maps.Keys(grantees), func(a, b debugGrantKey) int { return strings.Compare(a.identityType+a.name, b.identityType+b.name) })
		for _, key := range keys {
			rows = append(rows, sqlclient.Row{"database_name": name, "privilege_type": "DEBUG", "identity_id": "145", "identity_name": key.name, "identity_type": key.identityType, "admin_option": "f", "privilege_scope": "DATABASE", "grantor_name": "admin"})
		}
		return rows, true, nil
	}
	match := debugGrantStatement.FindStringSubmatch(sql)
	if match == nil {
		if strings.Contains(sql, " DEBUG ") && (strings.HasPrefix(sql, "GRANT ") || strings.HasPrefix(sql, "REVOKE ")) {
			return nil, true, fmt.Errorf("fake catalog cannot parse %q", sql)
		}
		return nil, false, nil
	}
	key := debugGrantKey{identityType: "global_user", name: fakeUnquote(match[5])}
	switch {
	case match[3] != "":
		key = debugGrantKey{identityType: "global_group", name: fakeUnquote(match[3])}
	case match[4] != "":
		key = debugGrantKey{identityType: "admins_of_account", name: match[4]}
	}
	f.grant(fakeUnquote(match[2]), key, match[1] == "GRANT")
	return nil, true, nil
}

// populate grants DEBUG to the representative global identity on the test database.
func (f *debugGrantFakeFamily) populate() {
	f.grant(debugGrantTestDatabase, debugGrantKey{identityType: "global_user", name: "IAM:sales_auditor"}, true)
}
