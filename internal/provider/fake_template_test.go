package provider

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// templateFakeFamily names the template family in every fake catalog.
const templateFakeFamily = "template"

// templateFakeSchema is the schema that holds the fake's templates.
const templateFakeSchema = "serving"

// templateFakeUser is the user grantee the template family answers the existence check for.
const templateFakeUser = "template:loader"

// templateFakeDecoy is a second template whose name the LIKE pattern of the managed one also matches, so reads must
// compare names exactly.
const templateFakeDecoy = "csvxload"

var _ = registerFakeFamily(templateFakeFamily, func() fakeFamily { return &templateFake{} })

// templateFakeGrant is one privilege on the template.
type templateFakeGrant struct {
	// identity is the grantee name SHOW GRANTS reports.
	identity string
	// kind is the lowercase identity type SHOW GRANTS reports.
	kind string
	// option records WITH GRANT OPTION.
	option bool
}

// templateFake emulates one COPY template in templateFakeSchema, its grants, and a decoy template, as SHOW TEMPLATES,
// SHOW TEMPLATE, and SHOW GRANTS ON TEMPLATE report them.
type templateFake struct {
	// exists records whether the managed template exists.
	exists bool
	// name is the template name.
	name string
	// owner is the template owner.
	owner string
	// definition is the stored format and parameters.
	definition copyDefinition
	// grants maps privilege names to their grantee.
	grants map[string]templateFakeGrant
	// schemaMissing hides templateFakeSchema, as after the schema was dropped.
	schemaMissing bool
	// withoutDecoy removes the decoy template.
	withoutDecoy bool
}

// populate makes the representative CSV template exist.
func (f *templateFake) populate() {
	f.exists, f.name, f.owner = true, "csv_load", "admin"
	f.definition = copyDefinition{format: "CSV", parameters: map[string]string{"IGNOREHEADER": "1", "DELIMITER": ","}}
	f.grants = map[string]templateFakeGrant{}
}

// templateFakeLike matches a LIKE pattern the way Redshift does, with % and _ as wildcards.
func templateFakeLike(pattern, name string) bool {
	expression := regexp.QuoteMeta(pattern)
	expression = strings.NewReplacer("%", ".*", "_", ".").Replace(expression)
	return regexp.MustCompile("^" + expression + "$").MatchString(name)
}

// templateFakeShow prints the definition as SHOW TEMPLATE does: CREATE OR REPLACE with a three-part name, FORMAT AS
// kept, AS dropped elsewhere, and the parameters in an order unrelated to the configuration.
func (f *templateFake) templateFakeShow(database string) string {
	parts := []string{fmt.Sprintf("CREATE OR REPLACE TEMPLATE %s.%s.%s FOR COPY AS", database, templateFakeSchema, templateFakeName(f.name))}
	if f.definition.format != "" {
		format := "FORMAT AS " + f.definition.format
		if f.definition.formatArgument != "" {
			format += " " + dataapi.Literal(f.definition.formatArgument)
		}
		parts = append(parts, format)
	}
	names := slices.Sorted(maps.Keys(f.definition.parameters))
	slices.Reverse(names)
	for _, name := range names {
		value := f.definition.parameters[name]
		parameter, _ := copyParameterByWord(name)
		switch {
		case parameter.argument == copyText || parameter.argument == copyOptionalText && value != "":
			parts = append(parts, name+" "+dataapi.Literal(value))
		case parameter.argument == copyInteger || parameter.argument == copyChoice:
			parts = append(parts, name+" "+value)
		default:
			parts = append(parts, name)
		}
	}
	return strings.Join(parts, " ") + ";"
}

// templateFakePlainName matches names SHOW TEMPLATE can print without quotes.
var templateFakePlainName = regexp.MustCompile(`^[a-z_][a-z0-9_$]*$`)

// templateFakeName prints a name unquoted when it can be, as the AWS examples show, and quoted otherwise.
func templateFakeName(name string) string {
	if templateFakePlainName.MatchString(name) {
		return name
	}
	return dataapi.Identifier(name)
}

// templateFakeTokens tokenizes a statement the fake received from the provider.
func templateFakeTokens(sql string) []dataapi.Token {
	tokens, err := dataapi.Tokens(sql)
	if err != nil {
		panic("fake template received untokenizable SQL: " + sql)
	}
	return tokens
}

// templateFakeTarget returns the template name of a statement: the last identifier before the first keyword after
// TEMPLATE.
func templateFakeTarget(tokens []dataapi.Token) (string, int) {
	name, index := "", 0
	for i := 0; i < len(tokens); i++ {
		if tokens[i].Kind == dataapi.TokenWord && strings.EqualFold(tokens[i].Text, "TEMPLATE") {
			for i++; i < len(tokens) && tokens[i].Kind != dataapi.TokenWord; i++ {
				if tokens[i].Kind == dataapi.TokenIdentifier {
					name = tokens[i].Text
				}
			}
			return name, i
		}
	}
	return name, index
}

// query answers template reads and applies template DDL and grants; other SQL passes to the next family.
func (f *templateFake) query(_ *catalog, connection dataapi.Connection, sql string, parameters map[string]string) ([]dataapi.Row, bool, error) {
	switch {
	case sql == "SELECT usename FROM pg_user WHERE usename = :name" && parameters["name"] == templateFakeUser:
		return []dataapi.Row{{"usename": templateFakeUser}}, true, nil
	case sql == "SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema" && parameters["schema"] == templateFakeSchema:
		if f.schemaMissing {
			return nil, true, nil
		}
		return []dataapi.Row{{"schema_name": templateFakeSchema}}, true, nil
	case strings.HasPrefix(sql, "SELECT schema_name FROM svv_all_schemas") && strings.Contains(sql, "LOWER(schema_type) = 'local'"):
		return []dataapi.Row{{"schema_name": "public"}, {"schema_name": templateFakeSchema}}, true, nil
	case strings.HasPrefix(sql, "SHOW TEMPLATES FROM SCHEMA "):
		if f.schemaMissing || !strings.Contains(sql, dataapi.Identifier(templateFakeSchema)) {
			return nil, true, nil
		}
		pattern := "%"
		if _, rest, found := strings.Cut(sql, " LIKE "); found {
			pattern = templateFakeTokens(rest)[0].Text
		}
		var rows []dataapi.Row
		for _, name := range []string{f.name, templateFakeDecoy} {
			if (name == f.name && f.exists || name == templateFakeDecoy && !f.withoutDecoy) && templateFakeLike(pattern, name) {
				rows = append(rows, dataapi.Row{"template_name": name, "template_id": "107685", "template_type": "COPY", "template_owner": f.owner, "database_name": connection.Database, "schema_name": templateFakeSchema})
			}
		}
		return rows, true, nil
	case strings.HasPrefix(sql, "SHOW TEMPLATE "):
		name, _ := templateFakeTarget(templateFakeTokens(sql))
		if !f.exists || name != f.name {
			return nil, true, fmt.Errorf("template %q does not exist", name)
		}
		return []dataapi.Row{{"create_template_statement": f.templateFakeShow(connection.Database)}}, true, nil
	case strings.HasPrefix(sql, "CREATE TEMPLATE "), strings.HasPrefix(sql, "CREATE OR REPLACE TEMPLATE "):
		if f.exists && strings.HasPrefix(sql, "CREATE TEMPLATE ") {
			return nil, true, errors.New("template already exists")
		}
		definition, err := parseTemplateDefinition(sql)
		if err != nil {
			return nil, true, err
		}
		name, _ := templateFakeTarget(templateFakeTokens(sql))
		f.exists, f.name, f.owner, f.definition = true, name, "admin", definition
		if f.grants == nil {
			f.grants = map[string]templateFakeGrant{}
		}
	case strings.HasPrefix(sql, "ALTER TEMPLATE "):
		return nil, true, f.alter(sql)
	case strings.HasPrefix(sql, "DROP TEMPLATE "):
		if !f.exists {
			return nil, true, errors.New("template does not exist")
		}
		f.exists, f.grants = false, map[string]templateFakeGrant{}
	case strings.HasPrefix(sql, "SHOW GRANTS ON TEMPLATE "):
		if !f.exists {
			return nil, true, errors.New("template does not exist")
		}
		var rows []dataapi.Row
		for _, privilege := range slices.Sorted(maps.Keys(f.grants)) {
			grant := f.grants[privilege]
			rows = append(rows, dataapi.Row{"database_name": connection.Database, "schema_name": templateFakeSchema, "object_name": f.name, "object_type": "TEMPLATE", "privilege_type": privilege, "identity_name": grant.identity, "identity_type": grant.kind, "admin_option": grantFakeBool(grant.option), "privilege_scope": "TEMPLATE"})
		}
		return rows, true, nil
	case (strings.HasPrefix(sql, "GRANT ") || strings.HasPrefix(sql, "REVOKE ")) && strings.Contains(sql, " ON TEMPLATE "):
		return nil, true, f.grant(sql)
	default:
		return nil, false, nil
	}
	return nil, true, nil
}

// alter applies RENAME TO, OWNER TO, ADD, DROP, and SET.
func (f *templateFake) alter(sql string) error {
	tokens := templateFakeTokens(sql)
	name, i := templateFakeTarget(tokens)
	if !f.exists || name != f.name {
		return fmt.Errorf("template %q does not exist", name)
	}
	if i >= len(tokens) {
		return fmt.Errorf("ALTER TEMPLATE without an action: %s", sql)
	}
	action, rest := strings.ToUpper(tokens[i].Text), tokens[i+1:]
	switch action {
	case "RENAME":
		f.name = rest[1].Text
	case "OWNER":
		f.owner = rest[1].Text
	case "ADD":
		added, err := parseCopyOptions(rest)
		if err != nil {
			return err
		}
		for parameter, value := range added.parameters {
			if _, exists := f.definition.parameters[parameter]; exists {
				return fmt.Errorf("parameter %s already exists", parameter)
			}
			f.definition.parameters[parameter] = value
		}
	case "DROP":
		parameter := strings.ToUpper(rest[0].Text)
		if _, exists := f.definition.parameters[parameter]; !exists || len(rest) != 1 {
			return fmt.Errorf("cannot drop parameter %s", parameter)
		}
		delete(f.definition.parameters, parameter)
	case "SET":
		parameter := strings.ToUpper(rest[0].Text)
		if f.definition.parameters[parameter] == "" || len(rest) != 3 || !strings.EqualFold(rest[1].Text, "TO") {
			return fmt.Errorf("cannot set parameter %s", parameter)
		}
		f.definition.parameters[parameter] = strings.ToUpper(rest[2].Text)
		if rest[2].Kind == dataapi.TokenString {
			f.definition.parameters[parameter] = rest[2].Text
		}
	default:
		return fmt.Errorf("unsupported ALTER TEMPLATE action %s", action)
	}
	return nil
}

// grant applies GRANT or REVOKE of one privilege on the template, with or without its grant option.
func (f *templateFake) grant(sql string) error {
	if !f.exists {
		return errors.New("template does not exist")
	}
	fields := strings.Fields(sql)
	grantee := sql[strings.LastIndex(sql, " TO ")+len(" TO "):]
	if fields[0] == "REVOKE" {
		grantee = sql[strings.LastIndex(sql, " FROM ")+len(" FROM "):]
	}
	grantee = strings.TrimSuffix(grantee, " WITH GRANT OPTION")
	kind := "user"
	for _, prefix := range []string{"ROLE ", "GROUP "} {
		if trimmed, ok := strings.CutPrefix(grantee, prefix); ok {
			kind, grantee = strings.ToLower(strings.TrimSpace(prefix)), trimmed
		}
	}
	identity := strings.Trim(grantee, `"`)
	if grantee == "PUBLIC" {
		kind, identity = "public", "public"
	}
	switch {
	case fields[0] == "GRANT":
		f.grants[fields[1]] = templateFakeGrant{identity: identity, kind: kind, option: strings.HasSuffix(sql, " WITH GRANT OPTION")}
	case strings.HasPrefix(sql, "REVOKE GRANT OPTION FOR "):
		grant := f.grants[fields[4]]
		grant.option = false
		f.grants[fields[4]] = grant
	default:
		delete(f.grants, fields[1])
	}
	return nil
}
