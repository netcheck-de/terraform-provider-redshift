package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// TestRoleSQL pins the role statements and lookup against r_CREATE_ROLE, r_ALTER_ROLE, and r_DROP_ROLE, including
// names, owners, and external IDs that need quoting.
func TestRoleSQL(t *testing.T) {
	role := func(name string) roleModel {
		return roleModel{Name: types.StringValue(name), Owner: types.StringNull(), ExternalID: types.StringNull(), RoleID: types.Int64Null()}
	}
	with := func(model roleModel, owner, external string) roleModel {
		if owner != "" {
			model.Owner = types.StringValue(owner)
		}
		if external != "" {
			model.ExternalID = types.StringValue(external)
		}
		return model
	}
	built := func(query sqlclient.Query) func() (string, error) {
		return func() (string, error) {
			sql, _, err := query.Build()
			return sql, err
		}
	}
	plain, quoted := with(role("readers"), "etl", "ABC123"), with(role(`Odd"Readers`), `Odd"Owner`, `Ext"ID`)
	checkSQL(t, "role", []sqlCase{
		{"create", func() string { return createRoleStatement(role("readers")) }},
		{"create_namespaced", func() string { return createRoleStatement(role("example:readers")) }},
		{"create_quoted", func() string { return createRoleStatement(role(`Odd"Readers`)) }},
		{"create_external_id", func() []string { return createRoleStatements(with(role("aad:readers"), "", "ABC123")) }},
		{"create_owner_and_external_id_quoted", func() []string { return createRoleStatements(quoted) }},
		{"alter_owner", func() []string { return alterRoleStatements(plain, with(plain, "loader", "")) }},
		{"alter_external_id", func() []string { return alterRoleStatements(plain, with(plain, "", "XYZ456")) }},
		{"alter_both_quoted", func() []string { return alterRoleStatements(role(`Odd"Readers`), quoted) }},
		{"alter_unchanged", func() []string { return alterRoleStatements(plain, plain) }},
		{"alter_unset_keeps_catalog", func() []string { return alterRoleStatements(plain, role("readers")) }},
		{"drop", func() string { return dropRoleStatement(role("readers")) }},
		{"drop_quoted", func() string { return dropRoleStatement(role(`Odd"Readers`)) }},
		{"read", built(readRoleQuery(role(`Odd"Readers`)))},
		{"read_empty_name", built(readRoleQuery(role("")))},
	})
}
