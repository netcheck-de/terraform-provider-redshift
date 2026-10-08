package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// systemPrivileges enumerates SQL capability names accepted by authoritative role grants.
var systemPrivileges = []string{
	"CREATE USER", "DROP USER", "ALTER USER", "CREATE SCHEMA", "DROP SCHEMA", "ALTER DEFAULT PRIVILEGES",
	"ACCESS CATALOG", "ACCESS SYSTEM TABLE", "CREATE TABLE", "DROP TABLE", "ALTER TABLE",
	"CREATE OR REPLACE FUNCTION", "CREATE OR REPLACE EXTERNAL FUNCTION", "DROP FUNCTION",
	"CREATE OR REPLACE PROCEDURE", "DROP PROCEDURE", "CREATE OR REPLACE VIEW", "DROP VIEW",
	"CREATE MODEL", "DROP MODEL", "CREATE DATASHARE", "ALTER DATASHARE", "DROP DATASHARE",
	"CREATE LIBRARY", "DROP LIBRARY", "CREATE ROLE", "DROP ROLE", "TRUNCATE TABLE", "VACUUM", "ANALYZE", "CANCEL",
	"IGNORE RLS", "EXPLAIN RLS", "EXPLAIN MASKING",
}

// newSystemGrantResource defines role system capabilities and their catalog reconciliation.
func newSystemGrantResource() resource.Resource {
	attributes := privilegeAttributes()
	attributes["role"] = privilegeString("Receiving SQL role.", false)
	return &privilegeResource{name: "system_grant", attributes: attributes, fields: []string{"role"}, prepare: func(data types.Object) (privilegeTarget, error) {
		role := objectString(data, "role")
		return privilegeTarget{
			checks:    []catalogCheck{{"SELECT role_name FROM svv_roles WHERE role_name = :name", map[string]string{"name": role}}},
			query:     catalogCheck{"SELECT system_privilege AS privilege_type FROM svv_system_privileges WHERE identity_type = 'role' AND identity_name = :role", map[string]string{"role": role}},
			recipient: "ROLE " + sqlclient.Identifier(role), allowed: systemPrivileges,
		}, nil
	}}
}
