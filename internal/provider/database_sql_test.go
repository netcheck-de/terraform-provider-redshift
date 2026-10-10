package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
)

// TestDatabaseSQL pins every database statement, including quoting of identifiers and producer bindings.
func TestDatabaseSQL(t *testing.T) {
	local := func(name string) databaseModel {
		return databaseModel{Name: types.StringValue(name), DatashareARN: types.StringNull(), WithPermissions: types.BoolValue(true)}
	}
	options := func(name, owner string, limit int64, collation, isolation string) databaseModel {
		data := local(name)
		data.Owner, data.ConnectionLimit = types.StringValue(owner), types.Int64Value(limit)
		data.Collation, data.IsolationLevel = types.StringValue(collation), types.StringValue(isolation)
		return data
	}
	shared := func(name, arn string, permissions bool) databaseModel {
		return databaseModel{Name: types.StringValue(name), DatashareARN: types.StringValue(arn), WithPermissions: types.BoolValue(permissions)}
	}
	create := func(data databaseModel) func() (string, error) {
		return func() (string, error) { return createDatabaseStatement(data) }
	}
	alter := func(prev, plan databaseModel) func() ([]string, error) {
		return func() ([]string, error) { return alterDatabaseStatements(prev, plan) }
	}
	built := func(query sqlclient.Query, expected map[string]string) func() (string, error) {
		return func() (string, error) {
			sql, parameters, err := query.Build()
			assert.Equal(t, expected, parameters)
			return sql, err
		}
	}
	oddARN := `arn:aws:redshift:eu-central-1:12'3\4:datashare:it's\ns/Odd"Share`
	configured := options("warehouse", "etl", 25, "CASE_SENSITIVE", "SNAPSHOT")
	changed := options("warehouse", `New"Owner`, -1, "CASE_SENSITIVE", "SERIALIZABLE")
	invalidLimit := options("warehouse", "etl", -2, "CASE_SENSITIVE", "SNAPSHOT")
	invalidIsolation := options("warehouse", "etl", 25, "CASE_SENSITIVE", "READ COMMITTED")
	sharedOwner := shared("analytics", shareARN, true)
	sharedOwner.Owner = types.StringValue("etl")
	unmanaged := local("warehouse")
	unmanaged.Owner, unmanaged.ConnectionLimit, unmanaged.IsolationLevel = types.StringNull(), types.Int64Null(), types.StringNull()
	unknownPlan := local("warehouse")
	unknownPlan.Owner, unknownPlan.ConnectionLimit, unknownPlan.IsolationLevel = types.StringUnknown(), types.Int64Unknown(), types.StringUnknown()
	checkSQL(t, "database", []sqlCase{
		{"create_local", create(local("warehouse"))},
		{"create_local_quoted", create(local(`Sales"DB`))},
		{"create_local_options", create(options(`Sales"DB`, `Etl"Owner`, 25, "CASE_INSENSITIVE", "SERIALIZABLE"))},
		{"create_local_unlimited_snapshot", create(options("warehouse", "etl", -1, "CASE_SENSITIVE", "SNAPSHOT"))},
		{"create_local_zero_connections", create(options("warehouse", "etl", 0, "CASE_SENSITIVE", "SNAPSHOT"))},
		{"create_local_unknown_options", create(unknownPlan)},
		{"create_local_invalid_limit", create(invalidLimit)},
		{"create_local_invalid_collation", create(options("warehouse", "etl", 25, "CS", "SNAPSHOT"))},
		{"create_local_invalid_isolation", create(invalidIsolation)},
		{"create_shared", create(shared("analytics", shareARN, true))},
		{"create_shared_without_permissions", create(shared("analytics", shareARN, false))},
		{"create_shared_quoted", create(shared(`Odd"DB`, oddARN, true))},
		{"create_shared_invalid_arn", create(shared("analytics", "arn:aws:s3:::bucket", true))},
		{"create_shared_with_owner", create(sharedOwner)},
		{"alter_unchanged", alter(configured, configured)},
		{"alter_all", alter(configured, changed)},
		{"alter_owner_quoted", alter(options(`Sales"DB`, "etl", 25, "CASE_SENSITIVE", "SNAPSHOT"), options(`Sales"DB`, `New"Owner`, 25, "CASE_SENSITIVE", "SNAPSHOT"))},
		{"alter_connection_limit", alter(changed, configured)},
		{"alter_unknown_plan", alter(configured, unknownPlan)},
		{"alter_unmanaged", alter(configured, unmanaged)},
		{"alter_invalid_limit", alter(configured, invalidLimit)},
		{"alter_invalid_isolation", alter(configured, invalidIsolation)},
		{"drop", func() string { return dropDatabaseStatement(local("warehouse")) }},
		{"drop_quoted", func() string { return dropDatabaseStatement(local(`Sales"DB`)) }},
		{"read", func() string { return readDatabaseStatement("analytics") }},
		{"read_quoted", func() string { return readDatabaseStatement(`it's\_DB`) }},
		{"read_wildcards", func() string { return readDatabaseStatement(`a\b%c_d\\`) }},
		{"read_options", built(readDatabaseOptionsQuery(`it's\"DB`), map[string]string{"name": `it's\"DB`})},
		{"read_collation", built(readDatabaseCollationQuery(), nil)},
		{"read_inbound_share", built(readDatabaseInboundShareQuery(shareSource{Account: "123456789012", Namespace: "ns", Name: `Odd"Share`}),
			map[string]string{"share": `Odd"Share`, "account": "123456789012", "namespace": "ns"})},
	})
}
