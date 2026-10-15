package provider

import (
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// languageGrantLanguages are the languages whose USAGE the provider manages: SQL for SQL UDFs and PLPGSQL for stored
// procedures, accepted in any case. PLPYTHONU is excluded because Redshift no longer supports Python UDFs.
// https://docs.aws.amazon.com/redshift/latest/dg/r_GRANT.html
var languageGrantLanguages = []sqlclient.Keyword{"SQL", "PLPGSQL"}

// languageGrantPrivileges is the only language privilege.
var languageGrantPrivileges = []sqlclient.Keyword{"USAGE"}

// readLanguageGrantQuery lists the grantee's explicit privileges on one language with their grant option.
// SVV_LANGUAGE_PRIVILEGES reports the current database only, so it runs in database_name, and names languages in
// lowercase, as pg_language does.
// https://docs.aws.amazon.com/redshift/latest/dg/r_SVV_LANUGAGE_PRIVILEGES.html
func readLanguageGrantQuery(language, grantee, granteeType string) sqlclient.Query {
	return sqlclient.Select("privilege_type", "admin_option").From("svv_language_privileges").
		Where("language_name = :language", sqlclient.Bind("language", strings.ToLower(language))).
		Where("identity_name = :grantee", sqlclient.Bind("grantee", grantee)).
		Where("identity_type = :identity_type", sqlclient.Bind("identity_type", columnGrantIdentityType(granteeType)))
}

// languageGrantCanonical records the language in its canonical uppercase spelling.
func languageGrantCanonical(fields map[string]string) {
	if language, ok := fields["language_name"]; ok {
		fields["language_name"] = strings.ToUpper(language)
	}
}

// languageGrantTarget validates a language tuple and renders its checks, catalog read, and GRANT/REVOKE USAGE ON
// LANGUAGE statements. The language is an allowlisted keyword, written unquoted as in the AWS examples.
func languageGrantTarget(data types.Object) (privilegeTarget, error) {
	grantee, check, err := principal(data)
	if err != nil {
		return privilegeTarget{}, err
	}
	database := objectString(data, "database_name")
	language, err := sqlclient.OneOf(objectString(data, "language_name"), languageGrantLanguages...)
	if err != nil {
		return privilegeTarget{}, fmt.Errorf("unsupported language_name: %w", err)
	}
	checks, err := newCatalogChecks(localDatabaseQuery(database))
	if err != nil {
		return privilegeTarget{}, err
	}
	query, err := newCatalogCheck(readLanguageGrantQuery(string(language), objectString(data, "grantee"), objectString(data, "grantee_type")))
	if err != nil {
		return privilegeTarget{}, err
	}
	target := privilegeTarget{
		database: database, checks: append([]catalogCheck{check}, checks...), allowed: languageGrantPrivileges, query: query,
		grant: grantSpec{object: sqlclient.Kw("ON LANGUAGE", language), grantee: grantee},
	}
	if objectString(data, "grantee_type") == "PUBLIC" {
		// PUBLIC holds USAGE on SQL and PLPGSQL by default, and SVV_LANGUAGE_PRIVILEGES lists only explicit grants,
		// so the default may have no row; AWS revokes it with REVOKE USAGE ON LANGUAGE ... FROM PUBLIC.
		// https://docs.aws.amazon.com/redshift/latest/dg/r_REVOKE.html
		target.implicit = []string{string(languageGrantPrivileges[0])}
	}
	return target, nil
}
