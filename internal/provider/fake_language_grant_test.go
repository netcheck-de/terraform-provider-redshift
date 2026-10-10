package provider

import (
	"fmt"
	"regexp"
	"strings"

	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// languageGrantStatement parses GRANT/REVOKE [GRANT OPTION FOR] USAGE ON LANGUAGE statements.
var languageGrantStatement = regexp.MustCompile(`^(GRANT|REVOKE)( GRANT OPTION FOR)? USAGE ON LANGUAGE (SQL|PLPGSQL) (?:TO|FROM) (.+?)( WITH GRANT OPTION)?$`)

// languageGrantFakeKey identifies one language/grantee tuple by the catalog identity.
type languageGrantFakeKey struct {
	// language is the language name.
	language string
	// name is the identity name.
	name string
	// kind is the lower-case identity type.
	kind string
}

// languageGrantFake emulates SVV_LANGUAGE_PRIVILEGES: whether USAGE is held and whether with grant option.
type languageGrantFake struct {
	// usage maps a tuple to its grant option flag; absent tuples hold no USAGE.
	usage map[languageGrantFakeKey]bool
	// defaults records the languages on which PUBLIC still holds its built-in USAGE. The view lists only explicit
	// grants, so the fake assumes the stricter case where the default has no row; REVOKE ... FROM PUBLIC clears it.
	defaults map[string]bool
}

var _ = registerFakeFamily("language_grant", func() fakeFamily {
	return &languageGrantFake{usage: map[languageGrantFakeKey]bool{}, defaults: map[string]bool{"sql": true, "plpgsql": true}}
})

// query answers language privilege reads and applies language grants.
func (f *languageGrantFake) query(_ *catalog, _ dataapi.Connection, sql string, parameters map[string]string) ([]dataapi.Row, bool, error) {
	if strings.Contains(sql, " FROM svv_language_privileges") {
		option, ok := f.usage[languageGrantFakeKey{parameters["language"], parameters["grantee"], parameters["identity_type"]}]
		if !ok {
			return nil, true, nil
		}
		return []dataapi.Row{{"privilege_type": "USAGE", "admin_option": fmt.Sprint(option)}}, true, nil
	}
	match := languageGrantStatement.FindStringSubmatch(sql)
	if match == nil {
		return nil, false, nil
	}
	name, kind := columnGrantFakeIdentity(match[4])
	// SVV_LANGUAGE_PRIVILEGES names languages in lowercase.
	key := languageGrantFakeKey{strings.ToLower(match[3]), name, kind}
	switch {
	case match[1] == "GRANT":
		f.usage[key] = f.usage[key] || match[5] != ""
	case match[2] != "":
		if _, ok := f.usage[key]; ok {
			f.usage[key] = false
		}
	default:
		delete(f.usage, key)
		if kind == "public" {
			delete(f.defaults, key.language)
		}
	}
	return nil, true, nil
}

// populate grants the lifecycle model's USAGE.
func (f *languageGrantFake) populate() {
	f.usage[languageGrantFakeKey{"plpgsql", "example:readers", "role"}] = false
}
