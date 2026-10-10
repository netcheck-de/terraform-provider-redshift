package provider

import (
	"fmt"
	"slices"
	"strings"

	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// scopedPrivileges limits emitted SQL to supported privilege keywords; grantScopePrivileges narrows it per scope.
var scopedPrivileges = []sqlclient.Keyword{"USAGE", "CREATE", "TEMPORARY", "SELECT", "INSERT", "UPDATE", "DELETE", "DROP", "REFERENCES", "TRUNCATE", "ALTER", "EXECUTE"}

// grantScopePrivileges lists the privileges GRANT documents for each scope: the ON DATABASE and ON SCHEMA forms and
// the scoped FOR … IN forms. FUNCTIONS and PROCEDURES share one catalog scope.
// https://docs.aws.amazon.com/redshift/latest/dg/r_GRANT.html#grant-scoped-syntax
var grantScopePrivileges = map[string][]sqlclient.Keyword{
	"DATABASE":   {"CREATE", "USAGE", "TEMPORARY", "ALTER"},
	"SCHEMAS":    {"CREATE", "USAGE", "ALTER", "DROP"},
	"SCHEMA":     {"CREATE", "USAGE", "ALTER", "DROP"},
	"TABLES":     {"SELECT", "INSERT", "UPDATE", "DELETE", "DROP", "ALTER", "TRUNCATE", "REFERENCES"},
	"FUNCTIONS":  {"EXECUTE"},
	"PROCEDURES": {"EXECUTE"},
	"LANGUAGES":  {"USAGE"},
	"COPY JOBS":  {"CREATE", "ALTER", "DROP"},
	"TEMPLATES":  {"ALTER", "DROP", "USAGE"},
}

// grantScopes lists the scope values in documentation order.
var grantScopes = []string{"DATABASE", "SCHEMAS", "SCHEMA", "TABLES", "FUNCTIONS", "PROCEDURES", "LANGUAGES", "COPY JOBS", "TEMPLATES"}

// grantSchemaScopes are the scopes that accept schema_name: SCHEMA requires it, the others narrow to one schema.
var grantSchemaScopes = []string{"SCHEMA", "TABLES", "FUNCTIONS", "PROCEDURES", "TEMPLATES"}

// grantScopeNames lists every privilege keyword that some scope accepts, for the schema validator.
func grantScopeNames() []string {
	var names []string
	for _, scope := range grantScopes {
		for _, name := range privilegeNames(grantScopePrivileges[scope]) {
			if !slices.Contains(names, name) {
				names = append(names, name)
			}
		}
	}
	return names
}

// recipient returns the configured recipient kind and name: ROLE, USER, or DATASHARE.
func (data grantModel) recipient() (kind, name string, err error) {
	var kinds []string
	for _, candidate := range []struct{ kind, name string }{{"ROLE", knownString(data.Role)}, {"USER", knownString(data.User)}, {"DATASHARE", knownString(data.Datashare)}} {
		if candidate.name != "" {
			kind, name = candidate.kind, candidate.name
			kinds = append(kinds, candidate.kind)
		}
	}
	if len(kinds) != 1 {
		return "", "", fmt.Errorf("configure exactly one nonempty role, user, or datashare")
	}
	return kind, name, nil
}

// grantDatabaseTypeQuery reads whether the granted database is local or shared, which selects the connection.
func grantDatabaseTypeQuery(database string) sqlclient.Query {
	return sqlclient.Select("database_type").From("svv_redshift_databases").Where("database_name = :database", sqlclient.Bind("database", database))
}

// grantRecipientQuery confirms that the receiving role, user, or outbound datashare exists.
func grantRecipientQuery(data grantModel) sqlclient.Query {
	if share := knownString(data.Datashare); share != "" {
		return sqlclient.Select("share_name").From("svv_datashares").Where("share_type = 'OUTBOUND'").Where("share_name = :share", sqlclient.Bind("share", share))
	}
	if user := knownString(data.User); user != "" {
		return privilegeUserQuery(user)
	}
	return sqlclient.Select("role_name").From("svv_roles").Where("role_name = :role", sqlclient.Bind("role", data.Role.ValueString()))
}

// readGrantStatement lists the recipient's grants. A datashare's schema grants are only visible on the schema itself.
// SHOW GRANTS FOR a user omits admin_option, so a user's grants are read on the database or schema instead, which
// reports whether each privilege carries the grant option.
// https://docs.aws.amazon.com/redshift/latest/dg/r_SHOW_GRANTS.html
func readGrantStatement(data grantModel) string {
	if knownString(data.Datashare) != "" {
		return sqlclient.Stmt("SHOW GRANTS ON SCHEMA").Ident(data.SchemaName.ValueString()).String()
	}
	if user := knownString(data.User); user != "" {
		if schema := knownString(data.SchemaName); schema != "" {
			return sqlclient.Stmt("SHOW GRANTS ON SCHEMA").Qualified(data.DatabaseName.ValueString(), schema).KwIdent("FOR", user).String()
		}
		return sqlclient.Stmt("SHOW GRANTS ON DATABASE").Ident(data.DatabaseName.ValueString()).KwIdent("FOR", user).String()
	}
	return sqlclient.Stmt("SHOW GRANTS FOR ROLE").Ident(data.Role.ValueString()).KwIdent("FROM DATABASE", data.DatabaseName.ValueString()).String()
}

// grantRowMatches reports whether a SHOW GRANTS row belongs to the tuple. Reads on a database or schema list
// every identity and omit object_type, so the identity type and the empty object type are accepted there.
func (data grantModel) grantRowMatches(row sqlclient.Row, identity string) bool {
	schemaName, scope := knownString(data.SchemaName), data.Scope.ValueString()
	objectType := "DATABASE"
	if schemaName != "" {
		objectType = "SCHEMA"
	}
	routine := func(value string) bool { return value == "FUNCTIONS" || value == "PROCEDURES" }
	matchingScope := row["privilege_scope"] == scope || (routine(scope) && routine(row["privilege_scope"]))
	matchingObject := row["object_type"] == objectType
	if knownString(data.User) != "" {
		matchingObject = (row["object_type"] == "" || matchingObject) && strings.EqualFold(row["identity_type"], "user")
	}
	return row["identity_name"] == identity && row["database_name"] == data.DatabaseName.ValueString() && matchingObject && matchingScope && (schemaName == "" || row["schema_name"] == schemaName) && (schemaName != "" || row["schema_name"] == "")
}

// spec renders the scope's ON or FOR clause and the recipient. Datashare grants run inside the producer database,
// so their schema is unqualified; role and user grants name the database because shared databases run from admin.
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
	if user := knownString(data.User); user != "" {
		spec.grantee = sqlclient.Ident(user)
	}
	switch {
	case schemaName == "" && scope == "DATABASE":
		spec.object = sqlclient.Fragment().KwIdent("ON DATABASE", database)
	case schemaName != "" && scope == "SCHEMA":
		spec.object = sqlclient.Fragment().KwQualified("ON SCHEMA", database, schemaName)
	default:
		keyword, err := sqlclient.OneOf(scope, "SCHEMAS", "TABLES", "FUNCTIONS", "PROCEDURES", "LANGUAGES", "COPY JOBS", "TEMPLATES")
		if err != nil {
			return grantSpec{}, err
		}
		spec.object, spec.optionRevoke = sqlclient.Kw("FOR", keyword), scopedOptionRevoke
		if schemaName != "" {
			spec.object = spec.object.KwIdent("IN SCHEMA", schemaName).KwIdent("DATABASE", database)
		} else {
			spec.object = spec.object.KwIdent("IN DATABASE", database)
		}
	}
	return spec, nil
}

// allowed returns the privileges the tuple's scope and recipient accept; a datashare takes one fixed privilege.
func (data grantModel) allowed() []sqlclient.Keyword {
	if knownString(data.Datashare) != "" {
		if data.Scope.ValueString() == "TABLES" {
			return []sqlclient.Keyword{"SELECT"}
		}
		return []sqlclient.Keyword{"USAGE"}
	}
	return grantScopePrivileges[data.Scope.ValueString()]
}
