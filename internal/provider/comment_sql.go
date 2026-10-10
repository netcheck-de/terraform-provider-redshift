package provider

import (
	"fmt"

	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// commentKinds maps each supported object_type to its COMMENT ON keyword. Lookup is case-sensitive, matching the
// schema validator, so an unsupported spelling fails instead of being normalized.
var commentKinds = map[string]sqlclient.Keyword{"DATABASE": "DATABASE", "SCHEMA": "SCHEMA", "TABLE": "TABLE", "VIEW": "VIEW", "COLUMN": "COLUMN", "CONSTRAINT": "CONSTRAINT"}

// Annotation sources per target kind. The LEFT JOIN keeps a row for an existing object without a comment, so read
// can tell a missing object from an empty annotation.
const (
	// commentText reports an absent annotation as the empty string that clears it.
	commentText sqlclient.Keyword = "COALESCE(d.description, '') AS text"
	// commentDatabaseSource joins database comments.
	commentDatabaseSource sqlclient.Keyword = "pg_database o LEFT JOIN pg_description d ON d.objoid = o.oid AND d.classoid = 'pg_database'::regclass AND d.objsubid = 0"
	// commentSchemaSource joins schema comments.
	commentSchemaSource sqlclient.Keyword = "pg_namespace o LEFT JOIN pg_description d ON d.objoid = o.oid AND d.classoid = 'pg_namespace'::regclass AND d.objsubid = 0"
	// commentRelationSource joins table and view comments, which pg_description stores with objsubid 0.
	commentRelationSource sqlclient.Keyword = "pg_class o JOIN pg_namespace n ON n.oid = o.relnamespace LEFT JOIN pg_description d ON d.objoid = o.oid AND d.classoid = 'pg_class'::regclass AND d.objsubid = 0"
	// commentColumnSource joins column comments, which pg_description stores under the column's attnum.
	commentColumnSource sqlclient.Keyword = "pg_class o JOIN pg_namespace n ON n.oid = o.relnamespace JOIN pg_attribute a ON a.attrelid = o.oid AND a.attname = :column AND a.attnum > 0 AND NOT a.attisdropped LEFT JOIN pg_description d ON d.objoid = o.oid AND d.classoid = 'pg_class'::regclass AND d.objsubid = a.attnum"
	// commentConstraintSource joins constraint comments, which pg_description stores against the pg_constraint row
	// rather than its table, so the constraint name alone does not identify them.
	commentConstraintSource sqlclient.Keyword = "pg_constraint k JOIN pg_class o ON o.oid = k.conrelid JOIN pg_namespace n ON n.oid = o.relnamespace LEFT JOIN pg_description d ON d.objoid = k.oid AND d.classoid = 'pg_constraint'::regclass AND d.objsubid = 0"
)

// commentTarget validates the target and renders its COMMENT ON prefix together with the catalog query reading its
// annotation, so both always address the same object.
func commentTarget(data commentModel) (sqlclient.Statement, sqlclient.Query, error) {
	kind, name, schemaName, column := data.ObjectType.ValueString(), data.ObjectName.ValueString(), data.SchemaName.ValueString(), data.ColumnName.ValueString()
	constraint := data.ConstraintName.ValueString()
	keyword, supported := commentKinds[kind]
	if !supported {
		return sqlclient.Statement{}, sqlclient.Query{}, fmt.Errorf("unsupported comment object_type %q", kind)
	}
	statement, query := sqlclient.Stmt("COMMENT ON").Kw(keyword), sqlclient.Select(commentText)
	nameBinding := sqlclient.Bind("name", name)
	switch kind {
	case "DATABASE", "SCHEMA":
		if schemaName != "" || column != "" {
			return sqlclient.Statement{}, sqlclient.Query{}, fmt.Errorf("%s does not accept schema_name or column_name", kind)
		}
		if constraint != "" {
			return sqlclient.Statement{}, sqlclient.Query{}, fmt.Errorf("constraint_name is required only for CONSTRAINT")
		}
		if kind == "SCHEMA" {
			return statement.Ident(name), query.From(commentSchemaSource).Where("o.nspname = :name", nameBinding), nil
		}
		if name != data.DatabaseName.ValueString() {
			return sqlclient.Statement{}, sqlclient.Query{}, fmt.Errorf("DATABASE object_name must equal database_name")
		}
		return statement.Ident(name), query.From(commentDatabaseSource).Where("o.datname = :name", nameBinding), nil
	}
	if schemaName == "" {
		return sqlclient.Statement{}, sqlclient.Query{}, fmt.Errorf("schema_name is required for %s", kind)
	}
	if (kind == "COLUMN") != (column != "") {
		return sqlclient.Statement{}, sqlclient.Query{}, fmt.Errorf("column_name is required only for COLUMN")
	}
	if (kind == "CONSTRAINT") != (constraint != "") {
		return sqlclient.Statement{}, sqlclient.Query{}, fmt.Errorf("constraint_name is required only for CONSTRAINT")
	}
	if name == "" {
		// Qualified omits empty qualifiers, which would turn schema.""."column" into a different column.
		return sqlclient.Statement{}, sqlclient.Query{}, fmt.Errorf("object_name is required for %s", kind)
	}
	schemaBinding := sqlclient.Bind("schema", schemaName)
	if kind == "CONSTRAINT" {
		// Constraint names are unique only per table, so both the statement and the read name the table.
		statement = statement.Ident(constraint).Kw("ON").Qualified(schemaName, name)
		query = query.From(commentConstraintSource).
			Where("k.conname = :constraint", sqlclient.Bind("constraint", constraint)).
			Where("o.relname = :name", nameBinding).Where("n.nspname = :schema", schemaBinding)
		return statement, query, nil
	}
	if kind == "COLUMN" {
		statement = statement.Qualified(schemaName, name, column)
		query = query.From(commentColumnSource, sqlclient.Bind("column", column))
	} else {
		statement = statement.Qualified(schemaName, name)
		query = query.From(commentRelationSource)
	}
	query = query.Where("o.relname = :name", nameBinding).Where("n.nspname = :schema", schemaBinding).
		WhereIf(kind == "VIEW", "o.relkind = 'v'").
		WhereIf(kind == "TABLE", "o.relkind IN ('r', 'm')")
	return statement, query, nil
}

// commentStatement sets text on the target. Empty text renders IS NULL, because Redshift removes an annotation
// only that way.
func commentStatement(data commentModel, text string) (string, error) {
	target, _, err := commentTarget(data)
	if err != nil {
		return "", err
	}
	if text == "" {
		return target.Kw("IS NULL").String(), nil
	}
	return target.KwLit("IS", text).String(), nil
}

// readCommentQuery reads the target's annotation in the target database.
func readCommentQuery(data commentModel) (sqlclient.Query, error) {
	_, query, err := commentTarget(data)
	return query, err
}

// commentDatabaseQuery checks in the administration database that the target database is local, because the
// annotation query must run inside it.
func commentDatabaseQuery(data commentModel) sqlclient.Query {
	return sqlclient.Select("database_name").
		From("svv_redshift_databases").
		Where("database_name = :database", sqlclient.Bind("database", data.DatabaseName.ValueString())).
		Where("database_type = 'local'")
}
