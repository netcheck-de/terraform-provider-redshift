package provider

import (
	"maps"
	"slices"
	"strings"

	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// grantFakeUser is the user recipient the grant fake family answers for; other users reach the legacy switch.
const grantFakeUser = "scoped:analyst"

var _ = registerFakeFamily("grant_user", func() fakeFamily { return &grantUserFamily{} })

// grantUserFamily emulates scoped grants to one user, including grant options, in the shared database.
type grantUserFamily struct {
	// exists records whether the user exists.
	exists bool
	// privileges records the user's TABLES privileges in the database.
	privileges map[string]bool
	// options records the privileges held WITH GRANT OPTION.
	options map[string]bool
}

// query answers the user check and SHOW GRANTS ON DATABASE … FOR the user, and applies GRANT and REVOKE to the user.
func (f *grantUserFamily) query(c *catalog, _ sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, bool, error) {
	quoted := sqlclient.Identifier(grantFakeUser)
	switch {
	case strings.HasPrefix(sql, "SELECT usename FROM pg_user WHERE usename = :name") && parameters["name"] == grantFakeUser:
		if f.exists {
			return []sqlclient.Row{{"usename": grantFakeUser}}, true, nil
		}

		return nil, true, nil
	case strings.HasPrefix(sql, "SHOW GRANTS ON DATABASE ") && strings.HasSuffix(sql, " FOR "+quoted):
		var rows []sqlclient.Row
		for _, privilege := range slices.Sorted(maps.Keys(f.privileges)) {
			rows = append(rows, sqlclient.Row{"database_name": "analytics", "privilege_type": privilege, "identity_name": grantFakeUser, "identity_type": "user", "admin_option": grantFakeBool(f.options[privilege]), "privilege_scope": "TABLES"})
		}

		return rows, true, nil
	case sql == `SHOW GRANTS ON DATABASE "analytics"` || sql == `SHOW GRANTS ON SCHEMA "analytics"."serving"`:
		return f.listing(c, sql), true, nil
	case (strings.HasPrefix(sql, "GRANT ") && strings.Contains(sql, " TO "+quoted)) || (strings.HasPrefix(sql, "REVOKE ") && strings.HasSuffix(sql, " FROM "+quoted)):
		f.apply(sql)
		return nil, true, nil
	}

	return nil, false, nil
}

// listing answers SHOW GRANTS ON DATABASE "analytics", which has no object_type or schema_name column, with the
// user's TABLES rows and the legacy catalog's role rows; and SHOW GRANTS ON SCHEMA "analytics"."serving" with a role's
// schema and scoped TABLES rows, a datashare's schema USAGE, and a group's USAGE, which redshift_grant does not manage.
func (f *grantUserFamily) listing(c *catalog, sql string) []sqlclient.Row {
	if strings.HasPrefix(sql, "SHOW GRANTS ON SCHEMA ") {
		row := func(name, kind, scope, privilege string) sqlclient.Row {
			return sqlclient.Row{"database_name": "analytics", "schema_name": "serving", "object_name": "serving", "object_type": "SCHEMA", "privilege_type": privilege, "identity_name": name, "identity_type": kind, "admin_option": "f", "privilege_scope": scope}
		}

		return []sqlclient.Row{
			row("example:readers", "role", "SCHEMA", "USAGE"), row("example:readers", "role", "TABLES", "SELECT"),
			row("ds:producer", "datashare", "SCHEMA", "USAGE"), row("readers", "group", "SCHEMA", "USAGE"),
		}
	}

	var rows []sqlclient.Row

	if f.exists {
		for _, privilege := range slices.Sorted(maps.Keys(f.privileges)) {
			rows = append(rows, sqlclient.Row{"database_name": "analytics", "privilege_type": privilege, "identity_name": grantFakeUser, "identity_type": "user", "admin_option": grantFakeBool(f.options[privilege]), "privilege_scope": "TABLES"})
		}
	}

	for _, privilege := range slices.Sorted(maps.Keys(c.privileges)) {
		rows = append(rows, sqlclient.Row{"database_name": "analytics", "privilege_type": privilege, "identity_name": "example:readers", "identity_type": "role", "admin_option": "f", "privilege_scope": "TABLES"})
	}

	// PUBLIC's built-in database privilege is not a redshift_grant tuple.
	return append(rows, sqlclient.Row{"database_name": "analytics", "privilege_type": "TEMP", "identity_name": "public", "identity_type": "public", "admin_option": "f", "privilege_scope": "DATABASE"})
}

// apply records one GRANT, GRANT … WITH GRANT OPTION, REVOKE, or REVOKE GRANT OPTION.
func (f *grantUserFamily) apply(sql string) {
	if f.privileges == nil {
		f.privileges, f.options = map[string]bool{}, map[string]bool{}
	}

	fields := strings.Fields(sql)
	switch {
	case fields[0] == "GRANT":
		f.privileges[fields[1]] = true
		f.options[fields[1]] = f.options[fields[1]] || strings.HasSuffix(sql, " WITH GRANT OPTION")
	case strings.HasPrefix(sql, "REVOKE GRANT OPTION "):
		delete(f.options, fields[3])
	default:
		delete(f.privileges, fields[1])
		delete(f.options, fields[1])
	}
}

// populate makes the user exist with SELECT, as the lifecycle case expects of a full catalog.
func (f *grantUserFamily) populate() {
	f.exists, f.privileges, f.options = true, map[string]bool{"SELECT": true}, map[string]bool{"SELECT": true}
}

// grantFakeBool renders a catalog boolean as the Data API returns it.
func grantFakeBool(value bool) string {
	if value {
		return "true"
	}

	return "false"
}
