package provider

import (
	"fmt"
	"slices"
	"strings"

	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// External model request and response formats; the first of each is the Redshift default.
// https://docs.aws.amazon.com/redshift/latest/dg/r_create_external_model.html
var (
	externalModelRequestTypes  = []sqlclient.Keyword{"UNIFIED", "RAW"}
	externalModelResponseTypes = []sqlclient.Keyword{"VARCHAR", "SUPER"}
)

// externalModelTarget renders the schema-qualified model name. Empty parts are rejected because Qualified omits an
// empty schema, which would resolve the model through the search path instead.
func externalModelTarget(schema, name string) (sqlclient.Statement, error) {
	if schema == "" || name == "" {
		return sqlclient.Statement{}, fmt.Errorf("a model requires a nonempty schema and name")
	}
	return sqlclient.Fragment().Qualified(schema, name), nil
}

// externalModelIAMRole renders IAM_ROLE DEFAULT, accepted in any case, or one role ARN as a literal; unlike Lambda
// UDFs, CREATE EXTERNAL MODEL documents a single role, not a chain.
func externalModelIAMRole(role string) (sqlclient.Statement, error) {
	if strings.EqualFold(role, "DEFAULT") {
		return sqlclient.Kw("DEFAULT"), nil
	}
	if !externalFunctionRolePattern.MatchString(role) {
		return sqlclient.Statement{}, fmt.Errorf("iam_role must be DEFAULT or one IAM role ARN, got %q", role)
	}
	return sqlclient.Lit(role), nil
}

// externalModelFormats returns the request and response formats, applying the Redshift defaults to unset values,
// and enforces the documented combinations: RAW always answers with SUPER, and the static prompt prefix and suffix
// only exist for UNIFIED requests.
func externalModelFormats(data externalModelModel) (request, response sqlclient.Keyword, err error) {
	if request, err = optOneOf(data.RequestType, externalModelRequestTypes...); err != nil {
		return "", "", fmt.Errorf("request_type: %w", err)
	}
	if response, err = optOneOf(data.ResponseType, externalModelResponseTypes...); err != nil {
		return "", "", fmt.Errorf("response_type: %w", err)
	}
	if request == "" {
		request = externalModelRequestTypes[0]
	}
	if response == "" {
		response = externalModelResponseTypes[0]
	}
	if request == "RAW" {
		if response != "SUPER" {
			return "", "", fmt.Errorf("request_type RAW requires response_type SUPER")
		}
		if knownString(data.Prompt) != "" || knownString(data.Suffix) != "" {
			return "", "", fmt.Errorf("prompt and suffix are only supported with request_type UNIFIED")
		}
	}
	return request, response, nil
}

// createExternalModelStatement renders CREATE EXTERNAL MODEL. The inference function is created in the model's
// schema. REQUEST_TYPE and RESPONSE_TYPE are always stated, so the statement records the formats the state holds
// even where they match the defaults.
func createExternalModelStatement(data externalModelModel) (string, error) {
	target, err := externalModelTarget(data.Schema.ValueString(), data.Name.ValueString())
	if err != nil {
		return "", err
	}
	if data.Function.ValueString() == "" {
		return "", fmt.Errorf("function must not be empty")
	}
	if data.ModelID.ValueString() == "" {
		return "", fmt.Errorf("model_id must not be empty")
	}
	role, err := externalModelIAMRole(data.IAMRole.ValueString())
	if err != nil {
		return "", err
	}
	request, response, err := externalModelFormats(data)
	if err != nil {
		return "", err
	}
	settings := []sqlclient.Statement{sqlclient.Fragment().KwLit("MODEL_ID", data.ModelID.ValueString())}
	if prompt := knownString(data.Prompt); prompt != "" {
		settings = append(settings, sqlclient.Fragment().KwLit("PROMPT", prompt))
	}
	if suffix := knownString(data.Suffix); suffix != "" {
		settings = append(settings, sqlclient.Fragment().KwLit("SUFFIX", suffix))
	}
	settings = append(settings, sqlclient.Kw("REQUEST_TYPE", request), sqlclient.Kw("RESPONSE_TYPE", response))
	statement := sqlclient.Stmt("CREATE EXTERNAL MODEL").Append(target).
		KwQualified("FUNCTION", data.Schema.ValueString(), data.Function.ValueString()).
		Kw("IAM_ROLE").Append(role).
		Kw("MODEL_TYPE BEDROCK SETTINGS").Paren(settings...)
	return statement.String(), statement.Err()
}

// dropExternalModelStatement renders DROP MODEL, which also drops the model's inference function. IF EXISTS is not
// used: Delete reads the catalog first, so a model dropped since then is not an error, and a failure stays visible.
// https://docs.aws.amazon.com/redshift/latest/dg/r_DROP_MODEL.html
func dropExternalModelStatement(data externalModelModel) (string, error) {
	target, err := externalModelTarget(data.Schema.ValueString(), data.Name.ValueString())
	if err != nil {
		return "", err
	}
	return sqlclient.Stmt("DROP MODEL").Append(target).String(), nil
}

// listModelsStatement renders SHOW MODEL ALL, which lists the schema and name of every model of the current database
// that the user can use, trained and external alike. No catalog view lists models of another database, so the
// statement runs in the model's database.
// https://docs.aws.amazon.com/redshift/latest/dg/r_SHOW_MODEL.html
func listModelsStatement() string {
	return sqlclient.Stmt("SHOW MODEL ALL").String()
}

// readExternalModelStatement renders SHOW MODEL for one schema-qualified model; it answers with Key and Value rows.
func readExternalModelStatement(schema, name string) (string, error) {
	target, err := externalModelTarget(schema, name)
	if err != nil {
		return "", err
	}
	return sqlclient.Stmt("SHOW MODEL").Append(target).String(), nil
}

// showModelLabel normalizes a SHOW MODEL column label or key, such as "Schema Name", "IAM Role", or "PARAMETERS:",
// to lower snake case. The documentation prints the labels with spaces, and a transport may report them in
// another case or with underscores, so every spelling compares equal.
func showModelLabel(label string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSuffix(strings.TrimSpace(label), ":"))), "_")
}

// showModelValue trims a SHOW MODEL value. The owner and IAM role are documented in single quotes, so one pair of
// surrounding quotes is removed.
func showModelValue(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'") {
		value = value[1 : len(value)-1]
	}
	return value
}

// showModelCell returns the value of the column whose normalized label is column.
func showModelCell(row sqlclient.Row, column string) string {
	for label, value := range row {
		if showModelLabel(label) == column {
			return showModelValue(value)
		}
	}
	return ""
}

// showModelEntry is one SHOW MODEL ALL row.
type showModelEntry struct {
	// schema and name identify the model.
	schema, name string
}

// parseShowModels decodes SHOW MODEL ALL rows, sorted by schema and name so listings are stable.
func parseShowModels(rows []sqlclient.Row) ([]showModelEntry, error) {
	entries := make([]showModelEntry, 0, len(rows))
	for _, row := range rows {
		entry := showModelEntry{schema: showModelCell(row, "schema_name"), name: showModelCell(row, "model_name")}
		if entry.schema == "" || entry.name == "" {
			return nil, fmt.Errorf("SHOW MODEL ALL returned a model without a schema or name")
		}
		entries = append(entries, entry)
	}
	slices.SortFunc(entries, func(a, b showModelEntry) int {
		if order := strings.Compare(a.schema, b.schema); order != 0 {
			return order
		}
		return strings.Compare(a.name, b.name)
	})
	return entries, nil
}

// parseShowModel decodes the Key and Value rows of SHOW MODEL into normalized keys. Blank rows and section headers
// such as "PARAMETERS:" carry no value and are skipped; the first occurrence of a key wins.
func parseShowModel(rows []sqlclient.Row) map[string]string {
	settings := map[string]string{}
	for _, row := range rows {
		key, value := showModelLabel(showModelCell(row, "key")), showModelCell(row, "value")
		if _, seen := settings[key]; key == "" || value == "" || seen {
			continue
		}
		settings[key] = value
	}
	return settings
}
