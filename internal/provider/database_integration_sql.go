package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// databaseIntegrationSetting renders one zero-ETL option, which CREATE DATABASE ... FROM INTEGRATION and ALTER
// DATABASE ... INTEGRATION SET spell the same way.
type databaseIntegrationSetting struct {
	// attribute names the block field, for diagnostics and tests.
	attribute string
	// value extracts the field; null and unknown values are not rendered.
	value func(databaseIntegrationModel) attr.Value
	// render appends the option with the field's value.
	render func(sqlclient.Statement, databaseIntegrationModel) sqlclient.Statement
}

// databaseIntegrationToggle renders KEYWORD = TRUE or KEYWORD = FALSE.
func databaseIntegrationToggle(value func(databaseIntegrationModel) types.Bool, ifTrue, ifFalse sqlclient.Keyword) func(sqlclient.Statement, databaseIntegrationModel) sqlclient.Statement {
	return func(statement sqlclient.Statement, integration databaseIntegrationModel) sqlclient.Statement {
		return statement.Toggle(value(integration).ValueBool(), ifTrue, ifFalse)
	}
}

// databaseIntegrationSettings lists the options in the order of the CREATE DATABASE syntax.
var databaseIntegrationSettings = []databaseIntegrationSetting{
	{
		attribute: "accept_invalid_chars",
		value:     func(m databaseIntegrationModel) attr.Value { return m.AcceptInvalidChars },
		render: databaseIntegrationToggle(func(m databaseIntegrationModel) types.Bool { return m.AcceptInvalidChars },
			"ACCEPTINVCHARS = TRUE", "ACCEPTINVCHARS = FALSE"),
	},
	{
		attribute: "query_all_states",
		value:     func(m databaseIntegrationModel) attr.Value { return m.QueryAllStates },
		render: databaseIntegrationToggle(func(m databaseIntegrationModel) types.Bool { return m.QueryAllStates },
			"QUERY_ALL_STATES = TRUE", "QUERY_ALL_STATES = FALSE"),
	},
	{
		attribute: "refresh_interval",
		value:     func(m databaseIntegrationModel) attr.Value { return m.RefreshInterval },
		render: func(statement sqlclient.Statement, m databaseIntegrationModel) sqlclient.Statement {
			return statement.KwInt("REFRESH_INTERVAL", m.RefreshInterval.ValueInt64())
		},
	},
	{
		attribute: "truncate_columns",
		value:     func(m databaseIntegrationModel) attr.Value { return m.TruncateColumns },
		render: databaseIntegrationToggle(func(m databaseIntegrationModel) types.Bool { return m.TruncateColumns },
			"TRUNCATECOLUMNS = TRUE", "TRUNCATECOLUMNS = FALSE"),
	},
	{
		attribute: "history_mode",
		value:     func(m databaseIntegrationModel) attr.Value { return m.HistoryMode },
		render: databaseIntegrationToggle(func(m databaseIntegrationModel) types.Bool { return m.HistoryMode },
			"HISTORY_MODE = TRUE", "HISTORY_MODE = FALSE"),
	},
	{
		attribute: "auto_remediation",
		value:     func(m databaseIntegrationModel) attr.Value { return m.AutoRemediation },
		render: databaseIntegrationToggle(func(m databaseIntegrationModel) types.Bool { return m.AutoRemediation },
			"AUTO_REMEDIATION = TRUE", "AUTO_REMEDIATION = FALSE"),
	},
}

// databaseIntegrationSource renders FROM INTEGRATION with the optional source database and the configured
// options after SET. The syntax shows the source database as a string literal, but Redshift rejects one and accepts
// the identifier the AWS example passes; quoting keeps the source's letter case.
func databaseIntegrationSource(integration databaseIntegrationModel) sqlclient.Statement {
	clause := sqlclient.Fragment().KwLit("FROM INTEGRATION", integration.IntegrationID.ValueString()).
		OptIdent("DATABASE", knownString(integration.SourceDatabase))
	settings := sqlclient.Fragment()

	for _, setting := range databaseIntegrationSettings {
		if value := setting.value(integration); !value.IsNull() && !value.IsUnknown() {
			settings = setting.render(settings, integration)
		}
	}

	return clause.When(settings.String() != "", func(s sqlclient.Statement) sqlclient.Statement { return s.Kw("SET").Append(settings) })
}

// alterDatabaseIntegrationStatements renders one ALTER DATABASE ... INTEGRATION SET per changed option. A null
// plan value is not managed and an unknown one waits for the catalog, so neither renders.
func alterDatabaseIntegrationStatements(prev, plan databaseModel) []string {
	if plan.Integration == nil {
		return nil
	}

	var prior databaseIntegrationModel
	if prev.Integration != nil {
		prior = *prev.Integration
	}

	var statements []string

	for _, setting := range databaseIntegrationSettings {
		after := setting.value(*plan.Integration)
		if after.IsNull() || after.IsUnknown() || after.Equal(setting.value(prior)) {
			continue
		}

		statement := setting.render(databaseAlter(plan).Kw("INTEGRATION SET"), *plan.Integration)
		// Without FOR ALL TABLES, Redshift changes only the default for newly replicated tables, leaving existing
		// tables in their old history mode even when SVV_INTEGRATION already reports the new database default.
		if setting.attribute == "history_mode" {
			statement = statement.Kw("FOR ALL TABLES")
		}

		statements = append(statements, statement.String())
	}

	return statements
}

// readDatabaseIntegrationQuery reads the integration a destination database receives. SVV_INTEGRATION pads its
// CHAR(128) columns, so the target is compared trimmed.
func readDatabaseIntegrationQuery(name string) sqlclient.Query {
	return sqlclient.Select("TRIM(integration_id) AS integration_id", "TRIM(source_database) AS source_database",
		"refresh_interval", "is_history_mode", "auto_remediation").
		From("svv_integration").
		Where("TRIM(target_database) = :name", sqlclient.Bind("name", name))
}
