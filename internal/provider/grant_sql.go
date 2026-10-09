package provider

import (
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// scopedPrivileges limits emitted SQL to supported privilege keywords.
var scopedPrivileges = []sqlclient.Keyword{"USAGE", "CREATE", "TEMPORARY", "SELECT", "INSERT", "UPDATE", "DELETE", "DROP", "REFERENCES", "TRUNCATE", "ALTER", "EXECUTE"}

// grantDatabaseTypeQuery reads whether the granted database is local or shared, which selects the connection.
func grantDatabaseTypeQuery(database string) sqlclient.Query {
	return sqlclient.Select("database_type").From("svv_redshift_databases").Where("database_name = :database", sqlclient.Bind("database", database))
}

// grantRecipientQuery confirms that the receiving role or outbound datashare exists.
func grantRecipientQuery(data grantModel) sqlclient.Query {
	if share := knownString(data.Datashare); share != "" {
		return sqlclient.Select("share_name").From("svv_datashares").Where("share_type = 'OUTBOUND'").Where("share_name = :share", sqlclient.Bind("share", share))
	}
	return sqlclient.Select("role_name").From("svv_roles").Where("role_name = :role", sqlclient.Bind("role", data.Role.ValueString()))
}

// readGrantStatement lists the recipient's grants; a datashare's schema grants are only visible on the schema itself.
func readGrantStatement(data grantModel) string {
	if knownString(data.Datashare) != "" {
		return sqlclient.Stmt("SHOW GRANTS ON SCHEMA").Ident(data.SchemaName.ValueString()).String()
	}
	return sqlclient.Stmt("SHOW GRANTS FOR ROLE").Ident(data.Role.ValueString()).KwIdent("FROM DATABASE", data.DatabaseName.ValueString()).String()
}

// spec renders the scope's ON or FOR clause and the recipient. Datashare grants run inside the producer database,
// so their schema is unqualified; role grants name the database because shared databases run from admin.
func (data grantModel) spec() (grantSpec, error) {
	database, schemaName, scope := data.DatabaseName.ValueString(), knownString(data.SchemaName), data.Scope.ValueString()
	if share := knownString(data.Datashare); share != "" {
		object := sqlclient.Fragment().KwIdent("ON SCHEMA", schemaName)
		if scope == "TABLES" {
			object = sqlclient.Fragment().KwIdent("FOR TABLES IN SCHEMA", schemaName)
		}
		return grantSpec{object: object, grantee: sqlclient.Fragment().KwIdent("DATASHARE", share)}, nil
	}
	spec := grantSpec{grantee: sqlclient.Fragment().KwIdent("ROLE", data.Role.ValueString())}
	switch {
	case schemaName == "" && scope == "DATABASE":
		spec.object = sqlclient.Fragment().KwIdent("ON DATABASE", database)
	case schemaName != "" && scope == "SCHEMA":
		spec.object = sqlclient.Fragment().KwQualified("ON SCHEMA", database, schemaName)
	default:
		keyword, err := sqlclient.OneOf(scope, "SCHEMAS", "TABLES", "FUNCTIONS", "PROCEDURES")
		if err != nil {
			return grantSpec{}, err
		}
		spec.object = sqlclient.Kw("FOR", keyword)
		if schemaName != "" {
			spec.object = spec.object.KwIdent("IN SCHEMA", schemaName).KwIdent("DATABASE", database)
		} else {
			spec.object = spec.object.KwIdent("IN DATABASE", database)
		}
	}
	return spec, nil
}
