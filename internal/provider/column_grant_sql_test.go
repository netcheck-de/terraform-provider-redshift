package provider

import (
	"maps"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// columnGrantTestModel builds a column grant model from identity fields and a privilege map.
func columnGrantTestModel(fields map[string]string, columns map[string][]string) columnGrantModel {
	value := func(name string) types.String {
		if text, ok := fields[name]; ok {
			return types.StringValue(text)
		}
		return types.StringNull()
	}
	return columnGrantModel{
		ID: types.StringNull(), DatabaseName: value("database_name"), SchemaName: value("schema_name"), ObjectName: value("object_name"),
		Grantee: value("grantee"), GranteeType: value("grantee_type"), Privileges: columnGrantValue(columns),
	}
}

// columnGrantBaseFields is a role tuple on the fixture relation.
var columnGrantBaseFields = map[string]string{"database_name": "warehouse", "schema_name": "serving", "object_name": "events", "grantee": "example:readers", "grantee_type": "ROLE"}

// TestColumnGrantSQL pins the checks, catalog read, and column GRANT/REVOKE statements of every grantee form.
func TestColumnGrantSQL(t *testing.T) {
	render := func(changes map[string]string, current, desired map[string][]string) func() ([]string, error) {
		return func() ([]string, error) {
			fields := maps.Clone(columnGrantBaseFields)
			maps.Copy(fields, changes)
			model := columnGrantTestModel(fields, desired)
			if err := validateColumnGrant(model); err != nil {
				return nil, err
			}
			target, err := prepareColumnGrant(model)
			if err != nil {
				return nil, err
			}
			var statements []string
			for _, check := range target.checks {
				statements = append(statements, check.sql)
			}
			query, _, err := target.query.Build()
			if err != nil {
				return nil, err
			}
			mutations, err := columnGrantStatements(target, current, desired)
			return append(append(statements, query), mutations...), err
		}
	}
	both := map[string][]string{"SELECT": {"id", "label"}, "UPDATE": {"label"}}
	checkSQL(t, "column_grant", []sqlCase{
		{"role_grant", render(nil, nil, both)},
		{"converged", render(nil, both, both)},
		{"revoke_before_grant", render(nil, both, map[string][]string{"SELECT": {"label", "note"}, "UPDATE": {"note"}})},
		{"revoke_all", render(nil, both, nil)},
		{"user", render(map[string]string{"grantee_type": "USER", "grantee": "analyst"}, nil, map[string][]string{"UPDATE": {"label"}})},
		{"group", render(map[string]string{"grantee_type": "GROUP", "grantee": "readers"}, nil, map[string][]string{"SELECT": {"id"}})},
		{"public", render(map[string]string{"grantee_type": "PUBLIC", "grantee": "public"}, map[string][]string{"SELECT": {"label"}}, map[string][]string{"SELECT": {"id"}})},
		{"quoted_identifiers", render(map[string]string{"database_name": `Odd"Database`, "schema_name": `Odd"Schema`, "object_name": `Odd"Table`, "grantee": `Odd"Role`}, nil, map[string][]string{"SELECT": {`Odd"Column`, "UPPER"}})},
		{"public_other_name", render(map[string]string{"grantee_type": "PUBLIC", "grantee": "readers"}, nil, both)},
		{"unsupported_grantee_type", render(map[string]string{"grantee_type": "DATASHARE"}, nil, both)},
		{"missing_schema_name", render(map[string]string{"schema_name": ""}, nil, both)},
		{"unsupported_privilege", render(nil, nil, map[string][]string{"DELETE": {"id"}})},
		{"empty_column_set", render(nil, nil, map[string][]string{"SELECT": {}})},
		{"empty_column_name", render(nil, nil, map[string][]string{"SELECT": {""}})},
	})
}

// TestColumnGrantStatementsRejectCatalogPrivileges keeps unknown catalog privilege names out of SQL text.
func TestColumnGrantStatementsRejectCatalogPrivileges(t *testing.T) {
	target, err := prepareColumnGrant(columnGrantTestModel(columnGrantBaseFields, nil))
	require.NoError(t, err)
	_, err = columnGrantStatements(target, columnGrantColumns{"SELECT; DROP TABLE x": {"id"}}, nil)
	require.ErrorContains(t, err, "unsupported column privilege")
	statements, err := columnGrantStatements(target, nil, columnGrantColumns{"SELECT": {"id"}})
	require.NoError(t, err)
	assert.Equal(t, []string{`GRANT SELECT ("id") ON TABLE "warehouse"."serving"."events" TO ROLE "example:readers"`}, statements)
}

// TestColumnGrantRowsGroupColumns checks grouping, sorting, and de-duplication of catalog rows.
func TestColumnGrantRowsGroupColumns(t *testing.T) {
	columns := columnGrantRows([]sqlclient.Row{
		{"column_name": "label", "privilege_type": "SELECT"}, {"column_name": "id", "privilege_type": "SELECT"},
		{"column_name": "id", "privilege_type": "SELECT"}, {"column_name": "label", "privilege_type": "UPDATE"},
	})
	assert.Equal(t, columnGrantColumns{"SELECT": {"id", "label"}, "UPDATE": {"label"}}, columns)
	assert.Equal(t, columns, columnGrantDesired(columnGrantValue(columns)))
	assert.Empty(t, columnGrantDesired(types.MapNull(types.SetType{ElemType: types.StringType})))
	assert.Empty(t, columnGrantDesired(types.MapUnknown(types.SetType{ElemType: types.StringType})))
}
