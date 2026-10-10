package provider

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// externalTableFileFormats are the STORED AS formats of CREATE EXTERNAL TABLE, mapped to the input format class
// the Glue catalog records for them, which is how read recognizes the format again.
// https://docs.aws.amazon.com/redshift/latest/dg/r_CREATE_EXTERNAL_TABLE.html
var externalTableFileFormats = map[sqlclient.Keyword]string{
	"PARQUET":      "org.apache.hadoop.hive.ql.io.parquet.MapredParquetInputFormat",
	"RCFILE":       "org.apache.hadoop.hive.ql.io.RCFileInputFormat",
	"SEQUENCEFILE": "org.apache.hadoop.mapred.SequenceFileInputFormat",
	"TEXTFILE":     "org.apache.hadoop.mapred.TextInputFormat",
	"ORC":          "org.apache.hadoop.hive.ql.io.orc.OrcInputFormat",
	"AVRO":         "org.apache.hadoop.hive.ql.io.avro.AvroContainerInputFormat",
}

// externalTableImpliedSerdes are the SerDe classes the catalog records for a format without ROW FORMAT SERDE, so an
// imported table configured without serde does not report one.
var externalTableImpliedSerdes = map[sqlclient.Keyword]string{
	"PARQUET":      "org.apache.hadoop.hive.ql.io.parquet.serde.ParquetHiveSerDe",
	"RCFILE":       "org.apache.hadoop.hive.serde2.columnar.ColumnarSerDe",
	"SEQUENCEFILE": "org.apache.hadoop.hive.serde2.lazy.LazySimpleSerDe",
	"TEXTFILE":     "org.apache.hadoop.hive.serde2.lazy.LazySimpleSerDe",
	"ORC":          "org.apache.hadoop.hive.ql.io.orc.OrcSerde",
	"AVRO":         "org.apache.hadoop.hive.serde2.avro.AvroSerDe",
}

// externalTableSetFileFormats are the formats ALTER TABLE ... SET FILE FORMAT accepts; ORC and the INPUTFORMAT form
// can only be chosen at creation.
// https://docs.aws.amazon.com/redshift/latest/dg/r_ALTER_TABLE.html
var externalTableSetFileFormats = []sqlclient.Keyword{"AVRO", "PARQUET", "RCFILE", "SEQUENCEFILE", "TEXTFILE"}

// externalTableSettableProperties are the table properties the ALTER TABLE reference and its external table
// examples document for SET TABLE PROPERTIES; other properties can only be set by CREATE EXTERNAL TABLE.
var externalTableSettableProperties = []string{"numRows", "skip.header.line.count", "orc.schema.resolution"}

// externalTablePseudoColumns are reserved by Redshift Spectrum and cannot be declared.
var externalTablePseudoColumns = []string{"$path", "$size", "$spectrum_oid"}

// externalTableTypeNames maps the canonical Redshift spelling from sqlclient.ColumnType to the short name that
// CREATE EXTERNAL TABLE documents, for the data types it supports. VARBYTE is left out because its catalog spelling
// is undocumented, so a refresh could not recognize it.
var externalTableTypeNames = map[string]sqlclient.Keyword{
	"smallint":                    "smallint",
	"integer":                     "integer",
	"bigint":                      "bigint",
	"numeric":                     "decimal",
	"real":                        "real",
	"double precision":            "double precision",
	"boolean":                     "boolean",
	"character":                   "char",
	"character varying":           "varchar",
	"date":                        "date",
	"timestamp without time zone": "timestamp",
}

// externalTableHiveTypes maps Hive spellings that the external catalog reports to the Redshift names they stand for.
// FLOAT is a 4-byte REAL in Hive but DOUBLE PRECISION in Redshift, so it must be translated before parsing.
var externalTableHiveTypes = map[string]string{"int": "integer", "float": "real", "double": "double precision"}

// externalTableType is one validated column type.
type externalTableType struct {
	// name is the documented short type name.
	name sqlclient.Keyword
	// modifiers are the length, or the precision and scale, that Redshift applies.
	modifiers []int64
}

// parseExternalTableType validates a Redshift type supported by external tables. A missing modifier becomes the
// one Redshift applies, so varchar and varchar(256) compare equal.
func parseExternalTableType(value string) (externalTableType, error) {
	canonical, err := sqlclient.ColumnType(value)
	if err != nil {
		return externalTableType{}, err
	}
	base, arguments, _ := strings.Cut(strings.TrimSuffix(string(canonical), ")"), "(")
	name, ok := externalTableTypeNames[base]
	if !ok {
		return externalTableType{}, fmt.Errorf("data type %q is not supported by external tables", value)
	}
	parsed := externalTableType{name: name}
	if arguments != "" {
		for argument := range strings.SplitSeq(arguments, ",") {
			number, err := strconv.ParseInt(strings.TrimSpace(argument), 10, 64)
			if err != nil {
				return externalTableType{}, fmt.Errorf("data type %q has an invalid modifier: %w", value, err)
			}
			parsed.modifiers = append(parsed.modifiers, number)
		}
	}
	return parsed, nil
}

// String spells the type without spaces in its modifiers, the form stored in state for catalog-derived types.
func (t externalTableType) String() string {
	if len(t.modifiers) == 0 {
		return string(t.name)
	}
	parts := make([]string, len(t.modifiers))
	for i, modifier := range t.modifiers {
		parts[i] = strconv.FormatInt(modifier, 10)
	}
	return string(t.name) + "(" + strings.Join(parts, ",") + ")"
}

// fragment renders the type for DDL.
func (t externalTableType) fragment() sqlclient.Statement {
	statement := sqlclient.Kw(t.name)
	if len(t.modifiers) == 0 {
		return statement
	}
	arguments := make([]sqlclient.Statement, len(t.modifiers))
	for i, modifier := range t.modifiers {
		arguments[i] = sqlclient.Int(modifier)
	}
	return statement.Args(arguments...)
}

// externalTableCatalogType converts a catalog external_type to the Redshift spelling; types this provider cannot
// declare, such as Glue's string or nested types, keep the catalog spelling.
func externalTableCatalogType(external string) string {
	normalized := strings.ToLower(strings.Join(strings.Fields(external), " "))
	normalized = strings.ReplaceAll(strings.ReplaceAll(normalized, "( ", "("), ", ", ",")
	base, rest, _ := strings.Cut(normalized, "(")
	if mapped, ok := externalTableHiveTypes[base]; ok && rest == "" {
		normalized = mapped
	}
	if parsed, err := parseExternalTableType(normalized); err == nil {
		return parsed.String()
	}
	return external
}

// externalTableTypesEqual compares two type spellings by the type they declare.
func externalTableTypesEqual(a, b string) bool {
	left, leftErr := parseExternalTableType(a)
	right, rightErr := parseExternalTableType(b)
	if leftErr != nil || rightErr != nil {
		return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
	}
	return left.String() == right.String()
}

// externalLocationsMatch compares S3 locations as the catalog reports them: SVV_EXTERNAL_PARTITIONS shows folders
// without their trailing slash and truncates locations to 128 characters.
// https://docs.aws.amazon.com/redshift/latest/dg/r_SVV_EXTERNAL_PARTITIONS.html
func externalLocationsMatch(configured, catalog string) bool {
	if strings.TrimSuffix(configured, "/") == strings.TrimSuffix(catalog, "/") {
		return true
	}
	return len(catalog) >= 128 && strings.HasPrefix(configured, catalog)
}

// externalLocation checks that a location names an S3 folder or manifest file, as LOCATION requires.
func externalLocation(value string) error {
	if !strings.HasPrefix(value, "s3://") || len(value) == len("s3://") {
		return fmt.Errorf("location %q must be an s3:// folder or manifest file", value)
	}
	return nil
}

// externalTableColumn is one column or partition key.
type externalTableColumn struct {
	// name is the configured column name.
	name string
	// dataType is the validated type.
	dataType externalTableType
}

// externalTableSpec is a validated external table definition.
type externalTableSpec struct {
	// schema is the external schema.
	schema string
	// name is the table name.
	name string
	// columns are the data columns in order.
	columns []externalTableColumn
	// partitionKeys are the partition columns in order.
	partitionKeys []externalTableColumn
	// fieldDelimiter and lineDelimiter select ROW FORMAT DELIMITED; empty omits them.
	fieldDelimiter, lineDelimiter string
	// serde selects ROW FORMAT SERDE; empty omits it.
	serde string
	// serdeProperties are the WITH SERDEPROPERTIES pairs.
	serdeProperties map[string]string
	// storedAs is the named file format; empty selects the INPUTFORMAT form.
	storedAs sqlclient.Keyword
	// inputFormat and outputFormat are the explicit format classes.
	inputFormat, outputFormat string
	// location is the S3 folder or manifest file.
	location string
	// tableProperties are the TABLE PROPERTIES pairs.
	tableProperties map[string]string
}

// externalTableColumnValue is a column as Terraform holds it.
type externalTableColumnValue struct {
	// Name is the column name.
	Name types.String `tfsdk:"name"`
	// Type is the configured data type.
	Type types.String `tfsdk:"type"`
}

// externalTableColumnType is the element type of the column and partition_key blocks.
var externalTableColumnType = types.ObjectType{AttrTypes: map[string]attr.Type{"name": types.StringType, "type": types.StringType}}

// externalTableColumns returns the known elements of a column list; known is false for a null or unknown list or
// any unknown element.
func externalTableColumns(list types.List) (columns []externalTableColumnValue, known bool) {
	if list.IsNull() || list.IsUnknown() {
		return nil, false
	}
	for _, element := range list.Elements() {
		object, ok := element.(types.Object)
		if !ok || object.IsNull() || object.IsUnknown() {
			return nil, false
		}
		attributes := object.Attributes()
		name, _ := attributes["name"].(types.String)
		dataType, _ := attributes["type"].(types.String)
		if name.IsUnknown() || dataType.IsUnknown() {
			return nil, false
		}
		columns = append(columns, externalTableColumnValue{Name: name, Type: dataType})
	}
	return columns, true
}

// externalTableColumnList builds a column list value.
func externalTableColumnList(columns []externalTableColumnValue) types.List {
	elements := make([]attr.Value, len(columns))
	for i, column := range columns {
		elements[i] = types.ObjectValueMust(externalTableColumnType.AttrTypes, map[string]attr.Value{"name": column.Name, "type": column.Type})
	}
	return types.ListValueMust(externalTableColumnType, elements)
}

// parseExternalTableColumns validates the blocks of kind, rejecting duplicate names, which Redshift compares without
// case.
func parseExternalTableColumns(kind string, list types.List, seen map[string]string) ([]externalTableColumn, error) {
	values, known := externalTableColumns(list)
	if !known && !list.IsNull() {
		return nil, fmt.Errorf("%s blocks must be known", kind)
	}
	columns := make([]externalTableColumn, 0, len(values))
	for _, value := range values {
		name := value.Name.ValueString()
		if strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("%s blocks need nonempty names", kind)
		}
		if slices.Contains(externalTablePseudoColumns, strings.ToLower(name)) {
			return nil, fmt.Errorf("%s cannot use the pseudocolumn name %q", kind, name)
		}
		if previous, ok := seen[strings.ToLower(name)]; ok {
			return nil, fmt.Errorf("%s name %q repeats %s", kind, name, previous)
		}
		seen[strings.ToLower(name)] = fmt.Sprintf("%q", name)
		dataType, err := parseExternalTableType(value.Type.ValueString())
		if err != nil {
			return nil, fmt.Errorf("%s %q: %w", kind, name, err)
		}
		columns = append(columns, externalTableColumn{name: name, dataType: dataType})
	}
	return columns, nil
}

// externalTableDelimiter validates one ROW FORMAT DELIMITED character.
func externalTableDelimiter(kind, value string) error {
	if value == "" {
		return nil
	}
	if len(value) != 1 || value[0] == 0 || value[0] > 0x7f {
		return fmt.Errorf("%s must be a single ASCII character other than NUL; write control characters as HCL escapes such as \"\\t\" or \"\\u0007\"", kind)
	}
	return nil
}

// externalTableDelimiterFragment renders a validated delimiter. Non-printing characters use the octal '\ddd' form
// that CREATE EXTERNAL TABLE documents for them, instead of a raw control character inside the literal.
func externalTableDelimiterFragment(value string) sqlclient.Statement {
	if char := value[0]; char < 0x20 || char == 0x7f {
		//sql:trusted externalTableDelimiter admits one ASCII byte, so the escape is three octal digits.
		return sqlclient.Kw(sqlclient.Keyword(fmt.Sprintf(`'\%03o'`, char)))
	}
	return sqlclient.Lit(value)
}

// externalTableCatalogDelimiter decodes the catalog's field.delim or line.delim, which may hold the character itself
// or the escape it was written with, such as \t or \011.
func externalTableCatalogDelimiter(catalog string) string {
	switch catalog {
	case `\t`:
		return "\t"
	case `\n`:
		return "\n"
	case `\r`:
		return "\r"
	}
	if len(catalog) == 4 && catalog[0] == '\\' {
		if code, err := strconv.ParseUint(catalog[1:], 8, 7); err == nil {
			return string(rune(code))
		}
	}
	return catalog
}

// externalTableProperties validates a property map; property names are case-sensitive and must be nonempty.
func externalTableProperties(kind string, value types.Map) (map[string]string, error) {
	if value.IsUnknown() {
		return nil, fmt.Errorf("%s must be known", kind)
	}
	properties := knownMap(value)
	for key := range properties {
		if key == "" {
			return nil, fmt.Errorf("%s need nonempty names", kind)
		}
	}
	return properties, nil
}

// externalTableSpecFrom validates a model into a spec. input_format and output_format are used only without
// stored_as, because Read fills them from the catalog for named formats.
func externalTableSpecFrom(data externalTableModel) (externalTableSpec, error) {
	spec := externalTableSpec{schema: data.Schema.ValueString(), name: data.Name.ValueString(), location: data.Location.ValueString()}
	if spec.schema == "" || spec.name == "" {
		return spec, fmt.Errorf("external table requires a nonempty schema and name")
	}
	seen := map[string]string{}
	var err error
	if spec.columns, err = parseExternalTableColumns("column", data.Columns, seen); err != nil {
		return spec, err
	}
	if len(spec.columns) == 0 {
		return spec, fmt.Errorf("external table requires at least one column block")
	}
	// Partition keys share the name space with columns: CREATE EXTERNAL TABLE rejects a partition key named like a column.
	if spec.partitionKeys, err = parseExternalTableColumns("partition_key", data.PartitionKeys, seen); err != nil {
		return spec, err
	}
	spec.fieldDelimiter, spec.lineDelimiter, spec.serde = knownString(data.FieldDelimiter), knownString(data.LineDelimiter), knownString(data.Serde)
	if err := externalTableDelimiter("field_delimiter", spec.fieldDelimiter); err != nil {
		return spec, err
	}
	if err := externalTableDelimiter("line_delimiter", spec.lineDelimiter); err != nil {
		return spec, err
	}
	if spec.serde != "" && (spec.fieldDelimiter != "" || spec.lineDelimiter != "") {
		return spec, fmt.Errorf("serde conflicts with field_delimiter and line_delimiter: ROW FORMAT is either DELIMITED or SERDE")
	}
	if spec.serdeProperties, err = externalTableProperties("serde_properties", data.SerdeProperties); err != nil {
		return spec, err
	}
	if len(spec.serdeProperties) > 0 && spec.serde == "" {
		return spec, fmt.Errorf("serde_properties require serde")
	}
	if storedAs := knownString(data.StoredAs); storedAs != "" {
		if spec.storedAs, err = sqlclient.OneOf(storedAs, slices.Sorted(maps.Keys(externalTableFileFormats))...); err != nil {
			return spec, fmt.Errorf("stored_as: %w", err)
		}
	} else {
		spec.inputFormat, spec.outputFormat = knownString(data.InputFormat), knownString(data.OutputFormat)
		if spec.inputFormat == "" || spec.outputFormat == "" {
			return spec, fmt.Errorf("external table requires stored_as, or both input_format and output_format")
		}
	}
	if err := externalLocation(spec.location); err != nil {
		return spec, err
	}
	if spec.tableProperties, err = externalTableProperties("table_properties", data.TableProperties); err != nil {
		return spec, err
	}
	return spec, nil
}

// validateExternalTableConfig adds the checks that need configuration rather than plan values: Read fills
// input_format and output_format for named formats, so only configuration shows that both forms were chosen.
func validateExternalTableConfig(data externalTableModel) error {
	if knownString(data.StoredAs) != "" && (knownString(data.InputFormat) != "" || knownString(data.OutputFormat) != "") {
		return fmt.Errorf("stored_as conflicts with input_format and output_format")
	}
	_, err := externalTableSpecFrom(data)
	return err
}

// externalTableRelation renders the qualified table; Spectrum tables must be qualified by their external schema.
func externalTableRelation(schema, name string) sqlclient.Statement {
	return sqlclient.Fragment().Qualified(schema, name)
}

// externalTablePairs renders 'name' = 'value' items in name order, so SQL does not depend on map iteration.
func externalTablePairs(properties map[string]string) []sqlclient.Statement {
	var pairs []sqlclient.Statement
	for _, key := range slices.Sorted(maps.Keys(properties)) {
		pairs = append(pairs, sqlclient.Lit(key).Kw("=").Lit(properties[key]))
	}
	return pairs
}

// externalTableColumnItems renders "name" type items.
func externalTableColumnItems(columns []externalTableColumn) []sqlclient.Statement {
	items := make([]sqlclient.Statement, len(columns))
	for i, column := range columns {
		items[i] = sqlclient.Ident(column.name).Append(column.dataType.fragment())
	}
	return items
}

// createExternalTableStatement renders CREATE EXTERNAL TABLE in the clause order of the reference syntax.
// https://docs.aws.amazon.com/redshift/latest/dg/r_CREATE_EXTERNAL_TABLE.html
func createExternalTableStatement(data externalTableModel) (string, error) {
	spec, err := externalTableSpecFrom(data)
	if err != nil {
		return "", err
	}
	statement := sqlclient.Stmt("CREATE EXTERNAL TABLE").Append(externalTableRelation(spec.schema, spec.name)).
		Paren(externalTableColumnItems(spec.columns)...).
		When(len(spec.partitionKeys) > 0, func(s sqlclient.Statement) sqlclient.Statement {
			return s.Kw("PARTITIONED BY").Paren(externalTableColumnItems(spec.partitionKeys)...)
		}).
		When(spec.fieldDelimiter != "" || spec.lineDelimiter != "", func(s sqlclient.Statement) sqlclient.Statement {
			return s.Kw("ROW FORMAT DELIMITED").
				When(spec.fieldDelimiter != "", func(s sqlclient.Statement) sqlclient.Statement {
					return s.Kw("FIELDS TERMINATED BY").Append(externalTableDelimiterFragment(spec.fieldDelimiter))
				}).
				When(spec.lineDelimiter != "", func(s sqlclient.Statement) sqlclient.Statement {
					return s.Kw("LINES TERMINATED BY").Append(externalTableDelimiterFragment(spec.lineDelimiter))
				})
		}).
		When(spec.serde != "", func(s sqlclient.Statement) sqlclient.Statement {
			return s.KwLit("ROW FORMAT SERDE", spec.serde).When(len(spec.serdeProperties) > 0, func(s sqlclient.Statement) sqlclient.Statement {
				return s.Kw("WITH SERDEPROPERTIES").Paren(externalTablePairs(spec.serdeProperties)...)
			})
		}).
		Kw("STORED AS").OptKw(spec.storedAs).
		When(spec.storedAs == "", func(s sqlclient.Statement) sqlclient.Statement {
			return s.KwLit("INPUTFORMAT", spec.inputFormat).KwLit("OUTPUTFORMAT", spec.outputFormat)
		}).
		KwLit("LOCATION", spec.location).
		When(len(spec.tableProperties) > 0, func(s sqlclient.Statement) sqlclient.Statement {
			return s.Kw("TABLE PROPERTIES").Paren(externalTablePairs(spec.tableProperties)...)
		})
	return statement.String(), statement.Err()
}

// externalTableAlter starts every ALTER TABLE statement for the table.
func externalTableAlter(data externalTableModel) sqlclient.Statement {
	return sqlclient.Stmt("ALTER TABLE").Append(externalTableRelation(data.Schema.ValueString(), data.Name.ValueString()))
}

// externalTableAvro reports the AVRO format, for which ALTER TABLE can neither add nor drop columns.
func externalTableAvro(storedAs, inputFormat types.String) bool {
	return strings.EqualFold(knownString(storedAs), "AVRO") || strings.Contains(strings.ToLower(knownString(inputFormat)), "avro")
}

// externalTableMapsByName reports whether Spectrum matches the table's columns to the file's columns by name, so
// the catalog's column order carries no meaning. AWS documents name mapping only for ORC, where it is the default
// unless orc.schema.resolution is set; any value but name maps by position, so another spelling such as NAME counts
// as position, which never skips a replacement. Every other format maps by position.
// https://docs.aws.amazon.com/redshift/latest/dg/r_CREATE_EXTERNAL_TABLE.html
func externalTableMapsByName(spec externalTableSpec) bool {
	orc := spec.storedAs == "ORC" || spec.storedAs == "" && spec.inputFormat == externalTableFileFormats["ORC"]
	resolution, set := spec.tableProperties["orc.schema.resolution"]
	return orc && (!set || resolution == "name")
}

// externalTableMappingSpec holds what externalTableMapsByName needs from a model, which may not validate as a whole
// while planning. Configured table properties override catalog, the properties the catalog last reported, so a key
// configuration leaves out still decides as it does for Spectrum.
func externalTableMappingSpec(data externalTableModel, catalog map[string]string) externalTableSpec {
	properties := maps.Clone(catalog)
	if properties == nil {
		properties = map[string]string{}
	}
	maps.Copy(properties, knownMap(data.TableProperties))
	// A resolution known only after apply may be anything, so it counts as position, the answer that never skips a
	// replacement; knownMap leaves it out, which would otherwise read as the name default.
	if resolution, ok := data.TableProperties.Elements()["orc.schema.resolution"]; data.TableProperties.IsUnknown() || ok && resolution.IsUnknown() {
		properties["orc.schema.resolution"] = "position"
	}
	// An unrecognized or unknown format leaves storedAs empty and maps by position, for the same reason.
	storedAs, _ := sqlclient.OneOf(knownString(data.StoredAs), slices.Sorted(maps.Keys(externalTableFileFormats))...)
	return externalTableSpec{storedAs: storedAs, inputFormat: knownString(data.InputFormat), tableProperties: properties}
}

// externalTableLayout is what judging a column change needs from the catalog but state does not hold: the table
// properties, which configuration may leave unmanaged, and the physical column order, which state replaces with the
// configured order for a table that maps columns by name. Its exported fields are its private state encoding.
type externalTableLayout struct {
	// Parameters are the catalog's table properties.
	Parameters map[string]string `json:"parameters"`
	// Columns are the data column names in catalog order.
	Columns []string `json:"columns"`
}

// parameters returns the catalog's table properties; a missing layout has none.
func (l *externalTableLayout) parameters() map[string]string {
	if l == nil {
		return nil
	}
	return l.Parameters
}

// physical returns columns in the layout's catalog order. It reports false without a layout or when the names
// differ, as after an out-of-band change no refresh has seen, because the order is then unknown.
func (l *externalTableLayout) physical(columns []externalTableColumnValue) ([]externalTableColumnValue, bool) {
	if l == nil || len(l.Columns) != len(columns) {
		return nil, false
	}
	ordered := make([]externalTableColumnValue, 0, len(columns))
	for _, name := range l.Columns {
		i := slices.IndexFunc(columns, func(column externalTableColumnValue) bool { return strings.EqualFold(column.Name.ValueString(), name) })
		if i < 0 {
			return nil, false
		}
		ordered = append(ordered, columns[i])
	}
	return ordered, true
}

// externalTableColumnsAlterable reports whether ALTER TABLE can bring the columns from prev to plan. ADD COLUMN
// appends to the catalog order, and a table that maps by position after the change matches that order to the files,
// so the check starts from the catalog order. That holds even when the blocks do not change, because switching
// orc.schema.resolution to position makes the order of a name-mapped table matter. State holds the catalog order
// only for a table that maps by position, so without a layout a switch away from name mapping is refused.
func externalTableColumnsAlterable(prev, plan externalTableModel, layout *externalTableLayout) bool {
	before, beforeKnown := externalTableColumns(prev.Columns)
	after, afterKnown := externalTableColumns(plan.Columns)
	if !beforeKnown || !afterKnown {
		return false
	}
	byName := externalTableMapsByName(externalTableMappingSpec(plan, layout.parameters()))
	physical, ok := layout.physical(before)
	if !ok {
		if !byName && externalTableMapsByName(externalTableMappingSpec(prev, layout.parameters())) {
			return false
		}
		physical = before
	}
	avro := externalTableAvro(prev.StoredAs, prev.InputFormat) || externalTableAvro(plan.StoredAs, plan.InputFormat)
	return externalTableColumnsInPlace(physical, after, byName, avro)
}

// externalTableColumnIndex maps lowercase column names to their position, because Redshift compares them without case.
func externalTableColumnIndex(columns []externalTableColumnValue) map[string]int {
	index := map[string]int{}
	for i, column := range columns {
		index[strings.ToLower(column.Name.ValueString())] = i
	}
	return index
}

// externalTableColumnDiff returns the columns to add, in plan order, and the columns to drop, and whether a column in
// both declares another type. The statements do not depend on how the table maps columns, because ADD COLUMN always
// appends; externalTableColumnsInPlace decides whether that is what the plan asks for.
func externalTableColumnDiff(prev, plan []externalTableColumnValue) (added []externalTableColumnValue, dropped []string, retyped bool) {
	index := externalTableColumnIndex(prev)
	kept := map[int]bool{}
	for _, column := range plan {
		position, found := index[strings.ToLower(column.Name.ValueString())]
		if !found {
			added = append(added, column)
			continue
		}
		kept[position] = true
		retyped = retyped || !externalTableTypesEqual(prev[position].Type.ValueString(), column.Type.ValueString())
	}
	for i, column := range prev {
		if !kept[i] {
			dropped = append(dropped, column.Name.ValueString())
		}
	}
	return added, dropped, retyped
}

// externalTableColumnsInPlace reports whether ALTER TABLE can move prev, in catalog order, to plan. Columns keep
// their type, and AVRO tables cannot add or drop columns at all. A table that maps columns by name accepts additions
// and drops anywhere and any order; a positional table must keep its remaining columns in order ahead of the
// appended ones, because the catalog order is what Spectrum matches to the file.
// https://docs.aws.amazon.com/redshift/latest/dg/r_ALTER_TABLE.html
func externalTableColumnsInPlace(prev, plan []externalTableColumnValue, byName, avro bool) bool {
	added, dropped, retyped := externalTableColumnDiff(prev, plan)
	if len(plan) == 0 || retyped || avro && (len(added) > 0 || len(dropped) > 0) {
		return false
	}
	if byName {
		return true
	}
	index, last := externalTableColumnIndex(prev), -1
	for _, column := range plan[:len(plan)-len(added)] {
		position, found := index[strings.ToLower(column.Name.ValueString())]
		if !found || position < last {
			return false
		}
		last = position
	}
	return true
}

// externalTableColumnsEquivalent reports lists that differ at most in name case and type spelling.
func externalTableColumnsEquivalent(prev, plan []externalTableColumnValue) bool {
	return slices.EqualFunc(prev, plan, func(a, b externalTableColumnValue) bool {
		return strings.EqualFold(a.Name.ValueString(), b.Name.ValueString()) && externalTableTypesEqual(a.Type.ValueString(), b.Type.ValueString())
	})
}

// externalTableStoredAsInPlace reports whether SET FILE FORMAT can move prev to plan.
func externalTableStoredAsInPlace(prev, plan string) bool {
	if strings.EqualFold(prev, plan) {
		return true
	}
	settable := func(format string) bool {
		_, err := sqlclient.OneOf(format, externalTableSetFileFormats...)
		return err == nil
	}
	return prev != "" && plan != "" && settable(prev) && settable(plan)
}

// externalTablePropertyChanges returns the properties to set to move prev to plan, and an error naming a
// property that SET TABLE PROPERTIES cannot change or remove.
func externalTablePropertyChanges(prev, plan map[string]string) (map[string]string, error) {
	for key := range prev {
		if _, ok := plan[key]; !ok {
			return nil, fmt.Errorf("table property %q cannot be removed in place", key)
		}
	}
	changes := map[string]string{}
	for key, value := range plan {
		if previous, ok := prev[key]; ok && previous == value {
			continue
		}
		if !slices.Contains(externalTableSettableProperties, key) {
			return nil, fmt.Errorf("table property %q can only be set when the table is created; ALTER TABLE ... SET TABLE PROPERTIES supports %s", key, strings.Join(externalTableSettableProperties, ", "))
		}
		changes[key] = value
	}
	return changes, nil
}

// externalTableAlterSteps change one aspect of the table per step; alterExternalTableStatements validates the plan
// first, so the renderers can rely on it. partition_key has no step: it stays in place only for spellings that
// declare the same keys, which needs no statement.
var externalTableAlterSteps = []alterStep[externalTableModel]{
	{
		attribute: "column",
		value:     func(data externalTableModel) attr.Value { return data.Columns },
		render: func(prev, plan externalTableModel) []string {
			before, _ := externalTableColumns(prev.Columns)
			after, _ := externalTableColumns(plan.Columns)
			added, dropped, _ := externalTableColumnDiff(before, after)
			var statements []string
			// Adding first keeps at least one column, because a table cannot lose its last one.
			for _, column := range added {
				dataType, _ := parseExternalTableType(column.Type.ValueString())
				statements = append(statements, externalTableAlter(plan).Kw("ADD COLUMN").Ident(column.Name.ValueString()).Append(dataType.fragment()).String())
			}
			for _, name := range dropped {
				statements = append(statements, externalTableAlter(plan).Kw("DROP COLUMN").Ident(name).String())
			}
			return statements
		},
	},
	{
		attribute: "stored_as",
		value:     func(data externalTableModel) attr.Value { return data.StoredAs },
		render: func(prev, plan externalTableModel) []string {
			format, _ := sqlclient.OneOf(plan.StoredAs.ValueString(), externalTableSetFileFormats...)
			if strings.EqualFold(prev.StoredAs.ValueString(), plan.StoredAs.ValueString()) {
				return nil
			}
			return []string{externalTableAlter(plan).Kw("SET FILE FORMAT", format).String()}
		},
	},
	{
		attribute: "location",
		value:     func(data externalTableModel) attr.Value { return data.Location },
		render: func(_, plan externalTableModel) []string {
			return []string{externalTableAlter(plan).KwLit("SET LOCATION", plan.Location.ValueString()).String()}
		},
	},
	{
		attribute: "table_properties",
		value:     func(data externalTableModel) attr.Value { return data.TableProperties },
		render: func(prev, plan externalTableModel) []string {
			changes, _ := externalTablePropertyChanges(knownMap(prev.TableProperties), knownMap(plan.TableProperties))
			var statements []string
			for _, pair := range externalTablePairs(changes) {
				statements = append(statements, externalTableAlter(plan).Kw("SET TABLE PROPERTIES").Paren(pair).String())
			}
			return statements
		},
	},
}

// alterExternalTableStatements renders the in-place changes from prev to plan, given the catalog layout read just
// before, or nil when it is unknown. It rejects changes that the plan modifiers would have turned into a
// replacement, as a guard for prior state the modifiers could not judge, such as a catalog changed since planning.
func alterExternalTableStatements(prev, plan externalTableModel, layout *externalTableLayout) ([]string, error) {
	if _, err := externalTableSpecFrom(plan); err != nil {
		return nil, err
	}
	if !externalTableColumnsAlterable(prev, plan, layout) {
		return nil, fmt.Errorf("column blocks change in place only by adding or dropping columns, at the end unless an ORC table maps columns by name, and never retype a column or change an AVRO table's columns; replace the table")
	}
	if !prev.StoredAs.Equal(plan.StoredAs) && !externalTableStoredAsInPlace(knownString(prev.StoredAs), knownString(plan.StoredAs)) {
		return nil, fmt.Errorf("stored_as can change in place only between %s; replace the table", strings.Join(stringsOf(externalTableSetFileFormats), ", "))
	}
	if _, err := externalTablePropertyChanges(knownMap(prev.TableProperties), knownMap(plan.TableProperties)); err != nil {
		return nil, fmt.Errorf("%w; replace the table", err)
	}
	return alterStatements(prev, plan, externalTableAlterSteps), nil
}

// stringsOf converts keywords for messages.
func stringsOf(keywords []sqlclient.Keyword) []string {
	values := make([]string, len(keywords))
	for i, keyword := range keywords {
		values[i] = string(keyword)
	}
	return values
}

// dropExternalTableStatement renders DROP TABLE without CASCADE. Views over external tables must be late-binding
// (WITH NO SCHEMA BINDING), so they never block the drop; they fail at query time until the table exists again.
// Dropping removes only catalog metadata; the S3 data stays.
// https://docs.aws.amazon.com/redshift/latest/dg/r_CREATE_EXTERNAL_TABLE.html
func dropExternalTableStatement(data externalTableModel) string {
	return sqlclient.Stmt("DROP TABLE").Append(externalTableRelation(data.Schema.ValueString(), data.Name.ValueString())).String()
}

// readExternalTableQuery reads one external table. Redshift folds ASCII letters of quoted identifiers to lowercase
// too, so schema and table names are compared without case; redshift_database_name excludes tables of other
// databases, which the view also lists.
// https://docs.aws.amazon.com/redshift/latest/dg/r_names.html
// https://docs.aws.amazon.com/redshift/latest/dg/r_SVV_EXTERNAL_TABLES.html
func readExternalTableQuery(data externalTableModel) sqlclient.Query {
	return sqlclient.Select("schemaname", "tablename", "location", "input_format", "output_format", "serialization_lib", "serde_parameters", "parameters").
		From("svv_external_tables").
		Where("redshift_database_name = :database", sqlclient.Bind("database", data.Database.ValueString())).
		Where("LOWER(schemaname) = LOWER(:schema)", sqlclient.Bind("schema", data.Schema.ValueString())).
		Where("LOWER(tablename) = LOWER(:name)", sqlclient.Bind("name", data.Name.ValueString()))
}

// readExternalTableColumnsQuery reads the columns and partition keys of one external table, matching names as
// readExternalTableQuery does.
// https://docs.aws.amazon.com/redshift/latest/dg/r_SVV_EXTERNAL_COLUMNS.html
func readExternalTableColumnsQuery(database, schema, table string) sqlclient.Query {
	return sqlclient.Select("columnname", "external_type", "columnnum", "part_key").
		From("svv_external_columns").
		Where("redshift_database_name = :database", sqlclient.Bind("database", database)).
		Where("LOWER(schemaname) = LOWER(:schema)", sqlclient.Bind("schema", schema)).
		Where("LOWER(tablename) = LOWER(:table)", sqlclient.Bind("table", table)).
		OrderBy("columnnum")
}

// externalTableCatalogColumns splits catalog column rows into data columns in column order and partition keys in
// partition-key order.
func externalTableCatalogColumns(rows []sqlclient.Row) (columns, partitionKeys []externalTableCatalogColumn, err error) {
	for _, row := range rows {
		number, numberErr := strconv.Atoi(row["columnnum"])
		key, keyErr := strconv.Atoi(row["part_key"])
		if numberErr != nil || keyErr != nil || row["columnname"] == "" {
			return nil, nil, fmt.Errorf("external table column %q has incomplete catalog metadata", row["columnname"])
		}
		column := externalTableCatalogColumn{name: row["columnname"], dataType: externalTableCatalogType(row["external_type"]), order: number}
		if key > 0 {
			column.order = key
			partitionKeys = append(partitionKeys, column)
		} else {
			columns = append(columns, column)
		}
	}
	byOrder := func(a, b externalTableCatalogColumn) int { return a.order - b.order }
	slices.SortStableFunc(columns, byOrder)
	slices.SortStableFunc(partitionKeys, byOrder)
	return columns, partitionKeys, nil
}

// externalTableCatalogColumn is one column as the catalog reports it.
type externalTableCatalogColumn struct {
	// name is the catalog column name, usually lowercase.
	name string
	// dataType is the Redshift spelling of the catalog type.
	dataType string
	// order is the column number, or the partition-key position.
	order int
}

// externalTableCatalogMap decodes a catalog JSON object such as serde_parameters or parameters; an empty text is
// an empty map, and non-string values keep their JSON spelling.
func externalTableCatalogMap(kind, text string) (map[string]string, error) {
	values := map[string]string{}
	if strings.TrimSpace(text) == "" {
		return values, nil
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		return nil, fmt.Errorf("decode external table %s: %w", kind, err)
	}
	for key, raw := range decoded {
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			value = string(raw)
		}
		values[key] = value
	}
	return values, nil
}
