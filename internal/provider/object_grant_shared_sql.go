package provider

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// Shared routines have one name-wide ACL, including shared producer procedures, as SHOW GRANTS ON FUNCTION reports.
var sharedObjectGrantKinds = []string{"DATABASE", "SCHEMA", "TABLE", "FUNCTION", "PROCEDURE", "ALL TABLES", "ALL FUNCTIONS", "ALL PROCEDURES"}

func objectGrantConfigAllowed(data types.Object) ([]sqlclient.Keyword, error) {
	if err := validateObjectGrantConfig(data); err != nil {
		return nil, err
	}
	kind := objectString(data, "object_type")
	if objectString(data, "database_type") == "shared" && (kind == "DATABASE" || kind == "SCHEMA") {
		return []sqlclient.Keyword{"USAGE"}, nil
	}
	return objectGrantPrivileges[kind], nil
}

// validateObjectGrantConfig checks only known selectors, even when generated database or grantee names are unknown.
func validateObjectGrantConfig(data types.Object) error {
	kind := objectString(data, "object_type")
	if objectString(data, "database_type") == "shared" && privilegeFieldsKnown(data, "arguments") && objectString(data, "arguments") != "" {
		return fmt.Errorf("shared routine grants are name-wide, not overload-specific; omit arguments")
	}
	if !privilegeFieldsKnown(data, "object_type") {
		return nil
	}
	if _, ok := objectGrantPrivileges[kind]; !ok {
		return fmt.Errorf("unsupported object_type %q", kind)
	}
	if privilegeFieldsKnown(data, "database_type") && objectString(data, "database_type") != "" {
		if objectString(data, "database_type") != "shared" || !slices.Contains(sharedObjectGrantKinds, kind) {
			return fmt.Errorf("shared object grants support only DATABASE, SCHEMA, TABLE, FUNCTION, PROCEDURE, ALL TABLES, ALL FUNCTIONS, and ALL PROCEDURES")
		}
	}
	if privilegeFieldsKnown(data, "arguments") && objectString(data, "arguments") != "" && kind != "FUNCTION" && kind != "PROCEDURE" {
		return fmt.Errorf("arguments is only valid for FUNCTION and PROCEDURE")
	}
	if privilegeFieldsKnown(data, "arguments") && (kind == "FUNCTION" || kind == "PROCEDURE") {
		if _, err := objectGrantSignature(objectString(data, "arguments")); err != nil {
			return err
		}
	}
	named := kind == "TABLE" || kind == "FUNCTION" || kind == "PROCEDURE" || kind == "MODEL" || kind == "TEMPLATE"
	if privilegeFieldsKnown(data, "schema_name") {
		schema := objectString(data, "schema_name")
		if kind == "DATABASE" && schema != "" {
			return fmt.Errorf("DATABASE does not accept schema_name or object_name")
		}
		if kind != "DATABASE" && schema == "" {
			return fmt.Errorf("schema_name is required for %s", kind)
		}
	}
	if privilegeFieldsKnown(data, "object_name") {
		name := objectString(data, "object_name")
		if named && name == "" {
			return fmt.Errorf("object_name is required for %s", kind)
		}
		if !named && name != "" {
			return fmt.Errorf("%s does not accept object_name", kind)
		}
	}
	return nil
}

// sharedObjectGrantTarget never connects to the shared database or reads local permission views. Consumer grants use
// qualified names and SHOW GRANTS from admin; SVV_ALL_* sees shared parents and scopes stay distinct from FOR TABLES.
// https://docs.aws.amazon.com/redshift/latest/dg/r_GRANT.html#grant-datashare-syntax
// https://docs.aws.amazon.com/redshift/latest/dg/r_SHOW_GRANTS.html
func sharedObjectGrantTarget(data types.Object) (privilegeTarget, error) {
	if err := validateObjectGrantConfig(data); err != nil {
		return privilegeTarget{}, err
	}
	grantee, principalCheck, err := principal(data)
	if err != nil {
		return privilegeTarget{}, err
	}
	database, schema, name, kind := objectString(data, "database_name"), objectString(data, "schema_name"), objectString(data, "object_name"), objectString(data, "object_type")
	queries := []sqlclient.Query{grantDatabaseTypeQuery(database).Where("database_type = 'shared'")}
	parts := []string{database}
	allowed := []sqlclient.Keyword{"USAGE"}
	scope := kind
	var snapshot *privilegeSnapshot
	if kind != "DATABASE" {
		parts = append(parts, schema)
		queries = append(queries, privilegeSchemaQuery(database, schema))
	}
	switch kind {
	case "TABLE":
		parts = append(parts, name)
		queries = append(queries, objectGrantTableQuery(database, schema, name))
		allowed = objectGrantPrivileges["TABLE"]
	case "FUNCTION", "PROCEDURE":
		parts = append(parts, name)
		queries = append(queries, sharedObjectGrantFunctionQuery(database, schema, name).Where(objectGrantRoutineKind("", kind == "PROCEDURE")))
		allowed = objectGrantPrivileges["FUNCTION"]
		scope = "FUNCTION"
	case "ALL TABLES", "ALL FUNCTIONS", "ALL PROCEDURES":
		allowed = objectGrantPrivileges[kind]
		scope = "TABLE"
		if kind != "ALL TABLES" {
			scope = "FUNCTION"
		}
		memberQuery := sharedObjectGrantMembersQuery(database, schema, scope)
		if kind == "ALL PROCEDURES" {
			memberQuery = sharedObjectGrantProcedureMembersQuery(database, schema)
		}
		members, err := newCatalogCheck(memberQuery)
		if err != nil {
			return privilegeTarget{}, err
		}
		snapshot = &privilegeSnapshot{members: members, query: func(row sqlclient.Row) catalogCheck {
			return catalogCheck{sql: sharedObjectGrantShow(database, schema, row["object_name"], scope)}
		}}
		if kind == "ALL PROCEDURES" {
			// Redshift rejects native shared ON ALL PROCEDURES. Each listed procedure instead uses the verified
			// name-wide FUNCTION ACL, and parent-kind checks keep a FUNCTION tuple from selecting that procedure.
			snapshot.grant = func(row sqlclient.Row) grantSpec {
				return grantSpec{object: sqlclient.Kw("ON FUNCTION").Qualified(database, schema, row["object_name"]), grantee: grantee}
			}
			snapshot.filterMembers = sharedObjectGrantProcedureMembers
		}
	default:
		if len(grantOptionPrivileges(data)) != 0 {
			return privilegeTarget{}, fmt.Errorf("shared DATABASE and SCHEMA USAGE do not support grant options")
		}
	}
	statementKind := kind
	if kind == "PROCEDURE" {
		statementKind = "FUNCTION"
	}
	if kind == "ALL PROCEDURES" {
		statementKind = "ALL FUNCTIONS"
	}
	keyword, err := sqlclient.OneOf(statementKind, "DATABASE", "SCHEMA", "TABLE", "FUNCTION", "ALL TABLES", "ALL FUNCTIONS")
	if err != nil {
		return privilegeTarget{}, err
	}
	object := sqlclient.Kw("ON", keyword).Qualified(parts...)
	if snapshot != nil {
		object = sqlclient.Kw("ON", keyword).Kw("IN SCHEMA").Qualified(database, schema)
	}
	checks, err := newCatalogChecks(queries...)
	if err != nil {
		return privilegeTarget{}, err
	}
	target := privilegeTarget{
		checks: append([]catalogCheck{principalCheck}, checks...), allowed: allowed,
		grant:     grantSpec{object: object, grantee: grantee},
		query:     catalogCheck{sql: sqlclient.Stmt("SHOW GRANTS").Append(object).String()},
		snapshot:  snapshot,
		scopeWide: snapshot != nil,
		filter: func(row sqlclient.Row) bool {
			return row["identity_name"] == objectString(data, "grantee") && strings.EqualFold(row["identity_type"], objectString(data, "grantee_type")) && row["privilege_scope"] == scope && (snapshot == nil || privilegeAllowed(allowed, normalizePrivilege(row["privilege_type"])))
		},
	}
	target.completeCatalog = kind == "FUNCTION" || kind == "PROCEDURE" || snapshot != nil
	if snapshot != nil {
		// The common set cannot expose partially held privileges. Revoke every undesired owned command, even when
		// it is absent from that intersection, so update and destroy cannot leave partial grants behind.
		target.implicit = privilegeNames(allowed)
		target.query = catalogCheck{}
	}
	if kind == "FUNCTION" || kind == "PROCEDURE" {
		guard, err := newCatalogCheck(sharedObjectGrantAmbiguityQuery(database, schema, name).Where(objectGrantRoutineKind("", kind != "PROCEDURE")))
		if err != nil {
			return privilegeTarget{}, err
		}
		target.mutationGuard = &guard
		target.mutationConflict = "shared function and procedure overloads use one name-wide ACL; this name exists as both kinds. Use one ALL FUNCTIONS tuple or separate producer names instead of independently managing ambiguous FUNCTION/PROCEDURE tuples"
	}
	return target, nil
}

func sharedObjectGrantAmbiguityQuery(database, schema, name string) sqlclient.Query {
	return sqlclient.Select("function_name").From("svv_redshift_functions").Where("database_name = :database", sqlclient.Bind("database", database)).Where("schema_name = :schema", sqlclient.Bind("schema", schema)).Where("function_name = :ambiguous_name", sqlclient.Bind("ambiguous_name", name))
}

func sharedObjectGrantProcedureMembersQuery(database, schema string) sqlclient.Query {
	return sqlclient.Select("DISTINCT function_name AS object_name", "function_type").From("svv_redshift_functions").Where("database_name = :database", sqlclient.Bind("database", database)).Where("schema_name = :schema", sqlclient.Bind("schema", schema)).OrderBy("object_name", "function_type")
}

// A correlated self-read of the shared SVV_REDSHIFT_FUNCTIONS view fails with bound Data API parameters (error 8001).
// Classify one metadata result instead, rejecting mixed-kind names before reading or mutating their shared ACL.
func sharedObjectGrantProcedureMembers(rows []sqlclient.Row) ([]sqlclient.Row, error) {
	procedures, functions := map[string]bool{}, map[string]bool{}
	for _, row := range rows {
		name := row["object_name"]
		if strings.Contains(strings.ToUpper(row["function_type"]), "PROCEDURE") {
			procedures[name] = true
		} else {
			functions[name] = true
		}
	}
	var members []sqlclient.Row
	for _, name := range slices.Sorted(maps.Keys(procedures)) {
		if functions[name] {
			return nil, fmt.Errorf("shared routine %q has both function and procedure overloads with one name-wide ACL; use ALL FUNCTIONS or separate producer names instead of ALL PROCEDURES", name)
		}
		members = append(members, sqlclient.Row{"object_name": name})
	}
	return members, nil
}

func sharedObjectGrantFunctionQuery(database, schema, name string) sqlclient.Query {
	return sqlclient.Select("function_name").From("svv_redshift_functions").
		Where("database_name = :database", sqlclient.Bind("database", database)).
		Where("schema_name = :schema", sqlclient.Bind("schema", schema)).
		Where("function_name = :name", sqlclient.Bind("name", name))
}

func sharedObjectGrantMembersQuery(database, schema, scope string) sqlclient.Query {
	query := sqlclient.Select("DISTINCT table_name AS object_name").From("svv_all_tables")
	if scope == "FUNCTION" {
		// Consumer function ACLs cover names rather than overloads, and include shared producer procedures.
		query = sqlclient.Select("DISTINCT function_name AS object_name").From("svv_redshift_functions")
	}
	return query.Where("database_name = :database", sqlclient.Bind("database", database)).
		Where("schema_name = :schema", sqlclient.Bind("schema", schema)).OrderBy("object_name")
}

func sharedObjectGrantShow(database, schema, name, scope string) string {
	var keyword sqlclient.Keyword = "TABLE"
	if scope == "FUNCTION" {
		keyword = "FUNCTION"
	}
	return sqlclient.Stmt("SHOW GRANTS ON", keyword).Qualified(database, schema, name).String()
}
