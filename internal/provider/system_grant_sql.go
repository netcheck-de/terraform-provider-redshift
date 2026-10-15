package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// systemPrivileges enumerates the system permissions GRANT documents for roles, in the order of its syntax; the
// RBAC system permission table lists the same set. TestSystemGrantAllowlistMatchesReference pins it.
// https://docs.aws.amazon.com/redshift/latest/dg/r_GRANT.html#grant-roles
// https://docs.aws.amazon.com/redshift/latest/dg/r_roles-system-privileges.html
var systemPrivileges = []sqlclient.Keyword{
	"CREATE USER", "DROP USER", "ALTER USER", "CREATE SCHEMA", "DROP SCHEMA", "ALTER DEFAULT PRIVILEGES",
	"ACCESS CATALOG", "ACCESS SYSTEM TABLE", "CREATE TABLE", "DROP TABLE", "ALTER TABLE",
	"CREATE OR REPLACE FUNCTION", "CREATE OR REPLACE EXTERNAL FUNCTION", "DROP FUNCTION",
	"CREATE OR REPLACE PROCEDURE", "DROP PROCEDURE", "CREATE OR REPLACE VIEW", "DROP VIEW",
	"CREATE MODEL", "DROP MODEL", "CREATE DATASHARE", "ALTER DATASHARE", "DROP DATASHARE",
	"CREATE LIBRARY", "DROP LIBRARY", "CREATE ROLE", "DROP ROLE", "TRUNCATE TABLE", "VACUUM", "ANALYZE", "CANCEL",
	"IGNORE RLS", "EXPLAIN RLS", "EXPLAIN MASKING",
}

// readSystemGrantQuery reads the system privileges granted directly to a role.
func readSystemGrantQuery(role string) sqlclient.Query {
	return sqlclient.Select("system_privilege AS privilege_type").From("svv_system_privileges").
		Where("identity_type = 'role'").
		Where("identity_name = :role", sqlclient.Bind("role", role))
}

// systemGrantTarget renders the role check, catalog read, and GRANT/REVOKE statements for role system privileges.
func systemGrantTarget(data types.Object) (privilegeTarget, error) {
	role := objectString(data, "role")
	checks, err := newCatalogChecks(privilegeRoleQuery(role))
	if err != nil {
		return privilegeTarget{}, err
	}
	query, err := newCatalogCheck(readSystemGrantQuery(role))
	if err != nil {
		return privilegeTarget{}, err
	}
	return privilegeTarget{
		checks: checks, query: query, allowed: systemPrivileges,
		grant: grantSpec{grantee: sqlclient.Kw("ROLE").Ident(role)},
	}, nil
}
