package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
)

// externalSchemaFixture returns a schema of the given source type with every option null, so cases set only
// what their form uses.
func externalSchemaFixture(sourceType, name string) externalSchemaModel {
	return externalSchemaModel{
		Database: types.StringValue("warehouse"), Name: types.StringValue(name), SourceType: types.StringValue(sourceType),
		GlueDatabase: types.StringNull(), SourceDatabase: types.StringNull(), SourceSchema: types.StringNull(),
		IAMRoleARN: types.StringNull(), Region: types.StringNull(), URI: types.StringNull(), Port: types.Int64Null(),
		SecretARN: types.StringNull(), Authentication: types.StringNull(), AuthenticationARN: types.StringNull(),
		Owner: types.StringNull(), RefreshRevision: types.StringNull(),
	}
}

// externalSchemaMSK returns a streaming schema with the given authentication.
func externalSchemaMSK(authentication string) externalSchemaModel {
	data := externalSchemaFixture("MSK", "stream")
	data.Authentication, data.URI = types.StringValue(authentication), types.StringValue("b-1.example.kafka.eu-central-1.amazonaws.com:9098")
	data.IAMRoleARN = types.StringValue("arn:aws:iam::123456789012:role/msk")
	return data
}

// TestExternalSchemaSQL pins every external schema statement, including quoting of names and catalog options.
func TestExternalSchemaSQL(t *testing.T) {
	glue := func(name, database string, region types.String) externalSchemaModel {
		data := externalSchemaFixture("DATA_CATALOG", name)
		data.GlueDatabase, data.IAMRoleARN, data.Region = types.StringValue(database), types.StringValue("arn:aws:iam::123456789012:role/spectrum"), region
		return data
	}
	federated := func(sourceType string) externalSchemaModel {
		data := externalSchemaFixture(sourceType, "federated")
		data.SourceDatabase, data.URI = types.StringValue(`it's \db`), types.StringValue("aurora.example.internal")
		data.IAMRoleARN = types.StringValue("arn:aws:iam::123456789012:role/federated")
		data.SecretARN = types.StringValue("arn:aws:secretsmanager:eu-central-1:123456789012:secret:aurora-AbCdEf")
		return data
	}
	create := func(data externalSchemaModel) func() (string, error) {
		return func() (string, error) { return createExternalSchemaStatement(data) }
	}
	alter := func(prev, plan externalSchemaModel) func() ([]string, error) {
		return func() ([]string, error) { return alterExternalSchemaStatements(prev, plan) }
	}
	plain := glue("example_external", "example_glue", types.StringNull())
	quoted := glue(`Lake"Schema`, `it's \glue`, types.StringValue(`eu-'central\1`))
	legacy := plain
	legacy.SourceType = types.StringNull()
	postgres := federated("POSTGRES")
	postgres.SourceSchema, postgres.Port = types.StringValue(`Odd"schema's`), types.Int64Value(5432)
	mysql := federated("MYSQL")
	mysql.Port = types.Int64Value(3306)
	mysqlSchema := mysql
	mysqlSchema.SourceSchema = types.StringValue("public")
	postgresMissing := federated("POSTGRES")
	postgresMissing.SecretARN, postgresMissing.URI = types.StringNull(), types.StringUnknown()
	hive := externalSchemaFixture("HIVE_METASTORE", "hive")
	hive.SourceDatabase, hive.URI, hive.Port = types.StringValue("hive_db"), types.StringValue("172.10.10.10"), types.Int64Value(99)
	hive.IAMRoleARN = types.StringValue("arn:aws:iam::123456789012:role/hive")
	redshift := externalSchemaFixture("REDSHIFT", `Shared"Sales`)
	redshift.SourceDatabase, redshift.SourceSchema = types.StringValue(`sales's\db`), types.StringValue("public")
	redshiftRole := redshift
	redshiftRole.IAMRoleARN = types.StringValue("arn:aws:iam::123456789012:role/unused")
	kinesis := externalSchemaFixture("KINESIS", "kds")
	kinesis.IAMRoleARN, kinesis.Region = types.StringValue("arn:aws:iam::123456789012:role/kinesis"), types.StringValue("us-west-2")
	mskNone := externalSchemaMSK("none")
	mskNone.IAMRoleARN = types.StringNull()
	mskIAM := externalSchemaMSK("iam")
	mskIAMWithoutRole := externalSchemaMSK("iam")
	mskIAMWithoutRole.IAMRoleARN = types.StringNull()
	mskCertificate := externalSchemaMSK("mtls")
	mskCertificate.AuthenticationARN = types.StringValue("arn:aws:acm:eu-central-1:123456789012:certificate/it's")
	mskSecret := externalSchemaMSK("mtls")
	mskSecret.SecretARN = types.StringValue(`arn:aws:secretsmanager:eu-central-1:123456789012:secret:mtls\cert`)
	mskBoth := mskCertificate
	mskBoth.SecretARN = mskSecret.SecretARN
	mskNoneWithCertificate := mskNone
	mskNoneWithCertificate.AuthenticationARN = mskCertificate.AuthenticationARN
	movedURI := mskIAM
	movedURI.URI = types.StringValue("b-2.example.kafka.eu-central-1.amazonaws.com:9098")
	rotatedRole := plain
	rotatedRole.IAMRoleARN = types.StringValue("arn:aws:iam::123456789012:role/rotated")
	owned := plain
	owned.Owner = types.StringValue(`Etl"Owner`)
	postgresRotated := postgres
	postgresRotated.SecretARN = types.StringValue("arn:aws:secretsmanager:eu-central-1:123456789012:secret:rotated")
	mskDroppedRole := mskSecret
	mskDroppedRole.IAMRoleARN = types.StringNull()
	unsupported := plain
	unsupported.SourceType = types.StringValue("KAFKA")
	checkSQL(t, "external_schema", []sqlCase{
		{"create", create(plain)},
		{"create_default_source", create(legacy)},
		{"create_region", create(glue("example_external", "example_glue", types.StringValue("eu-central-1")))},
		{"create_unknown_region", create(glue("example_external", "example_glue", types.StringUnknown()))},
		{"create_quoted", create(quoted)},
		{"create_glue_without_role", create(func() externalSchemaModel { data := plain; data.IAMRoleARN = types.StringNull(); return data }())},
		{"create_glue_with_uri", create(func() externalSchemaModel { data := plain; data.URI = types.StringValue("host"); return data }())},
		{"create_hive", create(hive)},
		{"create_postgres", create(postgres)},
		{"create_postgres_missing_options", create(postgresMissing)},
		{"create_mysql", create(mysql)},
		{"create_mysql_with_schema", create(mysqlSchema)},
		{"create_redshift", create(redshift)},
		{"create_redshift_with_role", create(redshiftRole)},
		{"create_kinesis", create(kinesis)},
		{"create_msk_none", create(mskNone)},
		{"create_msk_iam", create(mskIAM)},
		{"create_msk_iam_without_role", create(mskIAMWithoutRole)},
		{"create_msk_mtls_certificate", create(mskCertificate)},
		{"create_msk_mtls_secret", create(mskSecret)},
		{"create_msk_mtls_both", create(mskBoth)},
		{"create_msk_none_with_certificate", create(mskNoneWithCertificate)},
		{"create_unsupported_source", create(unsupported)},
		{"create_msk_invalid_authentication", create(externalSchemaMSK("TLS"))},
		{"alter_unsupported_source", alter(plain, unsupported)},
		{"alter_msk_invalid_authentication", alter(mskIAM, externalSchemaMSK("TLS"))},
		{"owner", func() string { return externalSchemaOwnerStatement(owned) }},
		{"alter_unchanged", alter(plain, plain)},
		{"alter_owner", alter(plain, owned)},
		{"alter_glue_role", alter(plain, rotatedRole)},
		{"alter_msk_uri", alter(mskIAM, movedURI)},
		{"alter_msk_to_certificate", alter(mskIAM, mskCertificate)},
		{"alter_msk_certificate_to_secret", alter(mskCertificate, mskSecret)},
		{"alter_msk_to_none", alter(mskSecret, externalSchemaMSK("none"))},
		{"alter_msk_drop_role", alter(mskSecret, mskDroppedRole)},
		{"alter_postgres_secret", alter(postgres, postgresRotated)},
		{"drop", func() string { return dropExternalSchemaStatement(plain) }},
		{"drop_quoted", func() string { return dropExternalSchemaStatement(quoted) }},
		{"read", func() (string, error) {
			sql, parameters, err := readExternalSchemaQuery(quoted).Build()
			assert.Equal(t, map[string]string{"name": `Lake"Schema`}, parameters)
			return sql, err
		}},
	})
}
