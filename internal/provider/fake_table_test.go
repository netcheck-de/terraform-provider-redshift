package provider

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

var _ = registerFakeFamily("table", func() fakeFamily {
	return &tableFake{tables: map[string]*tableFakeTable{}, templates: map[string]tableFakeTable{}}
})

// tableFakeColumn is one column as the fake catalog stores it.
type tableFakeColumn struct {
	// name, dataType, defaultText, and encoding are reported as stored.
	name, dataType, defaultText, encoding string
	// notNull, distKey, and sortKey are the key and constraint flags.
	notNull, distKey bool
	sortKey          int
}

// tableFakeConstraint is one constraint with the definition pg_get_constraintdef would print.
type tableFakeConstraint struct {
	// name, kind, and definition are reported as stored.
	name, kind, definition string
	// refSchema and refTable name the referenced table of a foreign key.
	refSchema, refTable string
}

// tableFakeTable is one table of the fake catalog.
type tableFakeTable struct {
	// owner is the owning user.
	owner string
	// distStyle is the pg_class.reldiststyle code.
	distStyle string
	// autoDistStyle is the PG_CLASS_INFO.releffectivediststyle code under DISTSTYLE AUTO; "" hides the row, as for a
	// SQL identity that may not see it.
	autoDistStyle string
	// columns are in attnum order.
	columns []tableFakeColumn
	// constraints are in name order.
	constraints []tableFakeConstraint
	// autoSortKey makes svv_table_info report AUTO(SORTKEY...) instead of the explicit first key column.
	autoSortKey bool
	// unlisted keeps the table out of svv_table_info, as for an empty table.
	unlisted bool
}

// effectiveDistStyle is the releffectivediststyle code PG_CLASS_INFO reports.
func (t *tableFakeTable) effectiveDistStyle() string {
	if t.distStyle == "9" {
		return t.autoDistStyle
	}
	return t.distStyle
}

// sortKey1 is the svv_table_info.sortkey1 value of the table.
func (t *tableFakeTable) sortKey1() string {
	first := ""
	for _, column := range t.columns {
		if column.sortKey == 1 || column.sortKey == -1 {
			first = column.name
		}
	}
	switch {
	case !t.autoSortKey:
		return first
	case first == "":
		return "AUTO(SORTKEY)"
	}
	return "AUTO(SORTKEY(" + first + "))"
}

// clone copies the table so templates and tables never share slices.
func (t tableFakeTable) clone() *tableFakeTable {
	t.columns, t.constraints = slices.Clone(t.columns), slices.Clone(t.constraints)
	return &t
}

// tableFake emulates CREATE, ALTER, and DROP TABLE and the catalog reads of the table resource. CREATE TABLE
// instantiates a template registered under the table's name instead of parsing the column list.
type tableFake struct {
	// tables maps schema.name to the existing tables.
	tables map[string]*tableFakeTable
	// templates maps schema.name to what CREATE TABLE produces.
	templates map[string]tableFakeTable
}

// tableFakeEvents is the representative table that lifecycle tests manage as serving.events.
func tableFakeEvents() tableFakeTable {
	return tableFakeTable{
		owner: "admin", distStyle: "1",
		columns: []tableFakeColumn{
			{name: "id", dataType: "bigint", notNull: true, defaultText: `"identity"(108123, 0, '1,1'::text)`, encoding: "az64", distKey: true, sortKey: 1},
			{name: "label", dataType: "character varying(64)", defaultText: "'none'::character varying", encoding: "lzo"},
		},
		constraints: []tableFakeConstraint{{name: "events_pkey", kind: "p", definition: "PRIMARY KEY (id)"}},
	}
}

// populate creates the representative table.
func (f *tableFake) populate() {
	f.templates["serving.events"] = tableFakeEvents()
	f.tables["serving.events"] = tableFakeEvents().clone()
}

// tableFakeIdent parses one leading quoted identifier.
func tableFakeIdent(text string) (string, string, bool) {
	if !strings.HasPrefix(text, `"`) {
		return "", text, false
	}
	var name strings.Builder
	for i := 1; i < len(text); i++ {
		if text[i] != '"' {
			name.WriteByte(text[i])
			continue
		}
		if i+1 < len(text) && text[i+1] == '"' {
			name.WriteByte('"')
			i++
			continue
		}
		return name.String(), strings.TrimPrefix(text[i+1:], " "), true
	}
	return "", text, false
}

// tableFakeQualified parses "schema"."name".
func tableFakeQualified(text string) (string, string, bool) {
	schemaName, rest, ok := tableFakeIdent(text)
	if !ok || !strings.HasPrefix(rest, ".") {
		return "", "", false
	}
	name, rest, ok := tableFakeIdent(rest[1:])
	if !ok {
		return "", "", false
	}
	return schemaName + "." + name, rest, true
}

// tableFakeIdents parses a parenthesized identifier list.
func tableFakeIdents(text string) ([]string, string, bool) {
	if !strings.HasPrefix(text, "(") {
		return nil, text, false
	}
	var names []string
	rest := text[1:]
	for {
		name, after, ok := tableFakeIdent(rest)
		if !ok {
			return nil, text, false
		}
		names = append(names, name)
		switch {
		case strings.HasPrefix(after, ", "):
			rest = after[2:]
		case strings.HasPrefix(after, ")"):
			return names, strings.TrimPrefix(after[1:], " "), true
		default:
			return nil, text, false
		}
	}
}

// tableFakePlain matches identifiers pg_get_constraintdef prints without quotes.
var tableFakePlain = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// tableFakeDefinitionList prints columns as pg_get_constraintdef does.
func tableFakeDefinitionList(names []string) string {
	quoted := make([]string, len(names))
	for i, name := range names {
		quoted[i] = name
		if !tableFakePlain.MatchString(name) {
			quoted[i] = `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
		}
	}
	return "(" + strings.Join(quoted, ", ") + ")"
}

// column returns the index of the named column, or -1.
func (t *tableFakeTable) column(name string) int {
	return slices.IndexFunc(t.columns, func(c tableFakeColumn) bool { return c.name == name })
}

// tableFakeColumnClause matches the column definition ADD COLUMN renders after the name.
var tableFakeColumnClause = regexp.MustCompile(`^(.+?)(?: DEFAULT (.+?))?(?: ENCODE ([A-Z0-9]+))?( NOT NULL)?$`)

// alter applies one ALTER TABLE clause; ok is false for clauses the fake does not know.
func (t *tableFakeTable) alter(table, clause string) (bool, error) {
	switch {
	case strings.HasPrefix(clause, "OWNER TO "):
		owner, _, ok := tableFakeIdent(strings.TrimPrefix(clause, "OWNER TO "))
		t.owner = owner
		return ok, nil
	case strings.HasPrefix(clause, "ADD COLUMN "):
		name, rest, ok := tableFakeIdent(strings.TrimPrefix(clause, "ADD COLUMN "))
		match := tableFakeColumnClause.FindStringSubmatch(rest)
		if !ok || match == nil {
			return false, nil
		}
		if t.column(name) >= 0 {
			return true, fmt.Errorf("column %q already exists", name)
		}
		if match[4] != "" && match[2] == "" {
			return true, fmt.Errorf("ALTER TABLE ADD COLUMN defined as NOT NULL must have a non-null default expression")
		}
		encoding := strings.ToLower(match[3])
		if encoding == "" {
			encoding = "lzo"
		}
		t.columns = append(t.columns, tableFakeColumn{name: name, dataType: match[1], defaultText: match[2], encoding: encoding, notNull: match[4] != ""})
		return true, nil
	case strings.HasPrefix(clause, "DROP COLUMN "):
		name, _, ok := tableFakeIdent(strings.TrimPrefix(clause, "DROP COLUMN "))
		index := t.column(name)
		if !ok || index < 0 {
			return ok, fmt.Errorf("column %q does not exist", name)
		}
		if t.columns[index].distKey || t.columns[index].sortKey != 0 {
			return true, fmt.Errorf("cannot drop the distribution or sort key column %q", name)
		}
		t.columns = slices.Delete(t.columns, index, index+1)
		return true, nil
	case strings.HasPrefix(clause, "ALTER COLUMN "):
		name, rest, ok := tableFakeIdent(strings.TrimPrefix(clause, "ALTER COLUMN "))
		index := t.column(name)
		if !ok || index < 0 {
			return ok, fmt.Errorf("column %q does not exist", name)
		}
		switch {
		case strings.HasPrefix(rest, "ENCODE "):
			t.columns[index].encoding = strings.ToLower(strings.TrimPrefix(rest, "ENCODE "))
		case strings.HasPrefix(rest, "TYPE "):
			t.columns[index].dataType = strings.TrimPrefix(rest, "TYPE ")
		default:
			return false, nil
		}
		return true, nil
	case strings.HasPrefix(clause, "ALTER DISTSTYLE KEY DISTKEY "), strings.HasPrefix(clause, "ALTER DISTKEY "):
		name, _, ok := tableFakeIdent(clause[strings.LastIndex(clause, "DISTKEY ")+len("DISTKEY "):])
		if !ok || t.column(name) < 0 {
			return ok, fmt.Errorf("distribution key %q does not exist", name)
		}
		t.distStyle = "1"
		for i := range t.columns {
			t.columns[i].distKey = t.columns[i].name == name
		}
		return true, nil
	case strings.HasPrefix(clause, "ALTER DISTSTYLE "):
		code, ok := map[string]string{"EVEN": "0", "ALL": "8", "AUTO": "9"}[strings.TrimPrefix(clause, "ALTER DISTSTYLE ")]
		if ok {
			// The fake's AUTO always starts out as AUTO(EVEN).
			t.distStyle, t.autoDistStyle = code, "11"
			for i := range t.columns {
				t.columns[i].distKey = false
			}
		}
		return ok, nil
	case clause == "ALTER SORTKEY AUTO":
		// Redshift keeps the current sort key and only lets automatic optimization replace it.
		if slices.ContainsFunc(t.columns, func(c tableFakeColumn) bool { return c.sortKey < 0 }) {
			return true, fmt.Errorf("cannot alter an interleaved sort key to AUTO")
		}
		t.autoSortKey = true
		return true, nil
	case clause == "ALTER SORTKEY NONE":
		for i := range t.columns {
			t.columns[i].sortKey = 0
		}
		t.autoSortKey = false
		return true, nil
	case strings.HasPrefix(clause, "ALTER COMPOUND SORTKEY "):
		names, _, ok := tableFakeIdents(strings.TrimPrefix(clause, "ALTER COMPOUND SORTKEY "))
		if !ok {
			return false, nil
		}
		for i := range t.columns {
			position := slices.Index(names, t.columns[i].name) + 1
			// A column that becomes a sort key is changed to RAW, as ALTER TABLE documents.
			if position > 0 && t.columns[i].sortKey == 0 {
				t.columns[i].encoding = "none"
			}
			t.columns[i].sortKey = position
		}
		t.autoSortKey = false
		return true, nil
	case strings.HasPrefix(clause, "DROP CONSTRAINT "):
		name, _, ok := tableFakeIdent(strings.TrimPrefix(clause, "DROP CONSTRAINT "))
		index := slices.IndexFunc(t.constraints, func(c tableFakeConstraint) bool { return c.name == name })
		if !ok || index < 0 {
			return ok, fmt.Errorf("constraint %q does not exist", name)
		}
		t.constraints = slices.Delete(t.constraints, index, index+1)
		return true, nil
	case strings.HasPrefix(clause, "ADD PRIMARY KEY "), strings.HasPrefix(clause, "ADD UNIQUE "):
		kind, keyword, suffix := "p", "PRIMARY KEY", "pkey"
		if strings.HasPrefix(clause, "ADD UNIQUE ") {
			kind, keyword, suffix = "u", "UNIQUE", "key"
		}
		names, _, ok := tableFakeIdents(strings.TrimPrefix(clause, "ADD "+keyword+" "))
		if !ok {
			return false, nil
		}
		name := table + "_" + suffix
		if kind == "u" {
			name = table + "_" + strings.Join(names, "_") + "_" + suffix
		}
		t.constraints = append(t.constraints, tableFakeConstraint{name: name, kind: kind, definition: keyword + " " + tableFakeDefinitionList(names)})
	case strings.HasPrefix(clause, "ADD FOREIGN KEY "):
		names, rest, ok := tableFakeIdents(strings.TrimPrefix(clause, "ADD FOREIGN KEY "))
		if !ok || !strings.HasPrefix(rest, "REFERENCES ") {
			return false, nil
		}
		referenced, rest, ok := tableFakeQualified(strings.TrimPrefix(rest, "REFERENCES "))
		refNames, _, refOK := tableFakeIdents(rest)
		if !ok || !refOK {
			return false, nil
		}
		refSchema, refTable, _ := strings.Cut(referenced, ".")
		t.constraints = append(t.constraints, tableFakeConstraint{
			name: table + "_" + strings.Join(names, "_") + "_fkey", kind: "f", refSchema: refSchema, refTable: refTable,
			definition: "FOREIGN KEY " + tableFakeDefinitionList(names) + " REFERENCES " + refTable + tableFakeDefinitionList(refNames),
		})
	default:
		return false, nil
	}
	slices.SortFunc(t.constraints, func(a, b tableFakeConstraint) int { return strings.Compare(a.name, b.name) })
	return true, nil
}

// query answers the table resource's reads and applies its statements to known tables.
func (f *tableFake) query(_ *catalog, _ dataapi.Connection, sql string, parameters map[string]string) ([]dataapi.Row, bool, error) {
	key := parameters["schema"] + "." + parameters["name"]
	table := f.tables[key]
	switch {
	case strings.HasPrefix(sql, "SELECT c.relname AS table_name"):
		if table == nil {
			return nil, true, nil
		}
		return []dataapi.Row{{"table_name": parameters["name"], "owner": table.owner, "diststyle": table.distStyle, "effective_diststyle": table.effectiveDistStyle()}}, true, nil
	case strings.HasPrefix(sql, "SELECT a.attnum AS position"):
		var rows []dataapi.Row
		for i, column := range tableFakeColumns(table) {
			rows = append(rows, dataapi.Row{"position": strconv.Itoa(i + 1), "column_name": column.name, "data_type": column.dataType, "not_null": strconv.FormatBool(column.notNull)})
		}
		return rows, true, nil
	case strings.HasPrefix(sql, "SELECT column_name, column_default"):
		var rows []dataapi.Row
		for _, column := range tableFakeColumns(table) {
			rows = append(rows, dataapi.Row{"column_name": column.name, "column_default": column.defaultText, "encoding": column.encoding, "distkey": strconv.FormatBool(column.distKey), "sortkey": strconv.Itoa(column.sortKey)})
		}
		return rows, true, nil
	case strings.HasPrefix(sql, "SELECT con.conname"):
		var rows []dataapi.Row
		if table != nil {
			for _, constraint := range table.constraints {
				rows = append(rows, dataapi.Row{"constraint_name": constraint.name, "constraint_type": constraint.kind, "definition": constraint.definition, "referenced_schema": constraint.refSchema, "referenced_table": constraint.refTable})
			}
		}
		return rows, true, nil
	case strings.HasPrefix(sql, "SELECT sortkey1 FROM svv_table_info"):
		if table == nil || table.unlisted {
			return nil, true, nil
		}
		return []dataapi.Row{{"sortkey1": table.sortKey1()}}, true, nil
	case strings.HasPrefix(sql, "CREATE TABLE "):
		name, _, ok := tableFakeQualified(strings.TrimPrefix(sql, "CREATE TABLE "))
		template, known := f.templates[name]
		if !ok || !known {
			return nil, false, nil
		}
		if f.tables[name] != nil {
			return nil, true, fmt.Errorf("table %s already exists", name)
		}
		f.tables[name] = template.clone()
		return nil, true, nil
	case strings.HasPrefix(sql, "DROP TABLE "):
		name, rest, ok := tableFakeQualified(strings.TrimPrefix(sql, "DROP TABLE "))
		if !ok || rest != "" || f.tables[name] == nil {
			return nil, false, nil
		}
		delete(f.tables, name)
		return nil, true, nil
	case strings.HasPrefix(sql, "ALTER TABLE "):
		name, clause, ok := tableFakeQualified(strings.TrimPrefix(sql, "ALTER TABLE "))
		target := f.tables[name]
		if !ok || target == nil {
			return nil, false, nil
		}
		_, tableName, _ := strings.Cut(name, ".")
		handled, err := target.alter(tableName, clause)
		return nil, handled, err
	}
	return nil, false, nil
}

// tableFakeColumns returns the columns of a table that may be absent.
func tableFakeColumns(table *tableFakeTable) []tableFakeColumn {
	if table == nil {
		return nil
	}
	return table.columns
}
