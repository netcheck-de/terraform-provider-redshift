package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// externalTableResource manages a Redshift Spectrum external table in an external schema.
type externalTableResource struct {
	// resourceClient provides SQL execution and warehouse ownership checks.
	resourceClient
}

// externalTableModel is the Terraform state of one external table.
type externalTableModel struct {
	// ID records the warehouse/database/schema/table identity.
	ID types.String `tfsdk:"id"`
	// Database is the local database holding the external schema.
	Database types.String `tfsdk:"database"`
	// Schema is the external schema that maps the external catalog database.
	Schema types.String `tfsdk:"schema"`
	// Name identifies the table.
	Name types.String `tfsdk:"name"`
	// Columns are the ordered data columns.
	Columns types.List `tfsdk:"columns"`
	// PartitionKeys are the ordered partition columns.
	PartitionKeys types.List `tfsdk:"partition_keys"`
	// FieldDelimiter is the ROW FORMAT DELIMITED field terminator.
	FieldDelimiter types.String `tfsdk:"field_delimiter"`
	// LineDelimiter is the ROW FORMAT DELIMITED line terminator.
	LineDelimiter types.String `tfsdk:"line_delimiter"`
	// Serde is the ROW FORMAT SERDE class.
	Serde types.String `tfsdk:"serde"`
	// SerdeProperties are the WITH SERDEPROPERTIES pairs.
	SerdeProperties types.Map `tfsdk:"serde_properties"`
	// StoredAs is the named file format.
	StoredAs types.String `tfsdk:"stored_as"`
	// InputFormat is the input format class, configured or observed.
	InputFormat types.String `tfsdk:"input_format"`
	// OutputFormat is the output format class, configured or observed.
	OutputFormat types.String `tfsdk:"output_format"`
	// Location is the S3 folder or manifest file.
	Location types.String `tfsdk:"location"`
	// TableProperties are the managed table properties.
	TableProperties types.Map `tfsdk:"table_properties"`
}

var _ = registerResource(newExternalTableResource)

// newExternalTableResource constructs an external table lifecycle handler.
func newExternalTableResource() resource.Resource { return &externalTableResource{} }

// Metadata identifies the external table resource to Terraform.
func (r *externalTableResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_external_table"
}

// externalTableColumnsAttribute defines columns and partition_keys, which differ only in how changes plan.
func externalTableColumnsAttribute(required bool, description string, modifier planmodifier.List) schema.ListNestedAttribute {
	return schema.ListNestedAttribute{
		Required: required, Optional: !required, MarkdownDescription: description,
		PlanModifiers: []planmodifier.List{modifier},
		NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{Required: true, MarkdownDescription: "Column name. The external catalog stores names in lowercase, so names differing only in case are the same column."},
			"type": schema.StringAttribute{Required: true, MarkdownDescription: "Data type: `smallint`, `integer`, `bigint`, `decimal(p,s)`, `real`, `double precision`, `boolean`, `char(n)`, `varchar(n)`, `date`, or `timestamp`, including their aliases such as `int4` or `numeric`. Spellings of the same type, such as `int` and `integer`, are equivalent."},
		}},
	}
}

// externalTableColumnsReplace replaces the table unless the column change is an append or a drop that ALTER TABLE
// supports for the table's format.
func externalTableColumnsReplace(ctx context.Context, req planmodifier.ListRequest, resp *listplanmodifier.RequiresReplaceIfFuncResponse) {
	before, beforeKnown := externalTableColumns(req.StateValue)
	after, afterKnown := externalTableColumns(req.PlanValue)
	if !beforeKnown || !afterKnown {
		resp.RequiresReplace = true
		return
	}
	var priorFormat, planFormat, priorInput, planInput types.String
	// Missing siblings leave the values null; diagnostics are irrelevant to a replacement decision.
	_ = req.State.GetAttribute(ctx, path.Root("stored_as"), &priorFormat)
	_ = req.Plan.GetAttribute(ctx, path.Root("stored_as"), &planFormat)
	_ = req.State.GetAttribute(ctx, path.Root("input_format"), &priorInput)
	_ = req.Plan.GetAttribute(ctx, path.Root("input_format"), &planInput)
	avro := externalTableAvro(priorFormat, priorInput) || externalTableAvro(planFormat, planInput)
	_, _, inPlace := externalTableColumnChanges(before, after, avro)
	resp.RequiresReplace = !inPlace
}

// externalTablePartitionKeysReplace replaces the table unless the keys differ only in spelling; partition keys
// cannot be altered.
func externalTablePartitionKeysReplace(_ context.Context, req planmodifier.ListRequest, resp *listplanmodifier.RequiresReplaceIfFuncResponse) {
	if req.PlanValue.IsUnknown() {
		resp.RequiresReplace = true
		return
	}
	// A null list and an empty list both declare an unpartitioned table.
	before, _ := externalTableColumns(req.StateValue)
	after, _ := externalTableColumns(req.PlanValue)
	resp.RequiresReplace = !externalTableColumnsEquivalent(before, after)
}

// externalTableStoredAsReplace replaces the table unless SET FILE FORMAT can switch between the formats.
func externalTableStoredAsReplace(_ context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
	resp.RequiresReplace = req.PlanValue.IsUnknown() || !externalTableStoredAsInPlace(knownString(req.StateValue), knownString(req.PlanValue))
}

// externalTableUnmanagedPropertiesKey names the private state that holds the catalog's table properties while an
// imported table's table_properties is still null. Only import leaves them unmanaged; a table created without
// properties has none, so the marker is what tells the two null states apart.
const externalTableUnmanagedPropertiesKey = "unmanaged_table_properties"

// externalTablePrivateState is the framework's private state, whose type is internal to the framework.
type externalTablePrivateState interface {
	GetKey(ctx context.Context, key string) ([]byte, diag.Diagnostics)
	SetKey(ctx context.Context, key string, value []byte) diag.Diagnostics
}

// externalTableUnmanagedProperties returns the catalog's properties recorded after import, and whether a marker
// exists. A nil private state, as in direct unit calls, has no marker.
func externalTableUnmanagedProperties(ctx context.Context, private externalTablePrivateState) (map[string]string, bool) {
	raw, diagnostics := private.GetKey(ctx, externalTableUnmanagedPropertiesKey)
	properties := map[string]string{}
	if diagnostics.HasError() || len(raw) == 0 || json.Unmarshal(raw, &properties) != nil {
		return nil, false
	}
	return properties, true
}

// recordExternalTableUnmanagedProperties keeps the marker current while properties stay unmanaged and removes it
// once table_properties is configured.
func recordExternalTableUnmanagedProperties(ctx context.Context, private externalTablePrivateState, unmanaged bool, catalog map[string]string) diag.Diagnostics {
	var raw []byte
	if unmanaged {
		raw, _ = json.Marshal(catalog) // String maps are always JSON-serializable.
	}
	return private.SetKey(ctx, externalTableUnmanagedPropertiesKey, raw)
}

// externalTablePriorProperties is what the planned properties change from. A null state means no properties for a
// created table, and the catalog's values of the configured keys for an imported one, so properties the catalog
// does not hold count as additions; unconfigured catalog properties are not removals.
func externalTablePriorProperties(state, plan types.Map, catalog map[string]string) map[string]string {
	if !state.IsNull() {
		return knownMap(state)
	}
	prior := map[string]string{}
	for key := range knownMap(plan) {
		if value, ok := catalog[key]; ok {
			prior[key] = value
		}
	}
	return prior
}

// externalTablePropertiesReplace replaces the table when a property is removed or one that SET TABLE PROPERTIES
// cannot change is added or changed, so the plan shows the replacement the apply would otherwise fail on.
func externalTablePropertiesReplace(ctx context.Context, req planmodifier.MapRequest, resp *mapplanmodifier.RequiresReplaceIfFuncResponse) {
	if req.PlanValue.IsUnknown() {
		resp.RequiresReplace = true
		return
	}
	catalog, _ := externalTableUnmanagedProperties(ctx, req.Private)
	_, err := externalTablePropertyChanges(externalTablePriorProperties(req.StateValue, req.PlanValue, catalog), knownMap(req.PlanValue))
	resp.RequiresReplace = err != nil
}

// Schema defines the external table definition and the in-place changes ALTER TABLE supports.
func (r *externalTableResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages one Redshift Spectrum external table, created with `CREATE EXTERNAL TABLE` in an external schema. Partitions are managed separately with `redshift_external_partition`.",
		Attributes: map[string]schema.Attribute{
			"id":       idAttribute(),
			"database": schema.StringAttribute{Required: true, PlanModifiers: replace, MarkdownDescription: "Local Redshift database holding the external schema. Changing it replaces the table."},
			"schema":   schema.StringAttribute{Required: true, PlanModifiers: replace, MarkdownDescription: "External schema that maps the external catalog database, for example a `redshift_external_schema`. Changing it replaces the table."},
			"name":     schema.StringAttribute{Required: true, PlanModifiers: replace, MarkdownDescription: "Table name; the external catalog stores it in lowercase. Changing it replaces the table."},
			"columns": externalTableColumnsAttribute(true, "Ordered data columns. Appending columns and dropping columns change the table in place with `ALTER TABLE ... ADD COLUMN` and `DROP COLUMN`, except for AVRO tables; reordering, retyping, or inserting columns replaces the table.",
				listplanmodifier.RequiresReplaceIf(externalTableColumnsReplace, "Changing columns other than by appending or dropping replaces the table.", "Changing columns other than by appending or dropping replaces the table.")),
			"partition_keys": externalTableColumnsAttribute(false, "Ordered `PARTITIONED BY` columns; their names must differ from the data columns. Spelling a name in another case or a type with an alias stays in place; any other change replaces the table.",
				listplanmodifier.RequiresReplaceIf(externalTablePartitionKeysReplace, "Changing partition keys replaces the table.", "Changing partition keys replaces the table.")),
			"field_delimiter": schema.StringAttribute{Optional: true, PlanModifiers: replace, MarkdownDescription: "`ROW FORMAT DELIMITED FIELDS TERMINATED BY` character: one ASCII character; write control characters as HCL escapes such as `\"\\t\"` or `\"\\u0007\"`. Conflicts with `serde`. Changing it replaces the table."},
			"line_delimiter":  schema.StringAttribute{Optional: true, PlanModifiers: replace, MarkdownDescription: "`ROW FORMAT DELIMITED LINES TERMINATED BY` character, usually `\"\\n\"`. Conflicts with `serde`. Changing it replaces the table."},
			"serde":           schema.StringAttribute{Optional: true, PlanModifiers: replace, MarkdownDescription: "`ROW FORMAT SERDE` class, such as `org.openx.data.jsonserde.JsonSerDe` or `org.apache.hadoop.hive.serde2.OpenCSVSerde`. Conflicts with the delimiters. Changing it replaces the table."},
			"serde_properties": schema.MapAttribute{
				ElementType: types.StringType, Optional: true, PlanModifiers: []planmodifier.Map{mapplanmodifier.RequiresReplace()},
				MarkdownDescription: "`WITH SERDEPROPERTIES` pairs for `serde`, such as `{ \"strip.outer.array\" = \"true\" }`. Changing it replaces the table.",
			},
			"stored_as": schema.StringAttribute{
				Optional: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplaceIf(externalTableStoredAsReplace, "Changing stored_as replaces the table unless SET FILE FORMAT supports both formats.", "Changing `stored_as` replaces the table unless `SET FILE FORMAT` supports both formats.")},
				MarkdownDescription: "File format: `PARQUET`, `RCFILE`, `SEQUENCEFILE`, `TEXTFILE`, `ORC`, or `AVRO`. Exactly one of `stored_as` and the pair `input_format`/`output_format` is required. Switching between `AVRO`, `PARQUET`, `RCFILE`, `SEQUENCEFILE`, and `TEXTFILE` runs `ALTER TABLE ... SET FILE FORMAT`; other changes replace the table.",
			},
			"input_format": schema.StringAttribute{
				Optional: true, Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplaceIfConfigured()},
				MarkdownDescription: "`STORED AS INPUTFORMAT` class, for formats such as Hudi or Delta Lake manifests; requires `output_format`. Without it, the class the catalog records for `stored_as` is reported. Changing it replaces the table.",
			},
			"output_format": schema.StringAttribute{
				Optional: true, Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplaceIfConfigured()},
				MarkdownDescription: "`OUTPUTFORMAT` class paired with `input_format`. Without it, the class the catalog records for `stored_as` is reported. Changing it replaces the table.",
			},
			"location": schema.StringAttribute{Required: true, MarkdownDescription: "`s3://` folder (ending in `/`) or manifest file holding the data, in the warehouse's AWS Region. Changes run `ALTER TABLE ... SET LOCATION`."},
			"table_properties": schema.MapAttribute{
				ElementType: types.StringType, Optional: true,
				PlanModifiers:       []planmodifier.Map{mapplanmodifier.RequiresReplaceIf(externalTablePropertiesReplace, "Removing a property, or changing one SET TABLE PROPERTIES does not support, replaces the table.", "Removing a property, or changing one `SET TABLE PROPERTIES` does not support, replaces the table.")},
				MarkdownDescription: "`TABLE PROPERTIES` pairs; names are case-sensitive. Only the configured properties are managed. Adding or changing `numRows`, `skip.header.line.count`, or `orc.schema.resolution` runs `ALTER TABLE ... SET TABLE PROPERTIES`; removing a property, or adding or changing any other property, replaces the table.",
			},
		},
	}
}

// ValidateConfig reports a definition Create would reject, during planning once the configuration is known.
func (r *externalTableResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data externalTableModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !req.Config.Raw.IsFullyKnown() {
		return
	}
	if err := validateExternalTableConfig(data); err != nil {
		resp.Diagnostics.AddError("Invalid external table", err.Error())
	}
}

// externalTableCatalog is the catalog state of one table.
type externalTableCatalog struct {
	// table is the SVV_EXTERNAL_TABLES row.
	table sqlclient.Row
	// columns are the data columns in order.
	columns []externalTableCatalogColumn
	// partitionKeys are the partition columns in key order.
	partitionKeys []externalTableCatalogColumn
	// serdeParameters are the decoded SerDe parameters.
	serdeParameters map[string]string
	// parameters are the decoded table properties.
	parameters map[string]string
}

// fetch reads the table's catalog state, or nil when the table or its database is gone.
func (r *externalTableResource) fetch(ctx context.Context, data externalTableModel) (*externalTableCatalog, error) {
	if err := r.bound(data.ID, data.Database.ValueString()); err != nil {
		return nil, err
	}
	if exists, err := r.localDatabaseExists(ctx, data.Database.ValueString()); err != nil || !exists {
		return nil, err
	}
	rows, err := r.selectRows(ctx, data.Database.ValueString(), readExternalTableQuery(data))
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	if len(rows) != 1 {
		return nil, fmt.Errorf("external table %q is ambiguous in the catalog", data.Name.ValueString())
	}
	catalog := &externalTableCatalog{table: rows[0]}
	columnRows, err := r.selectRows(ctx, data.Database.ValueString(), readExternalTableColumnsQuery(data.Database.ValueString(), data.Schema.ValueString(), data.Name.ValueString()))
	if err != nil {
		return nil, err
	}
	if catalog.columns, catalog.partitionKeys, err = externalTableCatalogColumns(columnRows); err != nil {
		return nil, err
	}
	if len(catalog.columns) == 0 {
		return nil, fmt.Errorf("external table %q has no columns in the catalog", data.Name.ValueString())
	}
	if catalog.serdeParameters, err = externalTableCatalogMap("SerDe parameters", rows[0]["serde_parameters"]); err != nil {
		return nil, err
	}
	if catalog.parameters, err = externalTableCatalogMap("parameters", rows[0]["parameters"]); err != nil {
		return nil, err
	}
	return catalog, nil
}

// externalTableObservation selects how much of the catalog a read adopts.
type externalTableObservation int

const (
	// externalTableRefresh keeps configured spellings and tracks only configured options and properties.
	externalTableRefresh externalTableObservation = iota
	// externalTableAdopt derives the definition after import; table properties stay unmanaged.
	externalTableAdopt
	// externalTableLookup reports the definition and every table property.
	externalTableLookup
)

// observeExternalTableColumns reports catalog columns, keeping a prior name and type spelling that declares the
// same column.
func observeExternalTableColumns(prior types.List, catalog []externalTableCatalogColumn) types.List {
	previous, _ := externalTableColumns(prior)
	columns := make([]externalTableColumnValue, len(catalog))
	for i, column := range catalog {
		columns[i] = externalTableColumnValue{Name: types.StringValue(column.name), Type: types.StringValue(column.dataType)}
		for _, known := range previous {
			if strings.EqualFold(known.Name.ValueString(), column.name) {
				columns[i].Name = known.Name
				if externalTableTypesEqual(known.Type.ValueString(), column.dataType) {
					columns[i].Type = known.Type
				}
			}
		}
	}
	return externalTableColumnList(columns)
}

// observeExternalTableOption keeps prior unless the catalog reports a different value. A value the catalog does
// not report is assumed unchanged, because Redshift records some options under names this provider cannot confirm,
// and an assumed drift would replace the table on every apply.
func observeExternalTableOption(prior types.String, catalog string, reported bool) types.String {
	if prior.IsNull() || prior.IsUnknown() || !reported || catalog == prior.ValueString() {
		return prior
	}
	return types.StringValue(catalog)
}

// observeExternalTableProperties refreshes the configured keys of a property map; see observeExternalTableOption.
func observeExternalTableProperties(prior types.Map, catalog map[string]string) types.Map {
	if prior.IsNull() || prior.IsUnknown() {
		return prior
	}
	observed := map[string]attr.Value{}
	for key, value := range prior.Elements() {
		observed[key] = value
		if text, ok := catalog[key]; ok {
			observed[key] = types.StringValue(text)
		}
	}
	return types.MapValueMust(types.StringType, observed)
}

// externalTableStringMap builds a map value, null when empty.
func externalTableStringMap(values map[string]string) types.Map {
	if len(values) == 0 {
		return types.MapNull(types.StringType)
	}
	elements := map[string]attr.Value{}
	for key, value := range values {
		elements[key] = types.StringValue(value)
	}
	return types.MapValueMust(types.StringType, elements)
}

// externalTableFormat returns the named format whose input format class the catalog reports, or "".
func externalTableFormat(inputFormat string) sqlclient.Keyword {
	for format, class := range externalTableFileFormats {
		if class == inputFormat {
			return format
		}
	}
	return ""
}

// nullableString maps an empty catalog value to null.
func nullableString(value string) types.String {
	if value == "" {
		return types.StringNull()
	}
	return types.StringValue(value)
}

// observeExternalTable derives the state from the catalog, starting from prior for identity and spellings.
func observeExternalTable(catalog *externalTableCatalog, prior externalTableModel, mode externalTableObservation) externalTableModel {
	data := prior
	row := catalog.table
	if !strings.EqualFold(prior.Name.ValueString(), row["tablename"]) {
		data.Name = types.StringValue(row["tablename"])
	}
	data.Columns = observeExternalTableColumns(prior.Columns, catalog.columns)
	data.PartitionKeys = observeExternalTableColumns(prior.PartitionKeys, catalog.partitionKeys)
	if len(catalog.partitionKeys) == 0 && (prior.PartitionKeys.IsNull() || prior.PartitionKeys.IsUnknown()) {
		data.PartitionKeys = types.ListNull(externalTableColumnType)
	}
	data.Location = types.StringValue(row["location"])
	if location := knownString(prior.Location); location != "" && externalLocationsMatch(location, row["location"]) {
		data.Location = prior.Location
	}
	data.InputFormat, data.OutputFormat = nullableString(row["input_format"]), nullableString(row["output_format"])
	format := externalTableFormat(row["input_format"])
	library := row["serialization_lib"]
	fieldDelimiter, hasFieldDelimiter := catalog.serdeParameters["field.delim"]
	lineDelimiter, hasLineDelimiter := catalog.serdeParameters["line.delim"]
	fieldDelimiter, lineDelimiter = externalTableCatalogDelimiter(fieldDelimiter), externalTableCatalogDelimiter(lineDelimiter)
	if mode == externalTableRefresh {
		if storedAs := knownString(prior.StoredAs); storedAs != "" && format != "" && !strings.EqualFold(storedAs, string(format)) {
			data.StoredAs = types.StringValue(string(format))
		}
		data.Serde = observeExternalTableOption(prior.Serde, library, library != "")
		data.FieldDelimiter = observeExternalTableOption(prior.FieldDelimiter, fieldDelimiter, hasFieldDelimiter)
		data.LineDelimiter = observeExternalTableOption(prior.LineDelimiter, lineDelimiter, hasLineDelimiter)
		data.SerdeProperties = observeExternalTableProperties(prior.SerdeProperties, catalog.serdeParameters)
		data.TableProperties = observeExternalTableProperties(prior.TableProperties, catalog.parameters)
		return data
	}
	data.StoredAs = nullableString(string(format))
	data.Serde = types.StringNull()
	if library != "" && (format == "" || library != externalTableImpliedSerdes[format]) {
		data.Serde = types.StringValue(library)
	}
	data.FieldDelimiter, data.LineDelimiter, data.SerdeProperties = types.StringNull(), types.StringNull(), types.MapNull(types.StringType)
	if data.Serde.IsNull() {
		// Hive's defaults, \u0001 and newline, are what a table without ROW FORMAT records.
		if hasFieldDelimiter && fieldDelimiter != "\u0001" {
			data.FieldDelimiter = types.StringValue(fieldDelimiter)
		}
		if hasLineDelimiter && lineDelimiter != "\n" {
			data.LineDelimiter = types.StringValue(lineDelimiter)
		}
	} else {
		properties := maps.Clone(catalog.serdeParameters)
		// serialization.format is bookkeeping Hive adds to every SerDe.
		delete(properties, "serialization.format")
		data.SerdeProperties = externalTableStringMap(properties)
	}
	data.TableProperties = types.MapNull(types.StringType)
	if mode == externalTableLookup {
		data.TableProperties = externalTableStringMap(catalog.parameters)
	}
	return data
}

// externalTableDivergence names the planned attributes the catalog does not reflect after an apply.
func externalTableDivergence(plan, observed externalTableModel) []string {
	var differing []string
	for name, values := range map[string][2]attr.Value{
		"columns":          {plan.Columns, observed.Columns},
		"partition_keys":   {plan.PartitionKeys, observed.PartitionKeys},
		"field_delimiter":  {plan.FieldDelimiter, observed.FieldDelimiter},
		"line_delimiter":   {plan.LineDelimiter, observed.LineDelimiter},
		"serde":            {plan.Serde, observed.Serde},
		"serde_properties": {plan.SerdeProperties, observed.SerdeProperties},
		"stored_as":        {plan.StoredAs, observed.StoredAs},
		"location":         {plan.Location, observed.Location},
		"table_properties": {plan.TableProperties, observed.TableProperties},
	} {
		if !values[0].IsUnknown() && !values[0].Equal(values[1]) {
			differing = append(differing, name)
		}
	}
	// The format classes are observations unless configured, which only the INPUTFORMAT form allows.
	if plan.StoredAs.IsNull() {
		for name, values := range map[string][2]attr.Value{"input_format": {plan.InputFormat, observed.InputFormat}, "output_format": {plan.OutputFormat, observed.OutputFormat}} {
			if !values[0].IsUnknown() && !values[0].Equal(values[1]) {
				differing = append(differing, name)
			}
		}
	}
	slices.Sort(differing)
	return differing
}

// verify re-reads the table after Create or Update and reports whether the catalog matches the plan.
func (r *externalTableResource) verify(ctx context.Context, plan externalTableModel) (externalTableModel, *externalTableCatalog, error) {
	catalog, err := r.fetch(ctx, plan)
	if err != nil {
		return plan, nil, err
	}
	if catalog == nil {
		return plan, nil, fmt.Errorf("the external table is absent after the change")
	}
	observed := observeExternalTable(catalog, plan, externalTableRefresh)
	if differing := externalTableDivergence(plan, observed); len(differing) > 0 {
		return observed, catalog, fmt.Errorf("the catalog does not reflect the planned %s", strings.Join(differing, ", "))
	}
	return observed, catalog, nil
}

// Create runs CREATE EXTERNAL TABLE and verifies the catalog definition.
func (r *externalTableResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data externalTableModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := validateExternalTableConfig(data)
	var statement string
	if err == nil {
		statement, err = createExternalTableStatement(data)
	}
	if err == nil {
		err = r.exec(ctx, data.Database.ValueString(), statement)
	}
	if err != nil {
		resp.Diagnostics.AddError("Create external table", err.Error())
		return
	}
	data.ID = r.identity(data.Database.ValueString(), map[string]string{"schema": data.Schema.ValueString(), "name": data.Name.ValueString()})
	planned := data
	// Failed verification must still record the created table with known values.
	if data.InputFormat.IsUnknown() {
		data.InputFormat = types.StringNull()
	}
	if data.OutputFormat.IsUnknown() {
		data.OutputFormat = types.StringNull()
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	observed, _, err := r.verify(ctx, planned)
	if err != nil {
		resp.Diagnostics.AddError("Verify external table", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &observed)...)
}

// Read refreshes the definition or removes a table that is gone, with its schema or database.
func (r *externalTableResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data externalTableModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	catalog, err := r.fetch(ctx, data)
	switch {
	case err != nil:
		resp.Diagnostics.AddError("Read external table", err.Error())
	case catalog == nil:
		resp.State.RemoveResource(ctx)
	default:
		mode := externalTableRefresh
		if data.Columns.IsNull() {
			// Import restores only the identity, so the definition comes from the catalog.
			mode = externalTableAdopt
		}
		observed := observeExternalTable(catalog, data, mode)
		resp.Diagnostics.Append(resp.State.Set(ctx, &observed)...)
		// The framework always provides private state; direct unit calls do not.
		if resp.Private != nil {
			_, imported := externalTableUnmanagedProperties(ctx, req.Private)
			unmanaged := (imported || mode == externalTableAdopt) && observed.TableProperties.IsNull()
			resp.Diagnostics.Append(recordExternalTableUnmanagedProperties(ctx, resp.Private, unmanaged, catalog.parameters)...)
		}
	}
}

// Update applies location, file format, property and column changes in place and verifies them.
func (r *externalTableResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, prior externalTableModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}
	catalog, err := r.fetch(ctx, prior)
	if err != nil {
		resp.Diagnostics.AddError("Read external table", err.Error())
		return
	}
	if catalog == nil {
		resp.Diagnostics.AddError("Update external table", "The external table disappeared; refresh the plan.")
		return
	}
	if prior.TableProperties.IsNull() {
		// Comparing with the catalog rather than the marker keeps a plan made without refresh from repeating
		// properties that are already set.
		prior.TableProperties = externalTableStringMap(externalTablePriorProperties(prior.TableProperties, plan.TableProperties, catalog.parameters))
	}
	statements, err := alterExternalTableStatements(prior, plan)
	if err == nil {
		err = r.exec(ctx, plan.Database.ValueString(), statements...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Update external table", err.Error())
		return
	}
	observed, verified, err := r.verify(ctx, plan)
	if err != nil {
		resp.Diagnostics.AddError("Verify external table", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &observed)...)
	if resp.Private != nil {
		_, imported := externalTableUnmanagedProperties(ctx, req.Private)
		resp.Diagnostics.Append(recordExternalTableUnmanagedProperties(ctx, resp.Private, imported && observed.TableProperties.IsNull(), verified.parameters)...)
	}
}

// Delete drops the table restrictively and verifies its removal; the S3 data is not touched.
func (r *externalTableResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data externalTableModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	catalog, err := r.fetch(ctx, data)
	if err == nil && catalog != nil {
		err = r.exec(ctx, data.Database.ValueString(), dropExternalTableStatement(data))
		if err == nil {
			catalog, err = r.fetch(ctx, data)
			if err == nil && catalog != nil {
				err = fmt.Errorf("the external table remains after deletion")
			}
		}
	}
	if err != nil {
		resp.Diagnostics.AddError("Delete external table", err.Error())
	}
}

// ImportState restores the table identity from its JSON ID; Read derives the definition.
func (r *externalTableResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importIdentity(ctx, req, resp, "database", "schema", "name")
}
