package provider

import "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"

// createSchemaStatement renders CREATE SCHEMA; the executing user becomes the owner, which read reports.
func createSchemaStatement(data schemaModel) string {
	return sqlclient.Stmt("CREATE SCHEMA").Ident(data.Name.ValueString()).String()
}

// dropSchemaStatement renders DROP SCHEMA without CASCADE, so a schema that still holds objects is kept.
func dropSchemaStatement(data schemaModel) string {
	return sqlclient.Stmt("DROP SCHEMA").Ident(data.Name.ValueString()).String()
}

// readSchemaQuery reads the schema with its owner's name, which read requires to be present.
func readSchemaQuery(data schemaModel) sqlclient.Query {
	return sqlclient.Select("n.nspname AS schema_name", "u.usename AS owner").
		From("pg_namespace n JOIN pg_user u ON n.nspowner = u.usesysid").
		Where("n.nspname = :name", sqlclient.Bind("name", data.Name.ValueString()))
}
