package provider

import "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"

// createDatashareSchemaStatements adds the schema and, when requested, includes its future objects.
// include_new starts out false, so only enabling it needs a second statement.
func createDatashareSchemaStatements(data datashareSchemaModel) []string {
	share := sqlclient.Stmt("ALTER DATASHARE").Ident(data.Datashare.ValueString())
	statements := []string{share.KwIdent("ADD SCHEMA", data.Schema.ValueString()).String()}
	if data.IncludeNew.ValueBool() {
		statements = append(statements, share.KwIdent("SET INCLUDENEW TRUE FOR SCHEMA", data.Schema.ValueString()).String())
	}
	return statements
}

// alterDatashareSchemaStatement sets include_new, the only in-place setting. Update renders it even when the
// plan matches state, so an update also repairs drift that the preceding refresh did not observe.
func alterDatashareSchemaStatement(data datashareSchemaModel) string {
	return sqlclient.Stmt("ALTER DATASHARE").Ident(data.Datashare.ValueString()).
		Kw("SET INCLUDENEW").Bool(data.IncludeNew.ValueBool()).
		KwIdent("FOR SCHEMA", data.Schema.ValueString()).String()
}

// dropDatashareSchemaStatement removes the schema from the share without touching the source schema.
func dropDatashareSchemaStatement(data datashareSchemaModel) string {
	return sqlclient.Stmt("ALTER DATASHARE").Ident(data.Datashare.ValueString()).KwIdent("REMOVE SCHEMA", data.Schema.ValueString()).String()
}

// readDatashareSchemaQuery reads the schema member under either object_type spelling the catalog uses for schemas.
func readDatashareSchemaQuery(data datashareSchemaModel) sqlclient.Query {
	return sqlclient.Select("object_name", "include_new").
		From("svv_datashare_objects").
		Where("share_type = 'OUTBOUND'").
		Where("share_name = :share", sqlclient.Bind("share", data.Datashare.ValueString())).
		Where("object_name = :schema", sqlclient.Bind("schema", data.Schema.ValueString())).
		Where("object_type IN ('schema', 'schemas')")
}
