package provider

import (
	"fmt"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// externalSchemaDefaultSource is the source type of configurations written before source_type existed, and the
// default of CREATE EXTERNAL SCHEMA itself.
const externalSchemaDefaultSource = "DATA_CATALOG"

// externalSchemaSource describes one CREATE EXTERNAL SCHEMA ... FROM form: its keyword, the
// SVV_EXTERNAL_SCHEMAS.eskind values it reads back as, the options it requires and accepts, and the options
// ALTER EXTERNAL SCHEMA can change in place.
type externalSchemaSource struct {
	// keyword follows FROM.
	keyword sqlclient.Keyword
	// kinds are the eskind values of this form.
	kinds []string
	// required options must be configured.
	required []string
	// optional options may be configured; every other source option is rejected.
	optional []string
	// alterable options change in place; the others replace the schema.
	alterable []string
}

// externalSchemaOptions lists every source-specific attribute, in the order validation reports them.
var externalSchemaOptions = []string{"glue_database", "source_database", "source_schema", "iam_role_arn", "region", "uri", "port", "secret_arn", "authentication", "authentication_arn"}

// externalSchemaSources holds the documented forms. ALTER EXTERNAL SCHEMA only alters DATA CATALOG, KAFKA and MSK
// schemas, and of a DATA CATALOG schema only the IAM role, since its other options belong to streaming sources.
var externalSchemaSources = map[string]externalSchemaSource{
	"DATA_CATALOG": {
		keyword: "DATA CATALOG", kinds: []string{"1"},
		required: []string{"glue_database", "iam_role_arn"}, optional: []string{"region"},
		alterable: []string{"iam_role_arn"},
	},
	"HIVE_METASTORE": {
		keyword: "HIVE METASTORE", kinds: []string{"2"},
		required: []string{"source_database", "uri", "iam_role_arn"}, optional: []string{"port"},
	},
	"POSTGRES": {
		keyword: "POSTGRES", kinds: []string{"3"},
		required: []string{"source_database", "uri", "iam_role_arn", "secret_arn"}, optional: []string{"source_schema", "port"},
	},
	"MYSQL": {
		keyword: "MYSQL", kinds: []string{"8"},
		required: []string{"source_database", "uri", "iam_role_arn", "secret_arn"}, optional: []string{"port"},
	},
	// A local database reads back as kind 4 and a datashare consumer database as kind 5.
	"REDSHIFT": {
		keyword: "REDSHIFT", kinds: []string{"4", "5"},
		required: []string{"source_database"}, optional: []string{"source_schema"},
	},
	"KINESIS": {
		keyword: "KINESIS", kinds: []string{"9"},
		required: []string{"iam_role_arn"}, optional: []string{"region"},
	},
	"MSK": {
		keyword: "MSK", kinds: []string{"10"},
		required: []string{"authentication", "uri"}, optional: []string{"iam_role_arn", "region", "authentication_arn", "secret_arn"},
		alterable: []string{"iam_role_arn", "uri", "authentication", "authentication_arn", "secret_arn"},
	},
}

// externalSchemaAuthentications are the AUTHENTICATION keywords of a streaming schema.
var externalSchemaAuthentications = []sqlclient.Keyword{"none", "iam", "mtls"}

// externalSchemaSourceName returns the configured source type, treating null as the DATA CATALOG default.
func externalSchemaSourceName(value types.String) string {
	if value.IsNull() {
		return externalSchemaDefaultSource
	}
	return value.ValueString()
}

// externalSchemaSourceOf returns the form of a known source type.
func externalSchemaSourceOf(data externalSchemaModel) (externalSchemaSource, error) {
	name := externalSchemaSourceName(data.SourceType)
	source, ok := externalSchemaSources[name]
	if !ok {
		return externalSchemaSource{}, fmt.Errorf("unsupported external schema source_type %q", name)
	}
	return source, nil
}

// externalSchemaOption returns one source-specific attribute by its schema name.
func externalSchemaOption(data externalSchemaModel, name string) attr.Value {
	return map[string]attr.Value{
		"glue_database": data.GlueDatabase, "source_database": data.SourceDatabase, "source_schema": data.SourceSchema,
		"iam_role_arn": data.IAMRoleARN, "region": data.Region, "uri": data.URI, "port": data.Port,
		"secret_arn": data.SecretARN, "authentication": data.Authentication, "authentication_arn": data.AuthenticationARN,
	}[name]
}

// validateExternalSchema checks the options against the source form. ValidateConfig passes unknownIsUnset=false
// and skips options whose value is not known yet; Create passes true, because by apply time every configured value
// is known and an unknown one is a computed option left unconfigured.
func validateExternalSchema(data externalSchemaModel, unknownIsUnset bool) error {
	if data.SourceType.IsUnknown() {
		return nil
	}
	name := externalSchemaSourceName(data.SourceType)
	source, err := externalSchemaSourceOf(data)
	if err != nil {
		return err
	}
	set := func(option string) (configured, known bool) {
		value := externalSchemaOption(data, option)
		if value.IsUnknown() {
			return !unknownIsUnset, unknownIsUnset
		}
		return !value.IsNull(), true
	}
	var problems []string
	for _, option := range externalSchemaOptions {
		configured, known := set(option)
		switch {
		case !known:
		case slices.Contains(source.required, option) && !configured:
			problems = append(problems, fmt.Sprintf("%s requires %s", name, option))
		case !slices.Contains(source.required, option) && !slices.Contains(source.optional, option) && configured:
			problems = append(problems, fmt.Sprintf("%s does not accept %s", name, option))
		}
	}
	if name == "MSK" && !data.Authentication.IsUnknown() {
		problems = append(problems, externalSchemaAuthenticationProblems(data, set)...)
	}
	if len(problems) > 0 {
		return fmt.Errorf("invalid %s external schema: %s", name, strings.Join(problems, "; "))
	}
	return nil
}

// externalSchemaAuthenticationProblems applies the streaming rules: IAM authentication needs a role, and mTLS
// needs exactly one certificate source, which the other modes do not take.
func externalSchemaAuthenticationProblems(data externalSchemaModel, set func(string) (bool, bool)) []string {
	var problems []string
	role, roleKnown := set("iam_role_arn")
	certificate, certificateKnown := set("authentication_arn")
	secret, secretKnown := set("secret_arn")
	switch data.Authentication.ValueString() {
	case "iam":
		if roleKnown && !role {
			problems = append(problems, "AUTHENTICATION iam requires iam_role_arn")
		}
		fallthrough
	case "none":
		if (certificateKnown && certificate) || (secretKnown && secret) {
			problems = append(problems, "authentication_arn and secret_arn apply only to AUTHENTICATION mtls")
		}
	case "mtls":
		if certificateKnown && secretKnown && certificate == secret {
			problems = append(problems, "AUTHENTICATION mtls requires exactly one of authentication_arn and secret_arn")
		}
	}
	return problems
}

// externalSchemaIAMRole appends IAM_ROLE unless no role is configured, which only streaming sources allow.
func externalSchemaIAMRole(statement sqlclient.Statement, data externalSchemaModel) sqlclient.Statement {
	return statement.OptLit("IAM_ROLE", knownString(data.IAMRoleARN))
}

// externalSchemaAuthentication appends AUTHENTICATION with the certificate source mTLS uses.
func externalSchemaAuthentication(statement sqlclient.Statement, data externalSchemaModel) (sqlclient.Statement, error) {
	authentication, err := optOneOf(data.Authentication, externalSchemaAuthentications...)
	if err != nil || authentication == "" {
		return statement, err
	}
	return statement.Kw("AUTHENTICATION", authentication).
		OptLit("AUTHENTICATION_ARN", knownString(data.AuthenticationARN)).OptLit("SECRET_ARN", knownString(data.SecretARN)), nil
}

// createExternalSchemaStatement renders CREATE EXTERNAL SCHEMA in the clause order of the AWS synopsis for each
// form: federated and Hive forms name the endpoint before IAM_ROLE, the DATA CATALOG form keeps REGION last, and
// MSK ends with its bootstrap URI. REGION is omitted until configured so Redshift uses the warehouse region.
func createExternalSchemaStatement(data externalSchemaModel) (string, error) {
	source, err := externalSchemaSourceOf(data)
	if err != nil {
		return "", err
	}
	if err := validateExternalSchema(data, true); err != nil {
		return "", err
	}
	kind := externalSchemaSourceName(data.SourceType)
	statement := sqlclient.Stmt("CREATE EXTERNAL SCHEMA").Ident(data.Name.ValueString()).Kw("FROM", source.keyword).
		OptLit("DATABASE", knownString(data.GlueDatabase)).OptLit("DATABASE", knownString(data.SourceDatabase)).
		OptLit("SCHEMA", knownString(data.SourceSchema))
	if kind != externalSchemaDefaultSource {
		statement = statement.OptLit("REGION", knownString(data.Region))
	}
	if kind != "MSK" {
		statement = statement.OptLit("URI", knownString(data.URI)).OptInt("PORT", knownInt64(data.Port))
	}
	statement = externalSchemaIAMRole(statement, data)
	if kind == externalSchemaDefaultSource {
		statement = statement.OptLit("REGION", knownString(data.Region))
	}
	if kind == "MSK" {
		if statement, err = externalSchemaAuthentication(statement, data); err != nil {
			return "", err
		}
		statement = statement.OptLit("URI", knownString(data.URI))
	} else {
		statement = statement.OptLit("SECRET_ARN", knownString(data.SecretARN))
	}
	return statement.String(), statement.Err()
}

// externalSchemaOwnerStatement renders ALTER SCHEMA ... OWNER TO; CREATE EXTERNAL SCHEMA has no AUTHORIZATION
// clause, so the AWS page directs ownership changes of external schemas to ALTER SCHEMA.
func externalSchemaOwnerStatement(data externalSchemaModel) string {
	return sqlclient.Stmt("ALTER SCHEMA").Ident(data.Name.ValueString()).KwIdent("OWNER TO", data.Owner.ValueString()).String()
}

// externalSchemaAlter starts every ALTER EXTERNAL SCHEMA statement for the schema.
func externalSchemaAlter(data externalSchemaModel) sqlclient.Statement {
	return sqlclient.Stmt("ALTER EXTERNAL SCHEMA").Ident(data.Name.ValueString())
}

// externalSchemaAuthenticationAttributes change together: mTLS needs its certificate source in the same
// statement, so the first changed one renders the whole clause and the later ones render nothing.
var externalSchemaAuthenticationAttributes = []string{"authentication", "authentication_arn", "secret_arn"}

// externalSchemaAuthenticationStep renders the combined AUTHENTICATION clause once per update.
func externalSchemaAuthenticationStep(attribute string) alterStep[externalSchemaModel] {
	return alterStep[externalSchemaModel]{
		attribute: attribute,
		value:     func(data externalSchemaModel) attr.Value { return externalSchemaOption(data, attribute) },
		render: func(prev, plan externalSchemaModel) []string {
			for _, earlier := range externalSchemaAuthenticationAttributes {
				if earlier == attribute {
					break
				}
				if before, after := externalSchemaOption(prev, earlier), externalSchemaOption(plan, earlier); !after.IsUnknown() && !before.Equal(after) {
					return nil
				}
			}
			// alterExternalSchemaStatements checked the keyword, so the error is always nil.
			statement, _ := externalSchemaAuthentication(externalSchemaAlter(plan), plan)
			return []string{statement.String()}
		},
	}
}

// externalSchemaAlterSteps change the owner of any external schema and the options ALTER EXTERNAL SCHEMA supports.
var externalSchemaAlterSteps = []alterStep[externalSchemaModel]{
	{
		attribute: "owner",
		value:     func(data externalSchemaModel) attr.Value { return data.Owner },
		render: func(_, plan externalSchemaModel) []string {
			return []string{externalSchemaOwnerStatement(plan)}
		},
	},
	{
		attribute: "iam_role_arn",
		value:     func(data externalSchemaModel) attr.Value { return data.IAMRoleARN },
		render: func(_, plan externalSchemaModel) []string {
			return []string{externalSchemaAlter(plan).KwLit("IAM_ROLE", plan.IAMRoleARN.ValueString()).String()}
		},
	},
	{
		attribute: "uri",
		value:     func(data externalSchemaModel) attr.Value { return data.URI },
		render: func(_, plan externalSchemaModel) []string {
			return []string{externalSchemaAlter(plan).KwLit("URI", plan.URI.ValueString()).String()}
		},
	},
	externalSchemaAuthenticationStep("authentication"),
	externalSchemaAuthenticationStep("authentication_arn"),
	externalSchemaAuthenticationStep("secret_arn"),
}

// alterExternalSchemaStatements renders the in-place changes from prev to plan. The plan modifiers replace the
// schema for every other change, so a change the form cannot alter is an error rather than a silent no-op.
func alterExternalSchemaStatements(prev, plan externalSchemaModel) ([]string, error) {
	source, err := externalSchemaSourceOf(plan)
	if err != nil {
		return nil, err
	}
	if _, err := optOneOf(plan.Authentication, externalSchemaAuthentications...); err != nil {
		return nil, err
	}
	var steps []alterStep[externalSchemaModel]
	for _, step := range externalSchemaAlterSteps {
		before, after := step.value(prev), step.value(plan)
		// Only the certificate sources can be dropped, by switching to the other one or leaving mTLS.
		removable := step.attribute == "secret_arn" || step.attribute == "authentication_arn"
		switch {
		case step.attribute == "owner":
			if !after.IsNull() {
				steps = append(steps, step)
			}
		case after.IsUnknown() || before.Equal(after):
		case !slices.Contains(source.alterable, step.attribute), after.IsNull() && !removable:
			return nil, fmt.Errorf("%s cannot change %s in place; the schema must be replaced", externalSchemaSourceName(plan.SourceType), step.attribute)
		default:
			steps = append(steps, step)
		}
	}
	return alterStatements(prev, plan, steps), nil
}

// dropExternalSchemaStatement renders DROP SCHEMA without DROP EXTERNAL DATABASE or CASCADE, so only the
// Redshift mapping is removed and the external database survives.
func dropExternalSchemaStatement(data externalSchemaModel) string {
	return sqlclient.Stmt("DROP SCHEMA").Ident(data.Name.ValueString()).String()
}

// readExternalSchemaQuery selects the namespace and owner of one schema with the kind, source database, and options
// of its external mapping. Existence comes from PG_NAMESPACE, which every user can read, because
// SVV_EXTERNAL_SCHEMAS shows a regular user only their own schemas: after a non-superuser hands the schema to
// another owner, its SVV columns are null while the schema still exists.
func readExternalSchemaQuery(data externalSchemaModel) sqlclient.Query {
	return sqlclient.Select("n.nspname AS schemaname", "s.eskind", "s.databasename", "s.esoptions", "u.usename AS owner").
		From("pg_namespace n LEFT JOIN svv_external_schemas s ON s.esoid = n.oid LEFT JOIN pg_user u ON u.usesysid = n.nspowner").
		Where("n.nspname = :name", sqlclient.Bind("name", data.Name.ValueString()))
}
