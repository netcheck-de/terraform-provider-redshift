package provider

import (
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// policyGrantTypes are the security policies that GRANT … TO { RLS | MASKING } POLICY accepts.
// https://docs.aws.amazon.com/redshift/latest/dg/r_GRANT.html#grant-row-level-security
var policyGrantTypes = []sqlclient.Keyword{"RLS", "MASKING"}

// policyGrantPrivileges is the only permission a policy can receive: reading its lookup table.
var policyGrantPrivileges = []sqlclient.Keyword{"SELECT"}

// policyGrantTableQuery confirms that the lookup table exists in database.
func policyGrantTableQuery(database, schema, name string) sqlclient.Query {
	return sqlclient.Select("table_name").From("svv_all_tables").
		Where("database_name = :database", sqlclient.Bind("database", database)).
		Where("schema_name = :schema", sqlclient.Bind("schema", schema)).
		Where("table_name = :name", sqlclient.Bind("name", name))
}

// policyGrantPolicyQuery confirms that the receiving policy exists, from SVV_RLS_POLICY or SVV_MASKING_POLICY.
// https://docs.aws.amazon.com/redshift/latest/dg/r_SVV_RLS_POLICY.html
// https://docs.aws.amazon.com/redshift/latest/dg/r_SVV_MASKING_POLICY.html
func policyGrantPolicyQuery(kind sqlclient.Keyword, database, name string) sqlclient.Query {
	if kind == "RLS" {
		return sqlclient.Select("polname").From("svv_rls_policy").
			Where("poldb = :database", sqlclient.Bind("database", database)).
			Where("polname = :name", sqlclient.Bind("name", name))
	}
	return sqlclient.Select("policy_name").From("svv_masking_policy").
		Where("policy_database = :database", sqlclient.Bind("database", database)).
		Where("policy_name = :name", sqlclient.Bind("name", name))
}

// policyGrantRecipient renders RLS POLICY "name" or MASKING POLICY "name" as the grantee, which privilegeResource
// substitutes for the SQL identity that prepare would render.
func policyGrantRecipient(data types.Object) (sqlclient.Statement, error) {
	kind, err := sqlclient.OneOf(objectString(data, "policy_type"), policyGrantTypes...)
	if err != nil {
		return sqlclient.Statement{}, fmt.Errorf("policy_type: %w", err)
	}
	name := objectString(data, "policy_name")
	if name == "" {
		return sqlclient.Statement{}, fmt.Errorf("policy_name must not be empty")
	}
	return sqlclient.Kw(kind, "POLICY").Ident(name), nil
}

// policyGrantTarget validates a lookup table and policy tuple and renders its parent checks and the
// GRANT SELECT ON TABLE … TO { RLS | MASKING } POLICY statement shape. It has no privilege read, because no
// documented catalog reports grants to policies; policyGrantResource never asks for one.
// https://docs.aws.amazon.com/redshift/latest/dg/r_REVOKE.html#revoke-role-level
func policyGrantTarget(data types.Object) (privilegeTarget, error) {
	kind, err := sqlclient.OneOf(objectString(data, "policy_type"), policyGrantTypes...)
	if err != nil {
		return privilegeTarget{}, fmt.Errorf("policy_type: %w", err)
	}
	database, schemaName, table, policy := objectString(data, "database_name"), objectString(data, "schema_name"), objectString(data, "object_name"), objectString(data, "policy_name")
	checks, err := newCatalogChecks(localDatabaseQuery(database), privilegeSchemaQuery(database, schemaName), policyGrantTableQuery(database, schemaName, table), policyGrantPolicyQuery(kind, database, policy))
	if err != nil {
		return privilegeTarget{}, err
	}
	object := sqlclient.Kw("ON TABLE").Qualified(database, schemaName, table)
	return privilegeTarget{
		database: database, checks: checks, allowed: policyGrantPrivileges,
		grant: grantSpec{object: object},
	}, nil
}
