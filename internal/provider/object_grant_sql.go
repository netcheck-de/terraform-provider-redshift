package provider

import (
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// objectGrantKinds lists object_type values: single objects, then the ALL … IN SCHEMA snapshots.
var objectGrantKinds = []string{"DATABASE", "SCHEMA", "TABLE", "FUNCTION", "PROCEDURE", "ALL TABLES", "ALL FUNCTIONS", "ALL PROCEDURES"}

// objectGrantPrivileges lists the privileges GRANT documents per object type. ALL TABLES keeps to the privileges
// SVV_RELATION_PRIVILEGES reports, because a snapshot is read back from it.
// https://docs.aws.amazon.com/redshift/latest/dg/r_GRANT.html
// https://docs.aws.amazon.com/redshift/latest/dg/r_SVV_RELATION_PRIVILEGES.html
var objectGrantPrivileges = map[string][]sqlclient.Keyword{
	"DATABASE":       {"CREATE", "USAGE", "TEMPORARY", "ALTER"},
	"SCHEMA":         {"CREATE", "USAGE", "ALTER", "DROP"},
	"TABLE":          {"SELECT", "INSERT", "UPDATE", "DELETE", "DROP", "REFERENCES", "ALTER", "TRUNCATE"},
	"FUNCTION":       {"EXECUTE"},
	"PROCEDURE":      {"EXECUTE"},
	"ALL TABLES":     {"SELECT", "INSERT", "UPDATE", "DELETE", "DROP", "REFERENCES"},
	"ALL FUNCTIONS":  {"EXECUTE"},
	"ALL PROCEDURES": {"EXECUTE"},
}

// objectGrantTableQuery confirms that a table or view exists.
func objectGrantTableQuery(database, schema, name string) sqlclient.Query {
	return sqlclient.Select("table_name").From("svv_all_tables").
		Where("database_name = :database", sqlclient.Bind("database", database)).
		Where("schema_name = :schema", sqlclient.Bind("schema", schema)).
		Where("table_name = :name", sqlclient.Bind("name", name))
}

// objectGrantRoutineQuery confirms that one function or procedure overload exists. The catalog lists argument types
// without length or precision, as objectGrantSignature renders them.
// https://docs.aws.amazon.com/redshift/latest/dg/r_SVV_REDSHIFT_FUNCTIONS.html
func objectGrantRoutineQuery(database, schema, name, arguments string) sqlclient.Query {
	return sqlclient.Select("function_name").From("svv_redshift_functions").
		Where("database_name = :database", sqlclient.Bind("database", database)).
		Where("schema_name = :schema", sqlclient.Bind("schema", schema)).
		Where("function_name = :name", sqlclient.Bind("name", name)).
		WhereEither(arguments != "", "argument_type = :arguments", "NVL(argument_type, '') = ''", sqlclient.Bind("arguments", arguments))
}

// objectGrantMembersQuery confirms that a snapshot has objects to cover: an empty schema cannot hold a privilege
// common to all its objects, so the grant is treated as missing rather than never converging.
func objectGrantMembersQuery(source sqlclient.Keyword, database, schema string) sqlclient.Query {
	return sqlclient.Select("DISTINCT schema_name").From(source).
		Where("database_name = :database", sqlclient.Bind("database", database)).
		Where("schema_name = :schema", sqlclient.Bind("schema", schema))
}

// objectGrantRoutineKind selects procedures or every other routine kind by SVV_REDSHIFT_FUNCTIONS.function_type, so
// the existence check, the object count, and the privilege rows of a routine snapshot all classify routines alike.
// The view documents regular functions, aggregate functions, and stored procedures but shows only the spelling
// 'REGULAR FUNCTION', so procedures are matched by the word rather than by one exact value.
func objectGrantRoutineKind(alias sqlclient.Keyword, procedures bool) sqlclient.Keyword {
	if procedures {
		return "NVL(" + alias + "function_type, '') ILIKE '%PROCEDURE%'"
	}
	return "NVL(" + alias + "function_type, '') NOT ILIKE '%PROCEDURE%'"
}

// readObjectGrantRoutineQuery reads one routine's privileges for one grantee. SHOW GRANTS has no procedure form, so
// both kinds use SVV_FUNCTION_PRIVILEGES of the current database.
// https://docs.aws.amazon.com/redshift/latest/dg/r_SVV_FUNCTION_PRIVILEGES.html
func readObjectGrantRoutineQuery(schema, name, arguments, grantee, granteeType string) sqlclient.Query {
	return sqlclient.Select("privilege_type", "admin_option").From("svv_function_privileges").
		Where("namespace_name = :schema", sqlclient.Bind("schema", schema)).
		Where("function_name = :name", sqlclient.Bind("name", name)).
		WhereEither(arguments != "", "argument_types = :arguments", "NVL(argument_types, '') = ''", sqlclient.Bind("arguments", arguments)).
		Where("identity_name = :grantee", sqlclient.Bind("grantee", grantee)).
		Where("identity_type = LOWER(:kind)", sqlclient.Bind("kind", granteeType))
}

// Snapshot reads keep a privilege only when every current object of the schema holds it. Each object first keeps
// its strongest row, because another grantor's plain grant can report the same privilege beside the one with the
// option; the privilege then carries the grant option only when every object holds it with the option.
const (
	objectGrantSnapshotColumns sqlclient.Keyword = "privilege_type, CASE WHEN MIN(opt) = 1 THEN 'true' ELSE 'false' END AS admin_option, COUNT(*) AS objects"
	objectGrantStrongest       sqlclient.Keyword = "MAX(CASE WHEN admin_option THEN 1 ELSE 0 END) AS opt"
	objectGrantAllTables       sqlclient.Keyword = "(SELECT " + objectGrantSnapshotColumns + " FROM (SELECT relation_name, privilege_type, " + objectGrantStrongest +
		" FROM svv_relation_privileges WHERE namespace_name = :schema AND identity_name = :grantee AND identity_type = LOWER(:kind)" +
		" AND privilege_type IN ('SELECT', 'INSERT', 'UPDATE', 'DELETE', 'DROP', 'REFERENCES') GROUP BY relation_name, privilege_type) AS per_object" +
		" GROUP BY privilege_type) AS granted"
	objectGrantAllTablesCount sqlclient.Keyword = "objects = (SELECT COUNT(*) FROM svv_all_tables WHERE database_name = :database AND schema_name = :schema)"
)

// objectGrantRoutineSnapshot renders the source and object count of an ALL FUNCTIONS or ALL PROCEDURES snapshot.
// Privilege rows are matched to their exact overload in SVV_REDSHIFT_FUNCTIONS, so a function and a procedure that
// share a name never count toward each other's snapshot.
// https://docs.aws.amazon.com/redshift/latest/dg/r_SVV_REDSHIFT_FUNCTIONS.html
func objectGrantRoutineSnapshot(procedures bool) (source, count sqlclient.Keyword) {
	source = "(SELECT " + objectGrantSnapshotColumns + " FROM (SELECT function_name, NVL(argument_types, '') AS arguments, privilege_type, " + objectGrantStrongest +
		" FROM svv_function_privileges f WHERE namespace_name = :schema AND identity_name = :grantee AND identity_type = LOWER(:kind) AND privilege_type = 'EXECUTE'" +
		" AND EXISTS (SELECT 1 FROM svv_redshift_functions r WHERE r.database_name = :database AND r.schema_name = f.namespace_name" +
		" AND r.function_name = f.function_name AND NVL(r.argument_type, '') = NVL(f.argument_types, '') AND " + objectGrantRoutineKind("r.", procedures) + ")" +
		" GROUP BY function_name, NVL(argument_types, ''), privilege_type) AS per_object GROUP BY privilege_type) AS granted"
	count = "objects = (SELECT COUNT(*) FROM svv_redshift_functions WHERE database_name = :database AND schema_name = :schema AND " + objectGrantRoutineKind("", procedures) + ")"
	return source, count
}

// readObjectGrantSnapshotQuery reads the privileges a grantee holds on every current object of one kind in a schema.
func readObjectGrantSnapshotQuery(kind, database, schema, grantee, granteeType string) sqlclient.Query {
	identity := []sqlclient.Param{sqlclient.Bind("schema", schema), sqlclient.Bind("grantee", grantee), sqlclient.Bind("kind", granteeType)}
	source, count := objectGrantAllTables, objectGrantAllTablesCount
	if kind != "ALL TABLES" {
		source, count = objectGrantRoutineSnapshot(kind == "ALL PROCEDURES")
		identity = append(identity, sqlclient.Bind("database", database))
	}
	return sqlclient.Select("privilege_type", "admin_option").From(source, identity...).
		Where(count, sqlclient.Bind("database", database), sqlclient.Bind("schema", schema))
}

// objectGrantSignature canonicalizes a comma-separated argument type list into the spelling the routine catalogs
// report: canonical type names without length or precision, which do not distinguish overloads.
// https://docs.aws.amazon.com/redshift/latest/dg/stored-procedure-naming.html
func objectGrantSignature(arguments string) (sqlclient.Keyword, error) {
	if strings.TrimSpace(arguments) == "" {
		return "", nil
	}
	var types []string
	depth, start := 0, 0
	for i, char := range arguments {
		switch char {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				types = append(types, arguments[start:i])
				start = i + 1
			}
		}
	}
	types = append(types, arguments[start:])
	for i, argument := range types {
		name, err := sqlclient.TypeName(strings.TrimSpace(argument))
		if err != nil {
			return "", fmt.Errorf("arguments: %w", err)
		}
		if open := strings.IndexByte(string(name), '('); open >= 0 {
			name = name[:open]
		}
		types[i] = string(name)
	}
	return sqlclient.Signature(types...)
}

// objectGrantTarget validates an object tuple and renders its checks, catalog read, and grants.
func objectGrantTarget(data types.Object) (privilegeTarget, error) {
	grantee, check, err := principal(data)
	if err != nil {
		return privilegeTarget{}, err
	}
	database, schemaName, name, kind := objectString(data, "database_name"), objectString(data, "schema_name"), objectString(data, "object_name"), objectString(data, "object_type")
	arguments := objectString(data, "arguments")
	allowed, ok := objectGrantPrivileges[kind]
	if !ok {
		return privilegeTarget{}, fmt.Errorf("unsupported object_type %q", kind)
	}
	routine := kind == "FUNCTION" || kind == "PROCEDURE"
	if arguments != "" && !routine {
		return privilegeTarget{}, fmt.Errorf("arguments is only valid for FUNCTION and PROCEDURE")
	}
	named := kind == "TABLE" || routine
	switch {
	case kind == "DATABASE" && (schemaName != "" || name != ""):
		return privilegeTarget{}, fmt.Errorf("DATABASE does not accept schema_name or object_name")
	case kind != "DATABASE" && schemaName == "":
		return privilegeTarget{}, fmt.Errorf("schema_name is required for %s", kind)
	case named && name == "":
		return privilegeTarget{}, fmt.Errorf("object_name is required for %s", kind)
	case !named && name != "":
		return privilegeTarget{}, fmt.Errorf("%s does not accept object_name", kind)
	}
	signature, err := objectGrantSignature(arguments)
	if err != nil {
		return privilegeTarget{}, err
	}
	queries := []sqlclient.Query{localDatabaseQuery(database)}
	granteeName, granteeType := objectString(data, "grantee"), objectString(data, "grantee_type")
	var object sqlclient.Statement
	var read sqlclient.Query
	showGrants := false
	switch kind {
	case "DATABASE":
		object, showGrants = sqlclient.Kw("ON DATABASE").Ident(database), true
	case "SCHEMA":
		queries = append(queries, privilegeSchemaQuery(database, schemaName))
		object, showGrants = sqlclient.Kw("ON SCHEMA").Qualified(database, schemaName), true
	case "TABLE":
		queries = append(queries, privilegeSchemaQuery(database, schemaName), objectGrantTableQuery(database, schemaName, name))
		object, showGrants = sqlclient.Kw("ON TABLE").Qualified(database, schemaName, name), true
	case "FUNCTION", "PROCEDURE":
		queries = append(queries, privilegeSchemaQuery(database, schemaName), objectGrantRoutineQuery(database, schemaName, name, string(signature)))
		keyword, err := sqlclient.OneOf(kind, "FUNCTION", "PROCEDURE")
		if err != nil {
			return privilegeTarget{}, err
		}
		object = sqlclient.Kw("ON", keyword).Qualified(database, schemaName, name).Args(sqlclient.Kw(signature))
		read = readObjectGrantRoutineQuery(schemaName, name, string(signature), granteeName, granteeType)
	case "ALL TABLES":
		queries = append(queries, objectGrantMembersQuery("svv_all_tables", database, schemaName))
		object = sqlclient.Kw("ON ALL TABLES IN SCHEMA").Ident(schemaName)
		read = readObjectGrantSnapshotQuery(kind, database, schemaName, granteeName, granteeType)
	default:
		// A schema holding only the other routine kind has nothing for this snapshot to cover.
		queries = append(queries, objectGrantMembersQuery("svv_redshift_functions", database, schemaName).Where(objectGrantRoutineKind("", kind == "ALL PROCEDURES")))
		keyword, err := sqlclient.OneOf(kind, "ALL FUNCTIONS", "ALL PROCEDURES")
		if err != nil {
			return privilegeTarget{}, err
		}
		object = sqlclient.Kw("ON", keyword).KwIdent("IN SCHEMA", schemaName)
		read = readObjectGrantSnapshotQuery(kind, database, schemaName, granteeName, granteeType)
	}
	checks, err := newCatalogChecks(queries...)
	if err != nil {
		return privilegeTarget{}, err
	}
	target := privilegeTarget{database: database, checks: append([]catalogCheck{check}, checks...), allowed: allowed, grant: grantSpec{object: object, grantee: grantee}}
	if showGrants {
		target.query = catalogCheck{sql: sqlclient.Stmt("SHOW GRANTS").Append(object).String()}
		target.filter = func(row sqlclient.Row) bool {
			return row["identity_name"] == granteeName && strings.EqualFold(row["identity_type"], granteeType) && row["privilege_scope"] == kind
		}
		return target, nil
	}
	target.query, err = newCatalogCheck(read)
	return target, err
}
