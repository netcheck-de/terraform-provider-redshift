package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
)

// TestDatabaseSQL pins every database statement, including quoting of identifiers and producer bindings.
func TestDatabaseSQL(t *testing.T) {
	local := func(name string) databaseModel {
		return databaseModel{Name: types.StringValue(name), DatashareARN: types.StringNull(), WithPermissions: types.BoolValue(true)}
	}
	shared := func(name, arn string, permissions bool) databaseModel {
		return databaseModel{Name: types.StringValue(name), DatashareARN: types.StringValue(arn), WithPermissions: types.BoolValue(permissions)}
	}
	create := func(data databaseModel) func() (string, error) {
		return func() (string, error) { return createDatabaseStatement(data) }
	}
	oddARN := `arn:aws:redshift:eu-central-1:12'3\4:datashare:it's\ns/Odd"Share`
	checkSQL(t, "database", []sqlCase{
		{"create_local", create(local("warehouse"))},
		{"create_local_quoted", create(local(`Sales"DB`))},
		{"create_shared", create(shared("analytics", shareARN, true))},
		{"create_shared_without_permissions", create(shared("analytics", shareARN, false))},
		{"create_shared_quoted", create(shared(`Odd"DB`, oddARN, true))},
		{"create_shared_invalid_arn", create(shared("analytics", "arn:aws:s3:::bucket", true))},
		{"drop", func() string { return dropDatabaseStatement(local("warehouse")) }},
		{"drop_quoted", func() string { return dropDatabaseStatement(local(`Sales"DB`)) }},
		{"read", func() string { return readDatabaseStatement("analytics") }},
		{"read_quoted", func() string { return readDatabaseStatement(`it's\_DB`) }},
		{"read_wildcards", func() string { return readDatabaseStatement(`a\b%c_d\\`) }},
		{"read_inbound_share", func() (string, error) {
			sql, parameters, err := readDatabaseInboundShareQuery(shareSource{Account: "123456789012", Namespace: "ns", Name: `Odd"Share`}).Build()
			assert.Equal(t, map[string]string{"share": `Odd"Share`, "account": "123456789012", "namespace": "ns"}, parameters)
			return sql, err
		}},
	})
}
