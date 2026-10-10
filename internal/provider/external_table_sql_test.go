package provider

import (
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// externalTableTestColumns builds a column list from name/type pairs.
func externalTableTestColumns(pairs ...string) types.List {
	var columns []externalTableColumnValue
	for i := 0; i+1 < len(pairs); i += 2 {
		columns = append(columns, externalTableColumnValue{Name: types.StringValue(pairs[i]), Type: types.StringValue(pairs[i+1])})
	}
	return externalTableColumnList(columns)
}

// externalTableTestMap builds a string map from key/value pairs.
func externalTableTestMap(pairs ...string) types.Map {
	elements := map[string]attr.Value{}
	for i := 0; i+1 < len(pairs); i += 2 {
		elements[pairs[i]] = types.StringValue(pairs[i+1])
	}
	return types.MapValueMust(types.StringType, elements)
}

// externalTableTestModel is the representative table the lifecycle cases and the fake catalog share.
func externalTableTestModel() externalTableModel {
	return externalTableModel{
		ID: types.StringNull(), Database: types.StringValue("admin"), Schema: types.StringValue("example_external"), Name: types.StringValue("events"),
		Columns:        externalTableTestColumns("id", "integer", "label", "varchar(64)"),
		PartitionKeys:  externalTableTestColumns("event_date", "date"),
		FieldDelimiter: types.StringValue(","), LineDelimiter: types.StringNull(), Serde: types.StringNull(), SerdeProperties: types.MapNull(types.StringType),
		StoredAs: types.StringValue("TEXTFILE"), InputFormat: types.StringNull(), OutputFormat: types.StringNull(),
		Location:        types.StringValue("s3://example-bucket/events/"),
		TableProperties: externalTableTestMap("skip.header.line.count", "1"),
	}
}

// TestExternalTableSQL pins every external table statement, including quoting of names, literals and properties.
func TestExternalTableSQL(t *testing.T) {
	plain := externalTableTestModel()
	unpartitioned := plain
	unpartitioned.PartitionKeys, unpartitioned.FieldDelimiter, unpartitioned.TableProperties = types.ListNull(externalTableColumnType), types.StringNull(), types.MapNull(types.StringType)
	unpartitioned.StoredAs = types.StringValue("parquet")
	delimited := plain
	delimited.FieldDelimiter, delimited.LineDelimiter = types.StringValue("\t"), types.StringValue("\n")
	serde := unpartitioned
	serde.StoredAs = types.StringValue("TEXTFILE")
	serde.Serde = types.StringValue("org.openx.data.jsonserde.JsonSerDe")
	serde.SerdeProperties = externalTableTestMap("strip.outer.array", "true", "case.insensitive", "false")
	inputFormat := unpartitioned
	inputFormat.StoredAs = types.StringNull()
	inputFormat.Serde = types.StringValue("org.apache.hadoop.hive.ql.io.parquet.serde.ParquetHiveSerDe")
	inputFormat.InputFormat = types.StringValue("org.apache.hudi.hadoop.HoodieParquetInputFormat")
	inputFormat.OutputFormat = types.StringValue("org.apache.hadoop.hive.ql.io.parquet.MapredParquetOutputFormat")
	aliases := unpartitioned
	aliases.Columns = externalTableTestColumns("a", "int2", "b", "int4", "c", "int8", "d", "numeric", "e", "decimal(8,2)", "f", "float4", "g", "float", "h", "bool", "i", "char", "j", "varchar", "k", "date", "l", "timestamp", "m", "varchar(max)")
	quoted := plain
	quoted.Schema, quoted.Name = types.StringValue(`Lake"Schema`), types.StringValue(`Odd"Table`)
	quoted.Columns = externalTableTestColumns(`Mixed"Case`, "integer")
	quoted.PartitionKeys = externalTableTestColumns(`Part"Key`, "varchar(10)")
	quoted.FieldDelimiter, quoted.LineDelimiter = types.StringValue(`'`), types.StringValue(`\`)
	quoted.Location = types.StringValue(`s3://bucket/it's \data/`)
	quoted.TableProperties = externalTableTestMap(`it's`, `C:\path`)
	quotedSerde := serde
	quotedSerde.Schema, quotedSerde.Name = quoted.Schema, quoted.Name
	quotedSerde.Serde = types.StringValue(`com.example.It'sSerDe\x`)
	quotedSerde.SerdeProperties = externalTableTestMap(`key's`, `value\'s`)
	invalid := func(change func(*externalTableModel)) externalTableModel {
		model := plain
		change(&model)
		return model
	}
	create := func(data externalTableModel) func() (string, error) {
		return func() (string, error) { return createExternalTableStatement(data) }
	}
	alter := func(prev, plan externalTableModel) func() ([]string, error) {
		return func() ([]string, error) { return alterExternalTableStatements(prev, plan) }
	}
	moved := plain
	moved.Location = types.StringValue("s3://example-bucket/events-v2/")
	parquet := plain
	parquet.StoredAs = types.StringValue("PARQUET")
	orc := plain
	orc.StoredAs = types.StringValue("ORC")
	recount := plain
	recount.TableProperties = externalTableTestMap("skip.header.line.count", "2", "numRows", "170000", "orc.schema.resolution", "position")
	compressed := plain
	compressed.TableProperties = externalTableTestMap("skip.header.line.count", "1", "compression_type", "gzip")
	removed := plain
	removed.TableProperties = types.MapNull(types.StringType)
	widened := plain
	widened.Columns = externalTableTestColumns("ID", "int4", "label", "varchar(64)", "amount", "decimal(8,2)", `New"Col`, "bigint")
	narrowed := plain
	narrowed.Columns = externalTableTestColumns("label", "varchar(64)", "note", "varchar")
	reordered := plain
	reordered.Columns = externalTableTestColumns("label", "varchar(64)", "id", "integer")
	retyped := plain
	retyped.Columns = externalTableTestColumns("id", "bigint", "label", "varchar(64)")
	avro := plain
	avro.StoredAs = types.StringValue("AVRO")
	avroWidened := widened
	avroWidened.StoredAs = types.StringValue("AVRO")
	quotedMoved := quoted
	quotedMoved.Location = types.StringValue(`s3://bucket/it's \moved/`)
	quotedMoved.TableProperties = externalTableTestMap(`it's`, `C:\path`, "numRows", `1'0\0`)
	quotedMoved.Columns = externalTableTestColumns(`Mixed"Case`, "integer", `Other"Col`, "date")
	checkSQL(t, "external_table", []sqlCase{
		{"create", create(plain)},
		{"create_parquet_unpartitioned", create(unpartitioned)},
		{"create_delimited_lines", create(delimited)},
		{"create_serde_properties", create(serde)},
		{"create_input_format", create(inputFormat)},
		{"create_type_aliases", create(aliases)},
		{"create_quoted", create(quoted)},
		{"create_quoted_serde", create(quotedSerde)},
		{"create_error_no_columns", create(invalid(func(m *externalTableModel) { m.Columns = externalTableTestColumns() }))},
		{"create_error_duplicate_column", create(invalid(func(m *externalTableModel) { m.Columns = externalTableTestColumns("id", "integer", "ID", "bigint") }))},
		{"create_error_partition_named_like_column", create(invalid(func(m *externalTableModel) { m.PartitionKeys = externalTableTestColumns("Label", "date") }))},
		{"create_error_pseudocolumn", create(invalid(func(m *externalTableModel) { m.Columns = externalTableTestColumns("$path", "varchar") }))},
		{"create_error_unsupported_type", create(invalid(func(m *externalTableModel) { m.Columns = externalTableTestColumns("id", "timestamptz") }))},
		{"create_error_invalid_type", create(invalid(func(m *externalTableModel) { m.Columns = externalTableTestColumns("id", "integer; DROP TABLE x") }))},
		{"create_error_delimiter", create(invalid(func(m *externalTableModel) { m.FieldDelimiter = types.StringValue("||") }))},
		{"create_error_non_ascii_delimiter", create(invalid(func(m *externalTableModel) { m.FieldDelimiter = types.StringValue("§") }))},
		{"create_error_serde_and_delimiter", create(invalid(func(m *externalTableModel) { m.Serde = types.StringValue("org.openx.data.jsonserde.JsonSerDe") }))},
		{"create_error_serde_properties_without_serde", create(invalid(func(m *externalTableModel) { m.SerdeProperties = externalTableTestMap("a", "b") }))},
		{"create_error_format", create(invalid(func(m *externalTableModel) { m.StoredAs = types.StringValue("JSON") }))},
		{"create_error_no_format", create(invalid(func(m *externalTableModel) { m.StoredAs = types.StringNull() }))},
		{"create_error_half_input_format", create(invalid(func(m *externalTableModel) {
			m.StoredAs, m.InputFormat = types.StringNull(), types.StringValue("org.apache.hadoop.mapred.TextInputFormat")
		}))},
		{"create_error_location", create(invalid(func(m *externalTableModel) { m.Location = types.StringValue("https://example.com/data/") }))},
		{"create_error_empty_property", create(invalid(func(m *externalTableModel) { m.TableProperties = externalTableTestMap("", "x") }))},
		{"alter_unchanged", alter(plain, plain)},
		{"alter_location", alter(plain, moved)},
		{"alter_file_format", alter(plain, parquet)},
		{"alter_file_format_case_only", alter(parquet, invalid(func(m *externalTableModel) { m.StoredAs = types.StringValue("parquet") }))},
		{"alter_table_properties", alter(plain, recount)},
		{"alter_add_columns", alter(plain, widened)},
		{"alter_drop_and_add_columns", alter(plain, narrowed)},
		{"alter_quoted", alter(quoted, quotedMoved)},
		{"alter_error_reorder_columns", alter(plain, reordered)},
		{"alter_error_retype_column", alter(plain, retyped)},
		{"alter_error_avro_columns", alter(avro, avroWidened)},
		{"alter_error_orc", alter(plain, orc)},
		{"alter_error_unsettable_property", alter(plain, compressed)},
		{"alter_error_removed_property", alter(plain, removed)},
		{"drop", func() string { return dropExternalTableStatement(plain) }},
		{"drop_quoted", func() string { return dropExternalTableStatement(quoted) }},
		{"read", func() (string, error) {
			sql, parameters, err := readExternalTableQuery(quoted).Build()
			assert.Equal(t, map[string]string{"database": "admin", "schema": `Lake"Schema`, "name": `Odd"Table`}, parameters)
			return sql, err
		}},
		{"read_columns", func() (string, error) {
			sql, parameters, err := readExternalTableColumnsQuery("admin", `Lake"Schema`, `Odd"Table`).Build()
			assert.Equal(t, map[string]string{"database": "admin", "schema": `Lake"Schema`, "table": `Odd"Table`}, parameters)
			return sql, err
		}},
	})
}

// TestExternalTableAlterCoverage keeps an alter step for every in-place attribute; partition keys stay in place
// only for equivalent spellings, which need no statement.
func TestExternalTableAlterCoverage(t *testing.T) {
	assertAlterCoverage(t, newExternalTableResource(), externalTableAlterSteps, "partition_keys")
}

// TestExternalTableTypes checks validation, canonical spelling and the Hive translation of catalog types.
func TestExternalTableTypes(t *testing.T) {
	for configured, expected := range map[string]string{
		"int": "integer", "INT4": "integer", "int2": "smallint", "bigint": "bigint", "numeric": "decimal(18,0)",
		"decimal(8, 2)": "decimal(8,2)", "float4": "real", "float": "double precision", "bool": "boolean",
		"char": "char(1)", "character varying": "varchar(256)", "varchar(max)": "varchar(65535)", "timestamp": "timestamp", "date": "date",
	} {
		parsed, err := parseExternalTableType(configured)
		require.NoError(t, err, configured)
		assert.Equal(t, expected, parsed.String(), configured)
	}
	for _, unsupported := range []string{"timestamptz", "super", "varbyte(10)", "geometry", "time", "string", "struct<a:int>"} {
		_, err := parseExternalTableType(unsupported)
		require.Error(t, err, unsupported)
	}
	for catalog, expected := range map[string]string{
		"int": "integer", "float": "real", "double": "double precision", "decimal(8,2)": "decimal(8,2)", "DECIMAL(8, 2)": "decimal(8,2)",
		"varchar(64)": "varchar(64)", "char(10)": "char(10)", "boolean": "boolean", "string": "string", "array<int>": "array<int>",
	} {
		assert.Equal(t, expected, externalTableCatalogType(catalog), catalog)
	}
	assert.True(t, externalTableTypesEqual("int4", "integer"))
	assert.True(t, externalTableTypesEqual("varchar", "varchar(256)"))
	assert.True(t, externalTableTypesEqual("String", "string"))
	assert.False(t, externalTableTypesEqual("float", "real"), "Redshift FLOAT is DOUBLE PRECISION")
	assert.False(t, externalTableTypesEqual("varchar(10)", "varchar(20)"))
}

// TestExternalTableLocationsMatch accepts the catalog's trailing-slash and truncation forms only.
func TestExternalTableLocationsMatch(t *testing.T) {
	long := "s3://bucket/partition/" + strings.Repeat("x", 200) + "/"
	assert.True(t, externalLocationsMatch("s3://bucket/data/", "s3://bucket/data"))
	assert.True(t, externalLocationsMatch("s3://bucket/data", "s3://bucket/data/"))
	assert.True(t, externalLocationsMatch(long, long[:128]))
	assert.False(t, externalLocationsMatch("s3://bucket/data/", "s3://bucket/other"))
	assert.False(t, externalLocationsMatch("s3://bucket/data/x", "s3://bucket/data"))
	require.Error(t, externalLocation("s3://"))
	require.Error(t, externalLocation("S3://bucket/"))
	require.NoError(t, externalLocation("s3://bucket/manifest.json"))
}

// TestExternalTableColumnChanges pins which column edits ALTER TABLE can make.
func TestExternalTableColumnChanges(t *testing.T) {
	columns := func(pairs ...string) []externalTableColumnValue {
		values, known := externalTableColumns(externalTableTestColumns(pairs...))
		require.True(t, known)
		return values
	}
	prev := columns("a", "integer", "b", "date", "c", "varchar(10)")
	added, dropped, ok := externalTableColumnChanges(prev, columns("A", "int", "c", "varchar(10)", "d", "bigint"), false)
	require.True(t, ok)
	assert.Equal(t, []string{"b"}, dropped)
	require.Len(t, added, 1)
	assert.Equal(t, "d", added[0].Name.ValueString())
	for name, plan := range map[string][]externalTableColumnValue{
		"reorder":         columns("b", "date", "a", "integer", "c", "varchar(10)"),
		"insert":          columns("a", "integer", "x", "date", "b", "date", "c", "varchar(10)"),
		"retype":          columns("a", "integer", "b", "timestamp", "c", "varchar(10)"),
		"empty":           nil,
		"widen":           columns("a", "integer", "b", "date", "c", "varchar(20)"),
		"drop_then_after": columns("x", "integer", "a", "integer"),
	} {
		_, _, ok := externalTableColumnChanges(prev, plan, false)
		assert.False(t, ok, name)
	}
	_, _, ok = externalTableColumnChanges(prev, columns("a", "int4", "b", "date", "c", "varchar(10)"), true)
	assert.True(t, ok, "AVRO tables accept spelling changes")
	_, _, ok = externalTableColumnChanges(prev, columns("a", "integer"), true)
	assert.False(t, ok, "AVRO tables cannot drop columns")
	assert.True(t, externalTableColumnsEquivalent(nil, columns()))
	assert.False(t, externalTableColumnsEquivalent(prev, columns("a", "integer")))
}

// TestExternalTableStoredAsInPlace follows the SET FILE FORMAT list.
func TestExternalTableStoredAsInPlace(t *testing.T) {
	assert.True(t, externalTableStoredAsInPlace("TEXTFILE", "parquet"))
	assert.True(t, externalTableStoredAsInPlace("ORC", "orc"))
	assert.True(t, externalTableStoredAsInPlace("", ""))
	assert.False(t, externalTableStoredAsInPlace("TEXTFILE", "ORC"))
	assert.False(t, externalTableStoredAsInPlace("", "PARQUET"))
	assert.False(t, externalTableStoredAsInPlace("PARQUET", ""))
	assert.Equal(t, []string{"AVRO", "PARQUET"}, stringsOf([]sqlclient.Keyword{"AVRO", "PARQUET"}))
}

// TestExternalTableDelimiters renders control characters in the documented octal form and decodes catalog escapes.
func TestExternalTableDelimiters(t *testing.T) {
	assert.Equal(t, `'\011'`, externalTableDelimiterFragment("\t").String())
	assert.Equal(t, `'\177'`, externalTableDelimiterFragment("\x7f").String())
	assert.Equal(t, `'|'`, externalTableDelimiterFragment("|").String())
	assert.Equal(t, `'\\'`, externalTableDelimiterFragment(`\`).String())
	for catalog, expected := range map[string]string{`\t`: "\t", `\n`: "\n", `\r`: "\r", `\001`: "\u0001", `\007`: "\a", ",": ",", `\x`: `\x`, `\999`: `\999`, "\t": "\t"} {
		assert.Equal(t, expected, externalTableCatalogDelimiter(catalog), catalog)
	}
}

// TestExternalTableUnknownAndIncompleteValues rejects unknown or empty parts that a plan could carry into Create.
func TestExternalTableUnknownAndIncompleteValues(t *testing.T) {
	unknownElement := types.ListValueMust(externalTableColumnType, []attr.Value{types.ObjectUnknown(externalTableColumnType.AttrTypes)})
	unknownName := types.ListValueMust(externalTableColumnType, []attr.Value{types.ObjectValueMust(externalTableColumnType.AttrTypes, map[string]attr.Value{"name": types.StringUnknown(), "type": types.StringValue("date")})})
	for name, change := range map[string]func(*externalTableModel){
		"unknown columns":    func(m *externalTableModel) { m.Columns = types.ListUnknown(externalTableColumnType) },
		"unknown element":    func(m *externalTableModel) { m.Columns = unknownElement },
		"unknown name":       func(m *externalTableModel) { m.PartitionKeys = unknownName },
		"empty column name":  func(m *externalTableModel) { m.Columns = externalTableTestColumns(" ", "date") },
		"unknown properties": func(m *externalTableModel) { m.TableProperties = types.MapUnknown(types.StringType) },
		"missing schema":     func(m *externalTableModel) { m.Schema = types.StringValue("") },
		"bad line delimiter": func(m *externalTableModel) { m.LineDelimiter = types.StringValue("\x00") },
	} {
		model := externalTableTestModel()
		change(&model)
		_, err := createExternalTableStatement(model)
		require.Error(t, err, name)
		_, err = alterExternalTableStatements(externalTableTestModel(), model)
		require.Error(t, err, name)
	}
	require.EqualError(t, externalTableDiagnosticsError(diag.Diagnostics{diag.NewErrorDiagnostic("summary", "detail")}), "summary: detail")
}

// TestExternalTableCatalogDecoding covers catalog JSON maps and column rows, including malformed values.
func TestExternalTableCatalogDecoding(t *testing.T) {
	values, err := externalTableCatalogMap("parameters", `{"numRows":"10","flag":true,"nested":{"a":1}}`)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"numRows": "10", "flag": "true", "nested": `{"a":1}`}, values)
	values, err = externalTableCatalogMap("parameters", " ")
	require.NoError(t, err)
	assert.Empty(t, values)
	_, err = externalTableCatalogMap("parameters", "{broken")
	require.ErrorContains(t, err, "decode external table parameters")
	columns, keys, err := externalTableCatalogColumns([]sqlclient.Row{
		{"columnname": "y", "external_type": "int", "columnnum": "3", "part_key": "2"},
		{"columnname": "b", "external_type": "double", "columnnum": "2", "part_key": "0"},
		{"columnname": "x", "external_type": "date", "columnnum": "4", "part_key": "1"},
		{"columnname": "a", "external_type": "string", "columnnum": "1", "part_key": "0"},
	})
	require.NoError(t, err)
	assert.Equal(t, []externalTableCatalogColumn{{"a", "string", 1}, {"b", "double precision", 2}}, columns)
	assert.Equal(t, []externalTableCatalogColumn{{"x", "date", 1}, {"y", "integer", 2}}, keys)
	for _, row := range []sqlclient.Row{
		{"columnname": "a", "external_type": "int", "columnnum": "x", "part_key": "0"},
		{"columnname": "a", "external_type": "int", "columnnum": "1", "part_key": ""},
		{"columnname": "", "external_type": "int", "columnnum": "1", "part_key": "0"},
	} {
		_, _, err := externalTableCatalogColumns([]sqlclient.Row{row})
		assert.Error(t, err)
	}
}
