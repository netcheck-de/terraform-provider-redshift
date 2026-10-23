package provider

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

var _ = registerFakeFamily("external_model", func() fakeFamily { return &externalModelFamily{models: map[string]*fakeExternalModel{}} })

// Representative model that fullCatalog populates and the lifecycle case manages.
const (
	fakeModelSchema   = "serving"
	fakeModelName     = "summarizer"
	fakeModelFunction = "f_summarize"
	fakeModelID       = "anthropic.claude-v2"
	fakeModelOwner    = "analyst"
	fakeModelPrompt   = "Summarize the following text:"
)

// fakeExternalModel is one model in the fake catalog, with the settings SHOW MODEL reports.
type fakeExternalModel struct {
	// schema and name identify the model.
	schema, name string
	// owner is the creating user.
	owner string
	// settings maps the SHOW MODEL keys after PARAMETERS: to their values, as the catalog prints them.
	settings map[string]string
}

// externalModelFamily emulates external models keyed by schema and name.
// fakeModelDefaultRole is how SHOW MODEL prints IAM_ROLE DEFAULT to the model's owner, as observed live; SHOW MODEL
// does not report the response type at all.
const fakeModelDefaultRole = "default-aws-iam-role"

// externalModelFamily fakes external models and their SHOW MODEL output.
type externalModelFamily struct {
	// models maps schema and name to the model.
	models map[string]*fakeExternalModel
	// hideSettings makes SHOW MODEL report only the documented general keys, as it may for external models.
	hideSettings bool
}

// populate adds the representative model.
func (f *externalModelFamily) populate() {
	f.models[fakeViewKey(fakeModelSchema, fakeModelName)] = &fakeExternalModel{schema: fakeModelSchema, name: fakeModelName, owner: fakeModelOwner, settings: map[string]string{
		"Model Type": "bedrock", "Function Name": fakeModelFunction, "IAM Role": fakeModelDefaultRole, "Model ID": fakeModelID,
		"Prompt": fakeModelPrompt, "Request Type": "unified",
	}}
}

// remove deletes a model, as an outside DROP MODEL would.
func (f *externalModelFamily) remove(name string) {
	delete(f.models, fakeViewKey(fakeModelSchema, name))
}

// get returns a model for test setup and assertions.
func (f *externalModelFamily) get(name string) *fakeExternalModel {
	return f.models[fakeViewKey(fakeModelSchema, name)]
}

// fakeExternalModelPattern matches the CREATE EXTERNAL MODEL statement the resource renders.
var fakeExternalModelPattern = regexp.MustCompile(`^CREATE EXTERNAL MODEL ("(?:[^"]|"")*"\."(?:[^"]|"")*") FUNCTION "((?:[^"]|"")*)" IAM_ROLE (DEFAULT|'[^']*') MODEL_TYPE BEDROCK SETTINGS \((.*)\)$`)

// fakeExternalModelSetting matches one SETTINGS item.
var fakeExternalModelSetting = regexp.MustCompile(`(MODEL_ID|PROMPT|SUFFIX) '((?:[^']|'')*)'|(REQUEST_TYPE|RESPONSE_TYPE) (\w+)`)

// fakeExternalModelSettingKeys maps SETTINGS keywords to the SHOW MODEL keys the fake reports.
var fakeExternalModelSettingKeys = map[string]string{
	"MODEL_ID": "Model ID", "PROMPT": "Prompt", "SUFFIX": "Suffix", "REQUEST_TYPE": "Request Type", "RESPONSE_TYPE": "Response Type",
}

// show renders SHOW MODEL rows in the documented layout: general keys, a blank row, and a PARAMETERS: section.
func (m *fakeExternalModel) show(hideSettings bool) []sqlclient.Row {
	rows := []sqlclient.Row{
		{"Key": "Model Name", "Value": m.name}, {"Key": "Schema Name", "Value": m.schema}, {"Key": "Owner", "Value": m.owner},
		{"Key": "Creation Time", "Value": "Sat, 15.01.2000 14:45:20"}, {"Key": "Model State", "Value": "READY"},
		{"Key": "", "Value": ""}, {"Key": "PARAMETERS:", "Value": ""},
	}
	if hideSettings {
		return rows
	}
	for _, key := range slices.Sorted(maps.Keys(m.settings)) {
		value := m.settings[key]
		if key == "IAM Role" {
			value = "'" + value + "'"
		}
		rows = append(rows, sqlclient.Row{"Key": key, "Value": value})
	}
	return rows
}

// query applies model DDL and answers SHOW MODEL.
func (f *externalModelFamily) query(_ *catalog, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, bool, error) {
	switch {
	case sql == "SHOW MODEL ALL":
		var rows []sqlclient.Row
		for _, key := range slices.Sorted(maps.Keys(f.models)) {
			rows = append(rows, sqlclient.Row{"Schema Name": f.models[key].schema, "Model Name": f.models[key].name})
		}
		return rows, true, nil
	case strings.HasPrefix(sql, "SHOW MODEL "):
		schema, name, tail, err := fakeViewTarget(strings.TrimPrefix(sql, "SHOW MODEL "))
		if err != nil || tail != "" {
			return nil, true, fmt.Errorf("fake model catalog cannot parse %q", sql)
		}
		model := f.models[fakeViewKey(schema, name)]
		if model == nil {
			return nil, true, fmt.Errorf("model %q does not exist", name)
		}
		return model.show(f.hideSettings), true, nil
	case strings.HasPrefix(sql, "CREATE EXTERNAL MODEL "):
		match := fakeExternalModelPattern.FindStringSubmatch(sql)
		if match == nil {
			return nil, true, fmt.Errorf("fake model catalog cannot parse %q", sql)
		}
		schema, name, _, err := fakeViewTarget(match[1])
		if err != nil {
			return nil, true, err
		}
		if f.models[fakeViewKey(schema, name)] != nil {
			return nil, true, fmt.Errorf("model %q already exists", name)
		}
		role := strings.ReplaceAll(strings.Trim(match[3], "'"), "''", "'")
		if role == "DEFAULT" {
			role = fakeModelDefaultRole
		}
		settings := map[string]string{"Model Type": "bedrock", "Function Name": strings.ReplaceAll(match[2], `""`, `"`), "IAM Role": role}
		for _, setting := range fakeExternalModelSetting.FindAllStringSubmatch(match[4], -1) {
			if setting[1] != "" {
				settings[fakeExternalModelSettingKeys[setting[1]]] = strings.ReplaceAll(strings.ReplaceAll(setting[2], "''", "'"), `\\`, `\`)
			} else if setting[3] != "RESPONSE_TYPE" {
				settings[fakeExternalModelSettingKeys[setting[3]]] = strings.ToLower(setting[4])
			}
		}
		f.models[fakeViewKey(schema, name)] = &fakeExternalModel{schema: schema, name: name, owner: "admin", settings: settings}
		return nil, true, nil
	case strings.HasPrefix(sql, "DROP MODEL "):
		schema, name, _, err := fakeViewTarget(strings.TrimPrefix(sql, "DROP MODEL "))
		if err != nil {
			return nil, true, err
		}
		if f.models[fakeViewKey(schema, name)] == nil {
			return nil, true, fmt.Errorf("model %q does not exist", name)
		}
		delete(f.models, fakeViewKey(schema, name))
		return nil, true, nil
	}
	return nil, false, nil
}
