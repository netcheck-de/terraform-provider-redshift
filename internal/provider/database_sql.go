package provider

import (
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// databaseUnlimited is the connection_limit value that renders CONNECTION LIMIT UNLIMITED, matching the -1 that
// PG_DATABASE_INFO.datconnlimit documents for "no limit".
const databaseUnlimited = -1

// databaseCollations are the COLLATE keywords; the CS/CI abbreviations are accepted by Redshift but not offered,
// so configuration and catalog readback share one spelling.
var databaseCollations = []sqlclient.Keyword{"CASE_SENSITIVE", "CASE_INSENSITIVE"}

// databaseIsolationLevels are the ISOLATION LEVEL keywords.
var databaseIsolationLevels = []sqlclient.Keyword{"SERIALIZABLE", "SNAPSHOT"}

// databaseAlter starts every ALTER DATABASE statement for the database.
func databaseAlter(data databaseModel) sqlclient.Statement {
	return sqlclient.Stmt("ALTER DATABASE").Ident(data.Name.ValueString())
}

// databaseConnectionLimit appends CONNECTION LIMIT, spelling the unlimited sentinel as UNLIMITED.
func databaseConnectionLimit(statement sqlclient.Statement, limit int64) sqlclient.Statement {
	if limit == databaseUnlimited {
		return statement.Kw("CONNECTION LIMIT UNLIMITED")
	}
	return statement.KwInt("CONNECTION LIMIT", limit)
}

// databaseOptions checks the local-only options before any of them is rendered, so a renderer never emits a
// statement whose sibling option was refused.
func databaseOptions(data databaseModel) (collation, isolation sqlclient.Keyword, err error) {
	if limit := knownInt64(data.ConnectionLimit); limit != nil && *limit < databaseUnlimited {
		return "", "", fmt.Errorf("connection_limit must be -1 (UNLIMITED) or at least 0, got %d", *limit)
	}
	if collation, err = optOneOf(data.Collation, databaseCollations...); err != nil {
		return "", "", err
	}
	isolation, err = optOneOf(data.IsolationLevel, databaseIsolationLevels...)
	return collation, isolation, err
}

// createDatabaseStatement renders CREATE DATABASE for a local database with its optional OWNER, CONNECTION
// LIMIT, COLLATE and ISOLATION LEVEL, or for a consumer database bound to the producer share encoded in
// datashare_arn, which accepts none of those options.
func createDatabaseStatement(data databaseModel) (string, error) {
	statement := sqlclient.Stmt("CREATE DATABASE").Ident(data.Name.ValueString())
	if data.DatashareARN.IsNull() {
		collation, isolation, err := databaseOptions(data)
		if err != nil {
			return "", err
		}
		statement = statement.OptIdent("OWNER", knownString(data.Owner))
		if limit := knownInt64(data.ConnectionLimit); limit != nil {
			statement = databaseConnectionLimit(statement, *limit)
		}
		statement = statement.When(collation != "", func(s sqlclient.Statement) sqlclient.Statement { return s.Kw("COLLATE", collation) }).
			When(isolation != "", func(s sqlclient.Statement) sqlclient.Statement { return s.Kw("ISOLATION LEVEL", isolation) })
		return statement.String(), statement.Err()
	}
	if err := validateDatabase(data); err != nil {
		return "", err
	}
	source, err := parseShare(data.DatashareARN.ValueString())
	if err != nil {
		return "", err
	}
	statement = statement.If(data.WithPermissions.ValueBool(), "WITH PERMISSIONS").
		KwIdent("FROM DATASHARE", source.Name).KwLit("OF ACCOUNT", source.Account).KwLit("NAMESPACE", source.Namespace)
	return statement.String(), statement.Err()
}

// databaseAlterSteps change one option per statement. Collation is absent because it replaces the database.
// The values were checked by databaseOptions, so the renders cannot meet an invalid keyword.
var databaseAlterSteps = []alterStep[databaseModel]{
	{
		attribute: "owner",
		value:     func(data databaseModel) attr.Value { return data.Owner },
		render: func(_, plan databaseModel) []string {
			return []string{databaseAlter(plan).KwIdent("OWNER TO", plan.Owner.ValueString()).String()}
		},
	},
	{
		attribute: "connection_limit",
		value:     func(data databaseModel) attr.Value { return data.ConnectionLimit },
		render: func(_, plan databaseModel) []string {
			return []string{databaseConnectionLimit(databaseAlter(plan), plan.ConnectionLimit.ValueInt64()).String()}
		},
	},
	{
		attribute: "isolation_level",
		value:     func(data databaseModel) attr.Value { return data.IsolationLevel },
		render: func(_, plan databaseModel) []string {
			isolation, _ := optOneOf(plan.IsolationLevel, databaseIsolationLevels...)
			return []string{databaseAlter(plan).Kw("ISOLATION LEVEL", isolation).String()}
		},
	},
}

// alterDatabaseStatements renders the in-place changes from prev to plan. A null plan value means the option
// is not managed (a shared database, or state written before the option existed), so it never renders a reset.
func alterDatabaseStatements(prev, plan databaseModel) ([]string, error) {
	if _, _, err := databaseOptions(plan); err != nil {
		return nil, err
	}
	var managed []alterStep[databaseModel]
	for _, step := range databaseAlterSteps {
		if !step.value(plan).IsNull() {
			managed = append(managed, step)
		}
	}
	return alterStatements(prev, plan, managed), nil
}

// dropDatabaseStatement renders DROP DATABASE.
func dropDatabaseStatement(data databaseModel) string {
	return sqlclient.Stmt("DROP DATABASE").Ident(data.Name.ValueString()).String()
}

// readDatabaseStatement lists databases matching name. SVV_REDSHIFT_DATABASES.database_options is VARCHAR(128)
// and truncates the producer JSON before its permissions flag, while SHOW returns complete parameters. LIKE
// treats _ and % as wildcards, so callers keep only the exact name. A backslash would escape the character after
// it and the pattern would miss the database, so it becomes _, which matches the backslash itself whether or not
// the pattern honors escapes.
func readDatabaseStatement(name string) string {
	return sqlclient.Stmt("SHOW DATABASES").KwLit("LIKE", strings.ReplaceAll(name, `\`, "_")).String()
}

// readDatabaseOptionsQuery reads a local database's owner name and connection limit. SHOW DATABASES reports the
// owner only as a user ID and omits the limit; PG_DATABASE_INFO holds both, but only for local databases.
func readDatabaseOptionsQuery(name string) sqlclient.Query {
	return sqlclient.Select("u.usename AS owner", "d.datconnlimit AS connection_limit").
		From("pg_database_info d LEFT JOIN pg_user u ON u.usesysid = d.datdba").
		Where("d.datname = :name", sqlclient.Bind("name", name))
}

// readDatabaseCollationQuery reads the collation of the database the query runs in; DB_COLLATION has no argument
// and no catalog column records the collation of another database.
func readDatabaseCollationQuery() sqlclient.Query {
	return sqlclient.Select("db_collation() AS collation")
}

// readDatabaseInboundShareQuery finds the associated inbound share and the database already bound to it.
func readDatabaseInboundShareQuery(source shareSource) sqlclient.Query {
	return sqlclient.Select("consumer_database").From("svv_datashares").
		Where("share_type = 'INBOUND'").
		Where("share_name = :share", sqlclient.Bind("share", source.Name)).
		Where("producer_account = :account", sqlclient.Bind("account", source.Account)).
		Where("producer_namespace = :namespace", sqlclient.Bind("namespace", source.Namespace))
}
