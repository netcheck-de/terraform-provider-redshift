package provider

import (
	"maps"
	"slices"
	"strings"

	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
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
func (f *grantUserFamily) query(_ *catalog, _ dataapi.Connection, sql string, parameters map[string]string) ([]dataapi.Row, bool, error) {
	quoted := dataapi.Identifier(grantFakeUser)
	switch {
	case strings.HasPrefix(sql, "SELECT usename FROM pg_user WHERE usename = :name") && parameters["name"] == grantFakeUser:
		if f.exists {
			return []dataapi.Row{{"usename": grantFakeUser}}, true, nil
		}
		return nil, true, nil
	case strings.HasPrefix(sql, "SHOW GRANTS ON DATABASE ") && strings.HasSuffix(sql, " FOR "+quoted):
		var rows []dataapi.Row
		for _, privilege := range slices.Sorted(maps.Keys(f.privileges)) {
			rows = append(rows, dataapi.Row{"database_name": "analytics", "privilege_type": privilege, "identity_name": grantFakeUser, "identity_type": "user", "admin_option": grantFakeBool(f.options[privilege]), "privilege_scope": "TABLES"})
		}
		return rows, true, nil
	case (strings.HasPrefix(sql, "GRANT ") && strings.Contains(sql, " TO "+quoted)) || (strings.HasPrefix(sql, "REVOKE ") && strings.HasSuffix(sql, " FROM "+quoted)):
		f.apply(sql)
		return nil, true, nil
	}
	return nil, false, nil
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
