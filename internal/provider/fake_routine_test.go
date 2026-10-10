package provider

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// fakeRoutineArgument is one declared argument as the fake catalog stores it.
type fakeRoutineArgument struct {
	// name, mode, and dataType are the declared values; dataType is canonical without modifiers, in the catalog's
	// lowercase.
	name, mode, dataType string
}

// fakeRoutine is one stored function or procedure overload.
type fakeRoutine struct {
	// arguments are the declared arguments in order.
	arguments []fakeRoutineArgument
	// returnType, volatility, body, and owner are the catalog values.
	returnType, volatility, body, owner string
	// definer records SECURITY DEFINER.
	definer bool
}

// signature returns the bare input types, as oidvectortypes reports them.
func (r *fakeRoutine) signature() string {
	var inputs []string
	for _, argument := range r.arguments {
		if argument.mode != "OUT" {
			inputs = append(inputs, argument.dataType)
		}
	}
	return strings.Join(inputs, ", ")
}

// routineFamily emulates pg_proc_info, SHOW PARAMETERS, and the DDL of one routine kind.
type routineFamily struct {
	// kind is FUNCTION or PROCEDURE.
	kind string
	// routines maps schema.name(signature) to the stored overload.
	routines map[string]*fakeRoutine
}

var (
	_ = registerFakeFamily("function", func() fakeFamily { return &routineFamily{kind: "FUNCTION", routines: map[string]*fakeRoutine{}} })
	_ = registerFakeFamily("procedure", func() fakeFamily { return &routineFamily{kind: "PROCEDURE", routines: map[string]*fakeRoutine{}} })
)

// fakeRoutineKey identifies an overload.
func fakeRoutineKey(schema, name, signature string) string {
	return schema + "." + name + "(" + signature + ")"
}

// populate stores the lifecycle cases' representative routines.
func (f *routineFamily) populate() {
	if f.kind == "FUNCTION" {
		f.routines[fakeRoutineKey("public", "f_example", "integer")] = &fakeRoutine{arguments: []fakeRoutineArgument{{mode: "IN", dataType: "integer"}}, returnType: "integer", volatility: "i", body: "SELECT $1 + 1", owner: "admin"}
		return
	}
	f.routines[fakeRoutineKey("public", "sp_example", "integer")] = &fakeRoutine{
		arguments: []fakeRoutineArgument{{name: "min_id", mode: "IN", dataType: "integer"}, {name: "total", mode: "OUT", dataType: "bigint"}},
		body:      "BEGIN total := min_id * 2; END;", owner: "admin",
	}
}

// query answers this kind's catalog reads and applies its DDL.
func (f *routineFamily) query(_ *catalog, _ dataapi.Connection, sql string, parameters map[string]string) ([]dataapi.Row, bool, error) {
	switch {
	case strings.HasPrefix(sql, "SELECT p.proname AS "+strings.ToLower(f.kind)+"_name"):
		signature := parameters["arguments"]
		routine, ok := f.routines[fakeRoutineKey(parameters["schema"], parameters["name"], signature)]
		if !ok {
			return nil, true, nil
		}
		return []dataapi.Row{{"owner": routine.owner, "language": strings.ToLower(map[string]string{"FUNCTION": "sql", "PROCEDURE": "plpgsql"}[f.kind]), "volatility": routine.volatility,
			"return_type": routine.returnType, "arguments": routine.signature(), "body": routine.body, "security_definer": strconv.FormatBool(routine.definer)}}, true, nil
	case strings.HasPrefix(sql, "SHOW PARAMETERS OF "+f.kind+" "):
		key, _, err := fakeRoutineTarget(strings.TrimPrefix(sql, "SHOW PARAMETERS OF "+f.kind+" "))
		if err != nil {
			return nil, true, err
		}
		routine, ok := f.routines[key]
		if !ok {
			return nil, true, fmt.Errorf("procedure %s does not exist", key)
		}
		rows := []dataapi.Row{}
		for i, argument := range routine.arguments {
			rows = append(rows, dataapi.Row{"parameter_name": argument.name, "ordinal_position": strconv.Itoa(i + 1), "parameter_type": argument.mode, "data_type": argument.dataType})
		}
		return rows, true, nil
	case strings.HasPrefix(sql, "CREATE "+f.kind+" "), strings.HasPrefix(sql, "CREATE OR REPLACE "+f.kind+" "):
		replace := strings.HasPrefix(sql, "CREATE OR REPLACE ")
		key, routine, err := fakeRoutineDefinition(strings.TrimPrefix(strings.TrimPrefix(sql, "CREATE OR REPLACE "+f.kind+" "), "CREATE "+f.kind+" "))
		if err != nil {
			return nil, true, err
		}
		existing, ok := f.routines[key]
		switch {
		case ok && !replace:
			return nil, true, fmt.Errorf("%s %s already exists", strings.ToLower(f.kind), key)
		case ok:
			routine.owner = existing.owner
		default:
			routine.owner = "admin"
		}
		f.routines[key] = routine
		return nil, true, nil
	case strings.HasPrefix(sql, "ALTER "+f.kind+" "):
		key, rest, err := fakeRoutineTarget(strings.TrimPrefix(sql, "ALTER "+f.kind+" "))
		if err != nil {
			return nil, true, err
		}
		routine, ok := f.routines[key]
		if !ok || !strings.HasPrefix(rest, " OWNER TO ") {
			return nil, true, fmt.Errorf("unexpected ALTER %s: %s", f.kind, sql)
		}
		routine.owner = fakeUnquote(strings.TrimPrefix(rest, " OWNER TO "))
		return nil, true, nil
	case strings.HasPrefix(sql, "DROP "+f.kind+" "):
		key, rest, err := fakeRoutineTarget(strings.TrimPrefix(sql, "DROP "+f.kind+" "))
		if err != nil {
			return nil, true, err
		}
		if _, ok := f.routines[key]; !ok || rest != "" {
			return nil, true, fmt.Errorf("cannot drop %s %s", strings.ToLower(f.kind), key)
		}
		delete(f.routines, key)
		return nil, true, nil
	}
	return nil, false, nil
}

// fakeUnquote reverses sqlclient.Identifier for one quoted name.
func fakeUnquote(quoted string) string {
	return strings.ReplaceAll(strings.TrimSuffix(strings.TrimPrefix(quoted, `"`), `"`), `""`, `"`)
}

// fakeQuotedName reads one quoted identifier at the start of text and returns the rest.
func fakeQuotedName(text string) (name, rest string, err error) {
	if !strings.HasPrefix(text, `"`) {
		return "", "", fmt.Errorf("fake catalog expected a quoted name in %q", text)
	}
	for end := 1; end < len(text); end++ {
		if text[end] != '"' {
			continue
		}
		if end+1 < len(text) && text[end+1] == '"' {
			end++
			continue
		}
		return fakeUnquote(text[:end+1]), text[end+1:], nil
	}
	return "", "", fmt.Errorf("fake catalog found an unterminated name in %q", text)
}

// fakeQualifiedName splits "schema"."name" at the start of text and returns the rest.
func fakeQualifiedName(text string) (schema, name, rest string, err error) {
	schema, rest, err = fakeQuotedName(text)
	if err == nil {
		name, rest, err = fakeQuotedName(strings.TrimPrefix(rest, "."))
	}
	return schema, name, rest, err
}

// fakeArgumentList splits the parenthesized argument list at the start of text at top-level commas.
func fakeArgumentList(text string) (items []string, rest string, err error) {
	if !strings.HasPrefix(text, "(") {
		return nil, "", fmt.Errorf("fake catalog expected an argument list in %q", text)
	}
	depth, quoted, start := 0, false, 1
	for i := 0; i < len(text); i++ {
		switch char := text[i]; {
		case char == '"':
			quoted = !quoted
		case quoted:
		case char == '(':
			depth++
		case char == ',' && depth == 1:
			items = append(items, strings.TrimSpace(text[start:i]))
			start = i + 1
		case char == ')':
			depth--
			if depth == 0 {
				if last := strings.TrimSpace(text[start:i]); last != "" || len(items) > 0 {
					items = append(items, last)
				}
				return items, text[i+1:], nil
			}
		}
	}
	return nil, "", errors.New("fake catalog found an unterminated argument list")
}

// fakeRoutineArguments parses declared arguments: an optional quoted name, an optional mode, and the type.
func fakeRoutineArguments(items []string) ([]fakeRoutineArgument, error) {
	arguments := make([]fakeRoutineArgument, len(items))
	for i, item := range items {
		argument := fakeRoutineArgument{mode: "IN"}
		if strings.HasPrefix(item, `"`) {
			name, rest, err := fakeQuotedName(item)
			if err != nil {
				return nil, err
			}
			argument.name, item = name, strings.TrimSpace(rest)
		}
		for _, mode := range []string{"INOUT", "OUT", "IN"} {
			if strings.HasPrefix(item, mode+" ") {
				argument.mode, item = mode, strings.TrimPrefix(item, mode+" ")
				break
			}
		}
		dataType, err := routineType(item)
		if err != nil {
			return nil, err
		}
		// The catalog spells types in lowercase; the provider uppercases them.
		argument.dataType = strings.ToLower(string(routineBaseType(dataType)))
		arguments[i] = argument
	}
	return arguments, nil
}

// fakeRoutineTarget parses "schema"."name"(signature) and returns the overload key and the rest of the statement.
func fakeRoutineTarget(text string) (key, rest string, err error) {
	schema, name, rest, err := fakeQualifiedName(text)
	if err != nil {
		return "", "", err
	}
	items, rest, err := fakeArgumentList(rest)
	if err != nil {
		return "", "", err
	}
	arguments, err := fakeRoutineArguments(items)
	if err != nil {
		return "", "", err
	}
	routine := fakeRoutine{arguments: arguments}
	return fakeRoutineKey(schema, name, routine.signature()), rest, nil
}

// fakeRoutineDefinition parses the CREATE text after the verb into the overload key and its stored definition.
func fakeRoutineDefinition(text string) (string, *fakeRoutine, error) {
	schema, name, rest, err := fakeQualifiedName(text)
	if err != nil {
		return "", nil, err
	}
	items, rest, err := fakeArgumentList(rest)
	if err != nil {
		return "", nil, err
	}
	arguments, err := fakeRoutineArguments(items)
	if err != nil {
		return "", nil, err
	}
	routine := &fakeRoutine{arguments: arguments}
	head, after, found := strings.Cut(rest, " AS $")
	if !found {
		return "", nil, fmt.Errorf("fake catalog expected a dollar-quoted body in %q", text)
	}
	tagEnd := strings.IndexByte(after, '$')
	if tagEnd < 0 {
		return "", nil, errors.New("fake catalog found an unterminated dollar-quote tag")
	}
	tag := "$" + after[:tagEnd+1]
	body, tail, found := strings.Cut(after[tagEnd+1:], tag)
	if !found {
		return "", nil, errors.New("fake catalog found an unterminated body")
	}
	routine.body, routine.definer = body, strings.Contains(tail, " SECURITY DEFINER")
	if returns, ok := strings.CutPrefix(head, " RETURNS "); ok {
		for code, keyword := range map[string]string{"v": " VOLATILE", "s": " STABLE", "i": " IMMUTABLE"} {
			if dataType, found := strings.CutSuffix(returns, keyword); found {
				canonical, err := routineType(dataType)
				if err != nil {
					return "", nil, err
				}
				routine.returnType, routine.volatility = strings.ToLower(string(routineBaseType(canonical))), code
			}
		}
	}
	return fakeRoutineKey(schema, name, routine.signature()), routine, nil
}
