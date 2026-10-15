package provider

import (
	"errors"
	"regexp"
	"strconv"
	"strings"

	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

var _ = registerFakeFamily("user", func() fakeFamily { return newUserFamily() })

// userFamily emulates the user options beyond the legacy catalog flags, which keep owning existence (c.user)
// and the capabilities (c.superuser, c.createDB) so role-grant fakes keep seeing the same user.
type userFamily struct {
	// disabled records PASSWORD DISABLE.
	disabled bool
	// validUntil is pg_user.valuntil as the fake returns it.
	validUntil string
	// connectionLimit is svv_user_info.connection_limit.
	connectionLimit string
	// sessionTimeout is svv_user_info.session_timeout.
	sessionTimeout string
	// syslog is svv_user_info.syslog_access.
	syslog string
	// externalID is svv_user_info.external_user_id.
	externalID string
	// config holds pg_user.useconfig entries in insertion order.
	config []string
	// infoHidden hides the svv_user_info row, as Redshift does for a regular user reading another user.
	infoHidden bool
}

// newUserFamily returns the options of a user created without any.
func newUserFamily() *userFamily {
	return &userFamily{connectionLimit: "UNLIMITED", sessionTimeout: "0", syslog: "RESTRICTED"}
}

// populate leaves the options at their defaults; the legacy flags decide whether the user exists.
func (f *userFamily) populate() {}

// userFakeAlter matches ALTER USER and captures the clause after the quoted name.
var userFakeAlter = regexp.MustCompile(`^ALTER USER "(?:[^"]|"")*" (.*)$`)

// userFakeOptions extract CREATE USER and ALTER USER options the fake keeps.
var (
	userFakeValidUntil = regexp.MustCompile(`VALID UNTIL '((?:[^']|'')*)'`)
	userFakeLimit      = regexp.MustCompile(`CONNECTION LIMIT (UNLIMITED|\d+)`)
	userFakeTimeout    = regexp.MustCompile(`SESSION TIMEOUT (\d+)`)
	userFakeSyslog     = regexp.MustCompile(`SYSLOG ACCESS (RESTRICTED|UNRESTRICTED)`)
	userFakeExternal   = regexp.MustCompile(`EXTERNALID "((?:[^"]|"")*)"`)
	userFakeSet        = regexp.MustCompile(`^SET ([a-z_]+) TO (.*)$`)
	userFakeReset      = regexp.MustCompile(`^RESET ([a-z_]+)$`)
	userFakeIdentifier = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)
)

// options applies the options a CREATE USER or ALTER USER clause carries.
func (f *userFamily) options(clause string) error {
	if match := userFakeValidUntil.FindStringSubmatch(clause); match != nil {
		f.validUntil = commentUnescaper.Replace(match[1])
	}
	if match := userFakeLimit.FindStringSubmatch(clause); match != nil {
		f.connectionLimit = match[1]
	}
	if match := userFakeTimeout.FindStringSubmatch(clause); match != nil {
		f.sessionTimeout = match[1]
	}
	if match := userFakeSyslog.FindStringSubmatch(clause); match != nil {
		f.syslog = match[1]
	}
	if match := userFakeExternal.FindStringSubmatch(clause); match != nil {
		if !f.disabled {
			return errors.New("EXTERNALID requires a disabled password")
		}
		f.externalID = strings.ReplaceAll(match[1], `""`, `"`)
	}
	return nil
}

// setting stores or removes one useconfig entry; search_path keeps quoted identifiers as Redshift does.
func (f *userFamily) setting(name, value string, remove bool) {
	kept := f.config[:0]
	for _, entry := range f.config {
		if !strings.HasPrefix(entry, name+"=") {
			kept = append(kept, entry)
		}
	}
	f.config = kept
	if !remove {
		f.config = append(f.config, name+"="+value)
	}
}

// userFakeSearchPath renders SET search_path literals the way Redshift stores them, quoting names that need it.
func userFakeSearchPath(values string) string {
	var names []string
	for _, match := range literals.FindAllStringSubmatch(values, -1) {
		name := commentUnescaper.Replace(match[1])
		if !userFakeIdentifier.MatchString(name) {
			name = `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
		}
		names = append(names, name)
	}
	return strings.Join(names, ", ")
}

// alter applies one ALTER USER clause.
func (f *userFamily) alter(c *catalog, clause string) error {
	switch {
	case clause == "PASSWORD DISABLE":
		if c.superuser {
			return errors.New("cannot disable the password of a superuser")
		}
		f.disabled = true
	case strings.HasPrefix(clause, "PASSWORD '"):
		f.disabled = false
	case clause == "CREATEUSER":
		if f.disabled {
			return errors.New("a superuser needs a password")
		}
		c.superuser = true
	case clause == "NOCREATEUSER":
		c.superuser = false
	case clause == "CREATEDB":
		c.createDB = true
	case clause == "NOCREATEDB":
		c.createDB = false
	case clause == "RESET SESSION TIMEOUT":
		f.sessionTimeout = "0"
	case userFakeSet.MatchString(clause):
		match := userFakeSet.FindStringSubmatch(clause)
		value := match[2]
		if match[1] == userSearchPathParameter {
			value = userFakeSearchPath(value)
		} else {
			unquoted, err := literal(value, 0)
			if err != nil {
				return err
			}
			value = commentUnescaper.Replace(unquoted)
		}
		f.setting(match[1], value, false)
	case userFakeReset.MatchString(clause):
		f.setting(userFakeReset.FindStringSubmatch(clause)[1], "", true)
	default:
		return f.options(clause)
	}
	return nil
}

// query answers the user reads and applies CREATE, ALTER, and DROP USER.
func (f *userFamily) query(c *catalog, _ dataapi.Connection, sql string, parameters map[string]string) ([]dataapi.Row, bool, error) {
	switch {
	case strings.HasPrefix(sql, "SELECT usename, usesuper, usecreatedb, valuntil"):
		if !c.user {
			return nil, true, nil
		}
		return []dataapi.Row{{
			"usename": parameters["name"], "usesuper": strconv.FormatBool(c.superuser), "usecreatedb": strconv.FormatBool(c.createDB),
			"valuntil": f.validUntil, "useconfig": strings.Join(f.config, userConfigSeparator),
		}}, true, nil
	case strings.HasPrefix(sql, "SELECT connection_limit, syslog_access, session_timeout, external_user_id FROM svv_user_info"):
		if !c.user || f.infoHidden {
			return nil, true, nil
		}
		return []dataapi.Row{{"connection_limit": f.connectionLimit, "syslog_access": f.syslog, "session_timeout": f.sessionTimeout, "external_user_id": f.externalID}}, true, nil
	case strings.HasPrefix(sql, "CREATE USER"):
		if c.user {
			return nil, true, errors.New("user already exists")
		}
		fresh := newUserFamily()
		fresh.infoHidden = f.infoHidden
		fresh.disabled = strings.Contains(sql, " PASSWORD DISABLE ")
		superuser := strings.Contains(sql, " CREATEUSER")
		if fresh.disabled && superuser {
			return nil, true, errors.New("cannot disable the password of a superuser")
		}
		if err := fresh.options(sql); err != nil {
			return nil, true, err
		}
		*f = *fresh
		c.user, c.superuser, c.createDB = true, superuser, strings.Contains(sql, " CREATEDB")
		return nil, true, nil
	case strings.HasPrefix(sql, "ALTER USER"):
		match := userFakeAlter.FindStringSubmatch(sql)
		if match == nil {
			return nil, true, errors.New("fake catalog cannot parse " + sql)
		}
		if !c.user {
			return nil, true, errors.New("user does not exist")
		}
		return nil, true, f.alter(c, match[1])
	case strings.HasPrefix(sql, "DROP USER"):
		if c.userGrant {
			return nil, true, errors.New("user still has role grants")
		}
		hidden := f.infoHidden
		*f = *newUserFamily()
		f.infoHidden = hidden
		c.user = false
		return nil, true, nil
	}
	return nil, false, nil
}
