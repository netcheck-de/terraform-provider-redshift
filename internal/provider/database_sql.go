package provider

import (
	"strings"

	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// createDatabaseStatement renders CREATE DATABASE for a local database, or for a consumer database bound to
// the producer share encoded in datashare_arn.
func createDatabaseStatement(data databaseModel) (string, error) {
	statement := sqlclient.Stmt("CREATE DATABASE").Ident(data.Name.ValueString())
	if data.DatashareARN.IsNull() {
		return statement.String(), statement.Err()
	}
	source, err := parseShare(data.DatashareARN.ValueString())
	if err != nil {
		return "", err
	}
	statement = statement.If(data.WithPermissions.ValueBool(), "WITH PERMISSIONS").
		KwIdent("FROM DATASHARE", source.Name).KwLit("OF ACCOUNT", source.Account).KwLit("NAMESPACE", source.Namespace)
	return statement.String(), statement.Err()
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

// readDatabaseInboundShareQuery finds the associated inbound share and the database already bound to it.
func readDatabaseInboundShareQuery(source shareSource) sqlclient.Query {
	return sqlclient.Select("consumer_database").From("svv_datashares").
		Where("share_type = 'INBOUND'").
		Where("share_name = :share", sqlclient.Bind("share", source.Name)).
		Where("producer_account = :account", sqlclient.Bind("account", source.Account)).
		Where("producer_namespace = :namespace", sqlclient.Bind("namespace", source.Namespace))
}
