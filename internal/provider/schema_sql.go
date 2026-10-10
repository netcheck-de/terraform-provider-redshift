package provider

import (
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// schemaUnlimited is the quota value that renders QUOTA UNLIMITED, the default of a schema created without one.
const schemaUnlimited = -1

// schemaAlter starts every ALTER SCHEMA statement for the schema.
func schemaAlter(data schemaModel) sqlclient.Statement {
	return sqlclient.Stmt("ALTER SCHEMA").Ident(data.Name.ValueString())
}

// schemaQuota appends QUOTA in megabytes, the unit Redshift converts every quota to and reports, so a configured
// value and its readback never differ by unit.
func schemaQuota(statement sqlclient.Statement, quota int64) (sqlclient.Statement, error) {
	switch {
	case quota == schemaUnlimited:
		return statement.Kw("QUOTA UNLIMITED"), nil
	case quota < 1:
		return statement, fmt.Errorf("quota must be -1 (UNLIMITED) or at least 1 MB, got %d", quota)
	default:
		return statement.KwInt("QUOTA", quota).Kw("MB"), nil
	}
}

// createSchemaStatement renders CREATE SCHEMA with the optional AUTHORIZATION owner and QUOTA; without an owner
// the executing user owns the schema, and without a quota it is unlimited.
func createSchemaStatement(data schemaModel) (string, error) {
	statement := sqlclient.Stmt("CREATE SCHEMA").Ident(data.Name.ValueString()).OptIdent("AUTHORIZATION", knownString(data.Owner))
	if quota := knownInt64(data.Quota); quota != nil {
		var err error
		if statement, err = schemaQuota(statement, *quota); err != nil {
			return "", err
		}
	}
	return statement.String(), statement.Err()
}

// schemaAlterSteps change one option per statement. A rename is not offered because it replaces the schema.
var schemaAlterSteps = []alterStep[schemaModel]{
	{
		attribute: "owner",
		value:     func(data schemaModel) attr.Value { return data.Owner },
		render: func(_, plan schemaModel) []string {
			return []string{schemaAlter(plan).KwIdent("OWNER TO", plan.Owner.ValueString()).String()}
		},
	},
	{
		attribute: "quota",
		value:     func(data schemaModel) attr.Value { return data.Quota },
		render: func(_, plan schemaModel) []string {
			// alterSchemaStatements checked the quota, so the error is always nil.
			statement, _ := schemaQuota(schemaAlter(plan), plan.Quota.ValueInt64())
			return []string{statement.String()}
		},
	},
}

// alterSchemaStatements renders the in-place changes from prev to plan. A null plan value comes only from state
// written before the option existed and means the option is not managed, so it never renders a reset.
func alterSchemaStatements(prev, plan schemaModel) ([]string, error) {
	if quota := knownInt64(plan.Quota); quota != nil {
		if _, err := schemaQuota(sqlclient.Fragment(), *quota); err != nil {
			return nil, err
		}
	}
	var managed []alterStep[schemaModel]
	for _, step := range schemaAlterSteps {
		if !step.value(plan).IsNull() {
			managed = append(managed, step)
		}
	}
	return alterStatements(prev, plan, managed), nil
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

// readSchemaSessionQuery reads the connected user and whether it is a superuser, which decides whether
// SVV_REDSHIFT_SCHEMA_QUOTA shows the quota of a schema the user does not own.
func readSchemaSessionQuery() sqlclient.Query {
	return sqlclient.Select("usename AS name", "usesuper AS superuser").From("pg_user").Where("usename = current_user")
}

// readSchemaQuotaQuery reads the schema's quota in megabytes. SVV_REDSHIFT_SCHEMA_QUOTA serves both provisioned
// clusters and Serverless, unlike SVV_SCHEMA_QUOTA_STATE; its names are CHAR(128), hence the TRIM.
func readSchemaQuotaQuery(data schemaModel) sqlclient.Query {
	return sqlclient.Select("quota").From("svv_redshift_schema_quota").
		Where("TRIM(database_name) = :database", sqlclient.Bind("database", data.Database.ValueString())).
		Where("TRIM(schema_name) = :name", sqlclient.Bind("name", data.Name.ValueString()))
}
