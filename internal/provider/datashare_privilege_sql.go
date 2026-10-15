package provider

import (
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// datasharePrivileges are the only permissions r_GRANT allows on a datashare for users, roles, and groups:
// ALTER changes membership and accessibility, SHARE grants usage to consumers. Consumer USAGE belongs to
// redshift_datashare_grant.
var datasharePrivileges = []sqlclient.Keyword{"ALTER", "SHARE"}

// datasharePrivilegeShareQuery confirms the outbound share exists in its producer database. SVV_DATASHARES lists
// every share of the namespace, so the check runs in the administration database; the documented samples trim
// source_database, which the comparison mirrors.
func datasharePrivilegeShareQuery(database, share string) sqlclient.Query {
	return sqlclient.Select("share_name").From("svv_datashares").
		Where("share_type = 'OUTBOUND'").
		Where("share_name = :share", sqlclient.Bind("share", share)).
		Where("BTRIM(source_database) = :database", sqlclient.Bind("database", database))
}

// readDatasharePrivilegeQuery reads the explicit ALTER and SHARE grants of one identity from
// SVV_DATASHARE_PRIVILEGES, whose identity_type is lowercase. PUBLIC is matched by type alone, because the view
// documents no name for it.
func readDatasharePrivilegeQuery(share, granteeType, grantee string) sqlclient.Query {
	return sqlclient.Select("privilege_type", "admin_option").From("svv_datashare_privileges").
		Where("datashare_name = :share", sqlclient.Bind("share", share)).
		WhereEither(granteeType == "PUBLIC", "identity_type = 'public'", "identity_type = :type AND identity_name = :grantee",
			sqlclient.Bind("type", strings.ToLower(granteeType)), sqlclient.Bind("grantee", grantee))
}

// datasharePrivilegeTarget renders the share and grantee checks, the catalog read, and GRANT/REVOKE … ON DATASHARE.
// Statements run in the producer database, because a datashare can only be changed from its own database
// (r_ALTER_DATASHARE).
func datasharePrivilegeTarget(data types.Object) (privilegeTarget, error) {
	database, share := objectString(data, "database_name"), objectString(data, "datashare_name")
	grantee, principalCheck, err := principal(data)
	if err != nil {
		return privilegeTarget{}, err
	}
	built, err := newCatalogChecks(
		datasharePrivilegeShareQuery(database, share),
		readDatasharePrivilegeQuery(share, objectString(data, "grantee_type"), objectString(data, "grantee")),
	)
	if err != nil {
		return privilegeTarget{}, err
	}
	return privilegeTarget{
		database: database, checks: []catalogCheck{built[0], principalCheck}, query: built[1], allowed: datasharePrivileges,
		grant: grantSpec{object: sqlclient.Fragment().KwIdent("ON DATASHARE", share), grantee: grantee},
	}, nil
}
