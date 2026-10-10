package provider

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// externalTableFakeSchema is the external schema the legacy fake catalog maps; the family handles only statements
// on it, so other families can own ALTER TABLE and DROP TABLE for local tables.
const externalTableFakeSchema = "example_external"

var _ = registerFakeFamily("external_table", func() fakeFamily { return &externalTableFamily{tables: map[string]*externalTableFake{}} })

// externalTableFakeColumn is a column as the Glue catalog stores it: lowercase name and Hive type.
type externalTableFakeColumn struct {
	// name is the lowercase column name.
	name string
	// hiveType is the catalog external_type.
	hiveType string
}

// externalTableFakePartition is one registered partition.
type externalTableFakePartition struct {
	// values are the partition values in key order.
	values []string
	// location is stored without a trailing slash, as SVV_EXTERNAL_PARTITIONS reports it.
	location string
}

// externalTableFake is one external table in the fake Glue catalog.
type externalTableFake struct {
	// location, inputFormat, outputFormat and serde are the storage descriptor.
	location, inputFormat, outputFormat, serde string
	// serdeParameters and parameters are the SerDe parameters and table properties.
	serdeParameters, parameters map[string]string
	// columns and partitionKeys are the ordered columns.
	columns, partitionKeys []externalTableFakeColumn
	// partitions are the registered partitions.
	partitions []externalTableFakePartition
}

// externalTableFamily emulates Spectrum tables and partitions in the legacy external schema.
type externalTableFamily struct {
	// tables maps lowercase table names to their definition.
	tables map[string]*externalTableFake
}

// externalTableFakeEvents is the representative table the lifecycle cases manage.
func externalTableFakeEvents() *externalTableFake {
	return &externalTableFake{
		location:        "s3://example-bucket/events/",
		inputFormat:     externalTableFileFormats["TEXTFILE"],
		outputFormat:    "org.apache.hadoop.hive.ql.io.HiveIgnoreKeyTextOutputFormat",
		serde:           externalTableImpliedSerdes["TEXTFILE"],
		serdeParameters: map[string]string{"field.delim": ",", "serialization.format": ","},
		parameters:      map[string]string{"EXTERNAL": "TRUE", "skip.header.line.count": "1", "transient_lastDdlTime": "1700000000"},
		columns:         []externalTableFakeColumn{{"id", "int"}, {"label", "varchar(64)"}},
		partitionKeys:   []externalTableFakeColumn{{"event_date", "date"}},
		partitions:      []externalTableFakePartition{{values: []string{"2024-01-01"}, location: "s3://example-bucket/events/event_date=2024-01-01"}},
	}
}

// populate registers the representative table and its partition.
func (f *externalTableFamily) populate() {
	f.tables["events"] = externalTableFakeEvents()
}

// query answers the catalog views and applies the DDL on the fake external schema.
func (f *externalTableFamily) query(c *catalog, _ dataapi.Connection, sql string, parameters map[string]string) ([]dataapi.Row, bool, error) {
	relation := dataapi.Identifier(externalTableFakeSchema) + "."
	switch {
	case strings.HasPrefix(sql, "SELECT schemaname, tablename, location, input_format"):
		table := f.lookup(c, parameters["schema"], parameters["name"])
		if table == nil {
			return nil, true, nil
		}
		serdeParameters, _ := json.Marshal(table.serdeParameters)
		tableParameters, _ := json.Marshal(table.parameters)
		return []dataapi.Row{{
			"schemaname": externalTableFakeSchema, "tablename": strings.ToLower(parameters["name"]), "location": table.location,
			"input_format": table.inputFormat, "output_format": table.outputFormat, "serialization_lib": table.serde,
			"serde_parameters": string(serdeParameters), "parameters": string(tableParameters),
		}}, true, nil
	case strings.HasPrefix(sql, "SELECT columnname, external_type"):
		table := f.lookup(c, parameters["schema"], parameters["table"])
		if table == nil {
			return nil, true, nil
		}
		var rows []dataapi.Row
		for i, column := range table.columns {
			rows = append(rows, dataapi.Row{"columnname": column.name, "external_type": column.hiveType, "columnnum": fmt.Sprint(i + 1), "part_key": "0"})
		}
		for i, column := range table.partitionKeys {
			rows = append(rows, dataapi.Row{"columnname": column.name, "external_type": column.hiveType, "columnnum": fmt.Sprint(len(table.columns) + i + 1), "part_key": fmt.Sprint(i + 1)})
		}
		return rows, true, nil
	case strings.HasPrefix(sql, "SELECT values, location FROM svv_external_partitions"):
		table := f.lookup(c, parameters["schema"], parameters["table"])
		if table == nil {
			return nil, true, nil
		}
		var rows []dataapi.Row
		for _, partition := range table.partitions {
			values, _ := json.Marshal(partition.values)
			rows = append(rows, dataapi.Row{"values": string(values), "location": partition.location})
		}
		return rows, true, nil
	case strings.HasPrefix(sql, "CREATE EXTERNAL TABLE "+relation):
		if !c.external {
			return nil, true, fmt.Errorf("external schema %s does not exist", externalTableFakeSchema)
		}
		return nil, true, f.create(sql)
	case strings.HasPrefix(sql, "ALTER TABLE "+relation):
		return nil, true, f.alter(sql)
	case strings.HasPrefix(sql, "DROP TABLE "+relation):
		tokens, err := externalTableFakeTokens(strings.TrimPrefix(sql, "DROP TABLE "))
		if err != nil {
			return nil, true, err
		}
		name, rest := externalTableFakeRelation(tokens)
		if f.tables[name] == nil || len(rest) > 0 {
			return nil, true, fmt.Errorf("cannot drop external table %q", name)
		}
		delete(f.tables, name)
		return nil, true, nil
	}
	return nil, false, nil
}

// lookup returns the table when the external schema mapping exists.
func (f *externalTableFamily) lookup(c *catalog, schema, name string) *externalTableFake {
	if !c.external || !strings.EqualFold(schema, externalTableFakeSchema) {
		return nil
	}
	return f.tables[strings.ToLower(name)]
}

// externalTableFakeToken is one lexical token of the fake's SQL reader.
type externalTableFakeToken struct {
	// kind is 'i' for a quoted identifier, 'l' for a literal, 'p' for punctuation and 'w' for a word.
	kind byte
	// text is the unquoted value.
	text string
}

// externalTableFakeTokens splits rendered SQL into tokens, undoing sqlclient.Identifier and sqlclient.Literal.
func externalTableFakeTokens(sql string) ([]externalTableFakeToken, error) {
	var tokens []externalTableFakeToken
	for i := 0; i < len(sql); {
		switch char := sql[i]; {
		case char == ' ' || char == '\n' || char == '\t':
			i++
		case char == '"' || char == '\'':
			var text strings.Builder
			j := i + 1
			for ; j < len(sql); j++ {
				if char == '\'' && sql[j] == '\\' && j+1 < len(sql) && sql[j+1] == '\\' {
					text.WriteByte('\\')
					j++
					continue
				}
				if sql[j] == char {
					if j+1 < len(sql) && sql[j+1] == char {
						text.WriteByte(char)
						j++
						continue
					}
					break
				}
				text.WriteByte(sql[j])
			}
			if j >= len(sql) {
				return nil, fmt.Errorf("unterminated quote in %q", sql)
			}
			kind := byte('l')
			if char == '"' {
				kind = 'i'
			}
			tokens = append(tokens, externalTableFakeToken{kind, text.String()})
			i = j + 1
		case strings.IndexByte("(),=.", char) >= 0:
			tokens = append(tokens, externalTableFakeToken{'p', string(char)})
			i++
		default:
			j := i
			for j < len(sql) && strings.IndexByte(" \n\t(),=.\"'", sql[j]) < 0 {
				j++
			}
			tokens = append(tokens, externalTableFakeToken{'w', sql[i:j]})
			i = j
		}
	}
	return tokens, nil
}

// externalTableFakeRelation reads "schema"."table" and returns the lowercase table name and the rest.
func externalTableFakeRelation(tokens []externalTableFakeToken) (string, []externalTableFakeToken) {
	if len(tokens) < 3 || tokens[1].text != "." {
		return "", nil
	}
	return strings.ToLower(tokens[2].text), tokens[3:]
}

// externalTableFakeGroup returns the tokens inside the parenthesis opening tokens and the rest after it.
func externalTableFakeGroup(tokens []externalTableFakeToken) (inside, rest []externalTableFakeToken, err error) {
	if len(tokens) == 0 || tokens[0].text != "(" {
		return nil, nil, fmt.Errorf("expected ( in fake SQL")
	}
	depth := 0
	for i, token := range tokens {
		if token.kind != 'p' {
			continue
		}
		switch token.text {
		case "(":
			depth++
		case ")":
			depth--
			if depth == 0 {
				return tokens[1:i], tokens[i+1:], nil
			}
		}
	}
	return nil, nil, fmt.Errorf("unbalanced parentheses in fake SQL")
}

// externalTableFakeItems splits a group at top-level commas.
func externalTableFakeItems(tokens []externalTableFakeToken) [][]externalTableFakeToken {
	var items [][]externalTableFakeToken
	depth, start := 0, 0
	for i, token := range tokens {
		switch {
		case token.kind == 'p' && token.text == "(":
			depth++
		case token.kind == 'p' && token.text == ")":
			depth--
		case token.kind == 'p' && token.text == "," && depth == 0:
			items = append(items, tokens[start:i])
			start = i + 1
		}
	}
	return append(items, tokens[start:])
}

// externalTableFakeHiveTypes maps rendered Redshift type names to the Hive names Glue stores.
var externalTableFakeHiveTypes = map[string]string{"integer": "int", "real": "float", "double precision": "double"}

// externalTableFakeColumn converts "name" type tokens to a catalog column.
func externalTableFakeColumnOf(tokens []externalTableFakeToken) (externalTableFakeColumn, error) {
	if len(tokens) < 2 || tokens[0].kind != 'i' {
		return externalTableFakeColumn{}, fmt.Errorf("malformed fake column definition")
	}
	var words []string
	dataType := ""
	for _, token := range tokens[1:] {
		switch {
		case token.kind == 'w' && strings.HasSuffix(dataType, "("):
			dataType += token.text
		case token.kind == 'w' && strings.HasSuffix(dataType, ","):
			dataType += token.text
		case token.kind == 'w':
			words = append(words, token.text)
			dataType = strings.Join(words, " ")
		default:
			dataType += token.text
		}
	}
	// Glue stores Hive types in lowercase.
	dataType = strings.ToLower(dataType)
	if hive, ok := externalTableFakeHiveTypes[dataType]; ok {
		dataType = hive
	}
	return externalTableFakeColumn{name: strings.ToLower(tokens[0].text), hiveType: dataType}, nil
}

// externalTableFakePairs reads 'name' = 'value' items.
func externalTableFakePairs(tokens []externalTableFakeToken) (map[string]string, error) {
	pairs := map[string]string{}
	for _, item := range externalTableFakeItems(tokens) {
		if len(item) != 3 || item[1].text != "=" {
			return nil, fmt.Errorf("malformed fake property pair")
		}
		pairs[item[0].text] = item[2].text
	}
	return pairs, nil
}

// externalTableFakeExpect consumes the given words.
func externalTableFakeExpect(tokens []externalTableFakeToken, words ...string) ([]externalTableFakeToken, bool) {
	if len(tokens) < len(words) {
		return tokens, false
	}
	for i, word := range words {
		if tokens[i].kind != 'w' || tokens[i].text != word {
			return tokens, false
		}
	}
	return tokens[len(words):], true
}

// create applies CREATE EXTERNAL TABLE as Glue would record it.
func (f *externalTableFamily) create(sql string) error {
	tokens, err := externalTableFakeTokens(strings.TrimPrefix(sql, "CREATE EXTERNAL TABLE "))
	if err != nil {
		return err
	}
	name, rest := externalTableFakeRelation(tokens)
	if f.tables[name] != nil {
		return fmt.Errorf("external table %q already exists", name)
	}
	table := &externalTableFake{serdeParameters: map[string]string{}, parameters: map[string]string{"EXTERNAL": "TRUE", "transient_lastDdlTime": "1700000000"}}
	inside, rest, err := externalTableFakeGroup(rest)
	if err != nil {
		return err
	}
	if table.columns, err = externalTableFakeColumns(inside); err != nil {
		return err
	}
	if after, ok := externalTableFakeExpect(rest, "PARTITIONED", "BY"); ok {
		if inside, rest, err = externalTableFakeGroup(after); err != nil {
			return err
		}
		if table.partitionKeys, err = externalTableFakeColumns(inside); err != nil {
			return err
		}
	}
	if after, ok := externalTableFakeExpect(rest, "ROW", "FORMAT", "DELIMITED"); ok {
		rest = after
		if after, ok := externalTableFakeExpect(rest, "FIELDS", "TERMINATED", "BY"); ok {
			table.serdeParameters["field.delim"], table.serdeParameters["serialization.format"], rest = after[0].text, after[0].text, after[1:]
		}
		if after, ok := externalTableFakeExpect(rest, "LINES", "TERMINATED", "BY"); ok {
			table.serdeParameters["line.delim"], rest = after[0].text, after[1:]
		}
	}
	if after, ok := externalTableFakeExpect(rest, "ROW", "FORMAT", "SERDE"); ok {
		table.serde, rest = after[0].text, after[1:]
		table.serdeParameters["serialization.format"] = "1"
		if after, ok := externalTableFakeExpect(rest, "WITH", "SERDEPROPERTIES"); ok {
			if inside, rest, err = externalTableFakeGroup(after); err != nil {
				return err
			}
			pairs, err := externalTableFakePairs(inside)
			if err != nil {
				return err
			}
			maps.Copy(table.serdeParameters, pairs)
		}
	}
	rest, ok := externalTableFakeExpect(rest, "STORED", "AS")
	if !ok || len(rest) < 1 {
		return fmt.Errorf("fake CREATE EXTERNAL TABLE lacks STORED AS")
	}
	if after, ok := externalTableFakeExpect(rest, "INPUTFORMAT"); ok {
		table.inputFormat, rest = after[0].text, after[1:]
		if after, ok = externalTableFakeExpect(rest, "OUTPUTFORMAT"); !ok {
			return fmt.Errorf("fake CREATE EXTERNAL TABLE lacks OUTPUTFORMAT")
		}
		table.outputFormat, rest = after[0].text, after[1:]
	} else {
		format, err := dataapi.OneOf(rest[0].text, slices.Collect(maps.Keys(externalTableFileFormats))...)
		if err != nil {
			return err
		}
		table.inputFormat, table.outputFormat, rest = externalTableFileFormats[format], "org.apache.hadoop.hive.ql.io.HiveIgnoreKeyTextOutputFormat", rest[1:]
		if table.serde == "" {
			table.serde = externalTableImpliedSerdes[format]
		}
	}
	if table.serde == "" {
		table.serde = externalTableImpliedSerdes["TEXTFILE"]
	}
	if rest, ok = externalTableFakeExpect(rest, "LOCATION"); !ok {
		return fmt.Errorf("fake CREATE EXTERNAL TABLE lacks LOCATION")
	}
	table.location, rest = rest[0].text, rest[1:]
	if after, ok := externalTableFakeExpect(rest, "TABLE", "PROPERTIES"); ok {
		if inside, rest, err = externalTableFakeGroup(after); err != nil {
			return err
		}
		pairs, err := externalTableFakePairs(inside)
		if err != nil {
			return err
		}
		maps.Copy(table.parameters, pairs)
	}
	if len(rest) > 0 {
		return fmt.Errorf("fake CREATE EXTERNAL TABLE has trailing tokens %v", rest)
	}
	f.tables[name] = table
	return nil
}

// externalTableFakeColumns reads a column list.
func externalTableFakeColumns(tokens []externalTableFakeToken) ([]externalTableFakeColumn, error) {
	var columns []externalTableFakeColumn
	for _, item := range externalTableFakeItems(tokens) {
		column, err := externalTableFakeColumnOf(item)
		if err != nil {
			return nil, err
		}
		columns = append(columns, column)
	}
	return columns, nil
}

// partitionValues reads PARTITION ("key" = 'value', ...) and checks the keys against the table's partition keys.
func (table *externalTableFake) partitionValues(tokens []externalTableFakeToken) ([]string, []externalTableFakeToken, error) {
	after, ok := externalTableFakeExpect(tokens, "PARTITION")
	if !ok {
		return nil, nil, fmt.Errorf("expected PARTITION")
	}
	inside, rest, err := externalTableFakeGroup(after)
	if err != nil {
		return nil, nil, err
	}
	items := externalTableFakeItems(inside)
	if len(items) != len(table.partitionKeys) {
		return nil, nil, fmt.Errorf("partition spec does not match the partition keys")
	}
	values := make([]string, len(items))
	for i, item := range items {
		if len(item) != 3 || !strings.EqualFold(item[0].text, table.partitionKeys[i].name) || item[2].kind != 'l' {
			return nil, nil, fmt.Errorf("partition spec does not match the partition keys")
		}
		values[i] = item[2].text
	}
	return values, rest, nil
}

// partition returns the index of the partition with values, or -1.
func (table *externalTableFake) partition(values []string) int {
	return slices.IndexFunc(table.partitions, func(partition externalTableFakePartition) bool { return slices.Equal(partition.values, values) })
}

// alter applies the ALTER TABLE forms the provider renders for external tables.
func (f *externalTableFamily) alter(sql string) error {
	tokens, err := externalTableFakeTokens(strings.TrimPrefix(sql, "ALTER TABLE "))
	if err != nil {
		return err
	}
	name, rest := externalTableFakeRelation(tokens)
	table := f.tables[name]
	if table == nil {
		return fmt.Errorf("external table %q does not exist", name)
	}
	avro := table.inputFormat == externalTableFileFormats["AVRO"]
	if after, ok := externalTableFakeExpect(rest, "ADD", "COLUMN"); ok {
		if avro {
			return fmt.Errorf("cannot add columns to an AVRO table")
		}
		column, err := externalTableFakeColumnOf(after)
		if err != nil {
			return fmt.Errorf("cannot add column: %w", err)
		}
		table.columns = append(table.columns, column)
		return nil
	}
	if after, ok := externalTableFakeExpect(rest, "DROP", "COLUMN"); ok {
		index := slices.IndexFunc(table.columns, func(column externalTableFakeColumn) bool { return column.name == strings.ToLower(after[0].text) })
		if index < 0 || len(table.columns) == 1 || avro {
			return fmt.Errorf("cannot drop column %q", after[0].text)
		}
		table.columns = slices.Delete(table.columns, index, index+1)
		return nil
	}
	if after, ok := externalTableFakeExpect(rest, "SET", "LOCATION"); ok {
		table.location = after[0].text
		return nil
	}
	if after, ok := externalTableFakeExpect(rest, "SET", "FILE", "FORMAT"); ok {
		format, err := dataapi.OneOf(after[0].text, externalTableSetFileFormats...)
		if err != nil {
			return err
		}
		table.inputFormat = externalTableFileFormats[format]
		return nil
	}
	if after, ok := externalTableFakeExpect(rest, "SET", "TABLE", "PROPERTIES"); ok {
		inside, _, err := externalTableFakeGroup(after)
		if err != nil {
			return err
		}
		pairs, err := externalTableFakePairs(inside)
		if err != nil {
			return err
		}
		maps.Copy(table.parameters, pairs)
		return nil
	}
	if after, ok := externalTableFakeExpect(rest, "ADD"); ok {
		values, after, err := table.partitionValues(after)
		if err != nil {
			return err
		}
		if table.partition(values) >= 0 {
			return fmt.Errorf("partition %v already exists", values)
		}
		after, ok = externalTableFakeExpect(after, "LOCATION")
		if !ok || len(after) != 1 {
			return fmt.Errorf("malformed ADD PARTITION")
		}
		table.partitions = append(table.partitions, externalTableFakePartition{values: values, location: strings.TrimSuffix(after[0].text, "/")})
		return nil
	}
	if after, ok := externalTableFakeExpect(rest, "DROP"); ok {
		values, _, err := table.partitionValues(after)
		if err != nil {
			return err
		}
		index := table.partition(values)
		if index < 0 {
			return fmt.Errorf("partition %v does not exist", values)
		}
		table.partitions = slices.Delete(table.partitions, index, index+1)
		return nil
	}
	values, after, err := table.partitionValues(rest)
	if err != nil {
		return err
	}
	index := table.partition(values)
	after, ok := externalTableFakeExpect(after, "SET", "LOCATION")
	if index < 0 || !ok {
		return fmt.Errorf("cannot move partition %v", values)
	}
	table.partitions[index].location = strings.TrimSuffix(after[0].text, "/")
	return nil
}
