package provider

import (
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// createExternalSchemaStatement renders CREATE EXTERNAL SCHEMA for a Glue Data Catalog database. REGION is
// omitted until configured so Redshift uses the warehouse region.
func createExternalSchemaStatement(data externalSchemaModel) (string, error) {
	statement := sqlclient.Stmt("CREATE EXTERNAL SCHEMA").Ident(data.Name.ValueString()).
		Kw("FROM DATA CATALOG").KwLit("DATABASE", data.GlueDatabase.ValueString()).
		KwLit("IAM_ROLE", data.IAMRoleARN.ValueString()).
		OptLit("REGION", knownString(data.Region))
	return statement.String(), statement.Err()
}

// dropExternalSchemaStatement renders DROP SCHEMA without DROP EXTERNAL DATABASE or CASCADE, so only the
// Redshift mapping is removed and the Glue database survives.
func dropExternalSchemaStatement(data externalSchemaModel) string {
	return sqlclient.Stmt("DROP SCHEMA").Ident(data.Name.ValueString()).String()
}

// readExternalSchemaQuery selects the kind, Glue database and options of one external schema.
func readExternalSchemaQuery(data externalSchemaModel) sqlclient.Query {
	return sqlclient.Select("schemaname", "eskind", "databasename", "esoptions").From("svv_external_schemas").
		Where("schemaname = :name", sqlclient.Bind("name", data.Name.ValueString()))
}
