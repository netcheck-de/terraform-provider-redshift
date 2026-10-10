package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// tableSecuritySample is a protected relation with explicit conjunction and datashare settings.
func tableSecuritySample() tableSecurityModel {
	return tableSecurityModel{
		Database: types.StringValue("analytics"), Schema: types.StringValue("public"), Relation: types.StringValue("events"),
		RowLevelSecurity: types.BoolValue(true), ConjunctionType: types.StringValue("AND"), DatashareRowLevelSecurity: types.BoolValue(true),
	}
}

// TestTableSecuritySQL pins the ALTER TABLE ROW LEVEL SECURITY forms and the catalog reads.
func TestTableSecuritySQL(t *testing.T) {
	with := func(change func(*tableSecurityModel)) tableSecurityModel {
		data := tableSecuritySample()
		change(&data)
		return data
	}
	create := func(data tableSecurityModel) func() ([]string, error) {
		return func() ([]string, error) { return createTableSecurityStatements(data) }
	}
	alter := func(prev, plan tableSecurityModel) func() ([]string, error) {
		return func() ([]string, error) { return alterTableSecurityStatements(prev, plan) }
	}
	built := func(query sqlclient.Query) func() (string, error) {
		return func() (string, error) {
			sql, _, err := query.Build()
			return sql, err
		}
	}
	quoted := with(func(d *tableSecurityModel) {
		d.Schema, d.Relation = types.StringValue(`Odd"Schema`), types.StringValue(`Odd"Events`)
	})
	off := with(func(d *tableSecurityModel) { d.RowLevelSecurity = types.BoolValue(false) })
	or := with(func(d *tableSecurityModel) { d.ConjunctionType = types.StringValue("or") })
	shared := with(func(d *tableSecurityModel) { d.DatashareRowLevelSecurity = types.BoolValue(false) })
	minimal := with(func(d *tableSecurityModel) {
		d.ConjunctionType, d.DatashareRowLevelSecurity = types.StringUnknown(), types.BoolUnknown()
	})
	checkSQL(t, "table_security", []sqlCase{
		{"create", create(tableSecuritySample())},
		{"create_minimal", create(minimal)},
		{"create_off_or_shared", create(with(func(d *tableSecurityModel) {
			d.RowLevelSecurity, d.ConjunctionType, d.DatashareRowLevelSecurity = types.BoolValue(false), types.StringValue("OR"), types.BoolValue(false)
		}))},
		{"create_quoted", create(quoted)},
		{"create_invalid_conjunction", create(with(func(d *tableSecurityModel) { d.ConjunctionType = types.StringValue("XOR; DROP TABLE t") }))},
		{"create_empty_relation", create(with(func(d *tableSecurityModel) { d.Relation = types.StringValue("") }))},
		{"alter_off", alter(tableSecuritySample(), off)},
		{"alter_conjunction", alter(tableSecuritySample(), or)},
		{"alter_on_and_conjunction", alter(off, with(func(d *tableSecurityModel) { d.ConjunctionType = types.StringValue("OR") }))},
		{"alter_datashare", alter(tableSecuritySample(), shared)},
		{"alter_unchanged", alter(tableSecuritySample(), tableSecuritySample())},
		{"alter_unknown_settings", alter(tableSecuritySample(), with(func(d *tableSecurityModel) {
			d.ConjunctionType, d.DatashareRowLevelSecurity = types.StringUnknown(), types.BoolUnknown()
		}))},
		{"alter_invalid_conjunction", alter(tableSecuritySample(), with(func(d *tableSecurityModel) { d.ConjunctionType = types.StringValue("NOR") }))},
		{"delete", func() (string, error) { return deleteTableSecurityStatement(tableSecuritySample()) }},
		{"delete_quoted", func() (string, error) { return deleteTableSecurityStatement(quoted) }},
		{"delete_empty_schema", func() (string, error) {
			return deleteTableSecurityStatement(with(func(d *tableSecurityModel) { d.Schema = types.StringValue("") }))
		}},
		{"read_relation", built(readTableSecurityRelationQuery(quoted))},
		{"read", built(readTableSecurityQuery(quoted))},
	})
}
