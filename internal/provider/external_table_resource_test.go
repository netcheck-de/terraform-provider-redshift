package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerLifecycleCase(lifecycleCase{
	name: "external table", new: newExternalTableResource, model: externalTableTestModel(),
	absent: func(c *catalog) { delete(fakeState[*externalTableFamily](c, "external_table").tables, "events") },
})

var _ = registerReplacementPolicy("redshift_external_table", map[string]replaceRule{
	"database":         replaceAlways,
	"schema":           replaceAlways,
	"name":             replaceAlways,
	"column":           replaceConditional,
	"partition_key":    replaceConditional,
	"field_delimiter":  replaceAlways,
	"line_delimiter":   replaceAlways,
	"serde":            replaceAlways,
	"serde_properties": replaceAlways,
	"stored_as":        replaceConditional,
	"input_format":     replaceAlways,
	"output_format":    replaceAlways,
	"location":         replaceNever,
	"table_properties": replaceConditional,
})

var _ = registerValidateConfigCase("external_table", validateConfigCase{
	new:   newExternalTableResource,
	valid: externalTableTestModel(),
	invalid: func() externalTableModel {
		model := externalTableTestModel()
		model.InputFormat = types.StringValue("org.apache.hadoop.mapred.TextInputFormat")
		return model
	}(),
	unknown: func() externalTableModel {
		model := externalTableTestModel()
		model.StoredAs, model.InputFormat, model.OutputFormat = types.StringNull(), types.StringUnknown(), types.StringValue("org.example.Output")
		return model
	}(),
})

// externalTableFake returns the fake catalog's representative table.
func externalTableFakeOf(c *catalog) *externalTableFake {
	return fakeState[*externalTableFamily](c, "external_table").tables["events"]
}

// externalTableTestResource binds a table resource to a SQL client.
func externalTableTestResource(t *testing.T, client sqlclient.Client) *externalTableResource {
	t.Helper()
	r := &externalTableResource{}
	configureTestResource(t, r, client)
	return r
}

// Unknown names, types, and entire blocks must not hide independently invalid fields or known name collisions.
func TestExternalTablePartialKnownColumnsValidation(t *testing.T) {
	known, unknown := types.StringValue, types.StringUnknown()
	column := func(name, dataType types.String) attr.Value {
		return types.ObjectValueMust(externalTableColumnType.AttrTypes, map[string]attr.Value{"name": name, "type": dataType})
	}
	list := func(columns ...attr.Value) types.List { return types.ListValueMust(externalTableColumnType, columns) }
	none := types.ListNull(externalTableColumnType)
	valid := column(known("id"), known("integer"))
	unknownBlock := types.ObjectUnknown(externalTableColumnType.AttrTypes)
	for _, test := range []struct {
		name                string
		columns, partitions types.List
		want                string
		minimumErrors       int
	}{
		{"unsupported type with unknown name", list(column(unknown, known("timestamptz"))), none, "not supported", 1},
		{"malformed type with unknown name", list(column(unknown, known("integer; DROP TABLE x"))), none, "type", 1},
		{"invalid modifier with unknown sibling", list(valid, unknownBlock, column(known("amount"), known("decimal(39,2)"))), none, "type", 1},
		{"empty name with unknown type", list(column(known(" "), unknown)), none, "nonempty names", 1},
		{"null name with unknown type", list(column(types.StringNull(), unknown)), none, "nonempty names", 1},
		{"null type with unknown name", list(column(unknown, types.StringNull())), none, "type", 1},
		{"pseudocolumn with unknown type", list(column(known("$PATH"), unknown)), none, "pseudocolumn", 1},
		{"duplicate columns with unknown types and sibling", list(column(known("ID"), unknown), unknownBlock, column(known("id"), unknown)), none, "repeats", 1},
		{"duplicate across partially known lists", list(column(known("ID"), unknown)), list(unknownBlock, column(known("id"), unknown)), "partition_key name", 1},
		{"duplicate partition keys with unknown type", list(valid), list(column(known("Day"), unknown), column(known("day"), known("date"))), "repeats", 1},
		{"partition pseudocolumn with unknown type", types.ListUnknown(externalTableColumnType), list(column(known("$SIZE"), unknown)), "pseudocolumn", 1},
		{"invalid partition type with unknown columns", types.ListUnknown(externalTableColumnType), list(column(unknown, known("super"))), "not supported", 1},
		{"empty partition name with unknown columns", types.ListUnknown(externalTableColumnType), list(column(known(""), unknown)), "nonempty names", 1},
		{"invalid type still records name", list(column(known("id"), known("super")), unknownBlock), list(column(known("ID"), unknown)), "repeats", 2},
		{"independent name and type errors", list(column(known("$path"), known("super")), unknownBlock), none, "pseudocolumn", 2},
		{"null block", list(valid, types.ObjectNull(externalTableColumnType.AttrTypes)), none, "must not be null", 1},
		{"empty data columns", list(), none, "at least one column", 1},
		{"null data columns", none, none, "at least one column", 1},
		{"valid known names and unknown types", list(column(known("id"), unknown), column(known("other"), unknown)), list(column(known("day"), unknown)), "", 0},
		{"valid types and unknown names", list(column(unknown, known("integer")), column(unknown, known("varchar(64)"))), list(column(unknown, known("date"))), "", 0},
		{"unknown blocks among valid columns", list(valid, unknownBlock, column(known("label"), known("varchar(64)"))), list(unknownBlock), "", 0},
		{"whole unknown lists defer", types.ListUnknown(externalTableColumnType), types.ListUnknown(externalTableColumnType), "", 0},
		{"optional partition keys omitted", list(valid), none, "", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := externalTableTestModel()
			model.Columns, model.PartitionKeys, model.Location = test.columns, test.partitions, unknown
			r := newExternalTableResource().(*externalTableResource)
			var response resource.ValidateConfigResponse
			r.ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config(testState(t, r, model))}, &response)
			if test.want == "" {
				assert.False(t, response.Diagnostics.HasError(), "%v", response.Diagnostics)
				return
			}
			require.True(t, response.Diagnostics.HasError(), "%v", response.Diagnostics)
			var details []string
			for _, diagnostic := range response.Diagnostics.Errors() {
				details = append(details, diagnostic.Detail())
			}
			assert.Contains(t, strings.Join(details, "\n"), test.want)
			assert.GreaterOrEqual(t, len(response.Diagnostics.Errors()), test.minimumErrors)
		})
	}
}

// TestExternalTablePartialKnownPropertiesValidation reports empty property names and serde_properties without serde at
// plan time, deferring only checks that depend on an unknown value.
func TestExternalTablePartialKnownPropertiesValidation(t *testing.T) {
	properties := func(key string, value types.String) types.Map {
		return types.MapValueMust(types.StringType, map[string]attr.Value{key: value})
	}
	none, unknown := types.MapNull(types.StringType), types.MapUnknown(types.StringType)
	serde := types.StringValue("org.example.Serde")
	for _, test := range []struct {
		name                             string
		tableProperties, serdeProperties types.Map
		serde                            types.String
		want                             string
	}{
		{"empty table key", properties("", types.StringValue("x")), none, types.StringNull(), "table_properties need nonempty names"},
		{"empty table key unknown value", properties("", types.StringUnknown()), none, types.StringNull(), "table_properties need nonempty names"},
		{"empty serde key", none, properties("", types.StringValue("x")), serde, "serde_properties need nonempty names"},
		{"empty serde key unknown value and dependency", none, properties("", types.StringUnknown()), types.StringUnknown(), "serde_properties need nonempty names"},
		{"properties without serde", none, properties("key", types.StringValue("x")), types.StringNull(), "serde_properties require serde"},
		{"unknown value still requires serde", none, properties("key", types.StringUnknown()), types.StringNull(), "serde_properties require serde"},
		{"empty serde still missing", none, properties("key", types.StringValue("x")), types.StringValue(""), "serde_properties require serde"},
		{"unknown serde dependency defers", none, properties("key", types.StringValue("x")), types.StringUnknown(), ""},
		{"unknown serde map defers missing dependency", none, unknown, types.StringNull(), ""},
		{"unknown maps and dependency", unknown, unknown, types.StringUnknown(), ""},
		{"known serde with unknown value", none, properties("key", types.StringUnknown()), serde, ""},
		{"known table key unknown value", properties("numRows", types.StringUnknown()), none, types.StringNull(), ""},
		{"empty serde map needs no dependency", none, types.MapValueMust(types.StringType, map[string]attr.Value{}), types.StringNull(), ""},
		{"null serde map needs no dependency", none, none, types.StringNull(), ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := externalTableTestModel()
			model.Columns = externalTableColumnList([]externalTableColumnValue{{Name: types.StringValue("id"), Type: types.StringUnknown()}})
			model.TableProperties, model.SerdeProperties, model.Serde = test.tableProperties, test.serdeProperties, test.serde
			model.FieldDelimiter = types.StringNull()
			r := newExternalTableResource().(*externalTableResource)
			var response resource.ValidateConfigResponse
			r.ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config(testState(t, r, model))}, &response)
			if test.want == "" {
				assert.False(t, response.Diagnostics.HasError(), "%v", response.Diagnostics)
				return
			}
			require.True(t, response.Diagnostics.HasError(), "%v", response.Diagnostics)
			assert.Contains(t, response.Diagnostics.Errors()[0].Detail(), test.want)
		})
	}
}

// TestExternalTableNestedUnknownValidationAndStrictCreate rejects a known unsupported column type beside unknowns,
// defers an unknown type at plan time, and has Create reject it before any SQL or state.
func TestExternalTableNestedUnknownValidationAndStrictCreate(t *testing.T) {
	model := externalTableTestModel()
	model.Columns = externalTableColumnList([]externalTableColumnValue{
		{Name: types.StringValue("id"), Type: types.StringUnknown()},
		{Name: types.StringUnknown(), Type: types.StringValue("super")},
	})
	r := newExternalTableResource().(*externalTableResource)
	var response resource.ValidateConfigResponse
	r.ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config(testState(t, r, model))}, &response)
	require.True(t, response.Diagnostics.HasError())
	assert.Contains(t, response.Diagnostics.Errors()[0].Detail(), "not supported")

	// Planning may defer the unknown type, but Create must reject it before any SQL or partial identity state.
	model.Columns = externalTableColumnList([]externalTableColumnValue{{Name: types.StringValue("id"), Type: types.StringUnknown()}})
	response = resource.ValidateConfigResponse{}
	r.ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config(testState(t, r, model))}, &response)
	require.False(t, response.Diagnostics.HasError(), "%v", response.Diagnostics)
	c := fullCatalog()
	state, diagnostics := applyOperation(t, externalTableTestResource(t, c), "create", model, model, nil)
	require.True(t, diagnostics.HasError(), "%v", diagnostics)
	assert.Contains(t, diagnostics.Errors()[0].Detail(), "column blocks must be known")
	assert.True(t, state.Raw.IsNull(), "failed validation must not record a partial resource")
	assert.Empty(t, c.writes)
}

// externalTableState runs one lifecycle operation and decodes the resulting state.
func externalTableState(t *testing.T, client sqlclient.Client, operation string, prior, planned externalTableModel) (externalTableModel, error) {
	t.Helper()
	state, diagnostics := applyOperation(t, externalTableTestResource(t, client), operation, prior, planned, nil)
	var observed externalTableModel
	if !state.Raw.IsNull() {
		require.False(t, state.Get(context.Background(), &observed).HasError())
	}
	if diagnostics.HasError() {
		return observed, errors.New(diagnostics.Errors()[0].Summary() + ": " + diagnostics.Errors()[0].Detail())
	}
	return observed, nil
}

// TestExternalTableCreateKeepsConfiguredSpellings verifies that Hive catalog spellings converge with configuration.
func TestExternalTableCreateKeepsConfiguredSpellings(t *testing.T) {
	c := fullCatalog()
	delete(fakeState[*externalTableFamily](c, "external_table").tables, "events")
	model := externalTableTestModel()
	model.Columns = externalTableTestColumns("ID", "int4", "Amount", "decimal(8, 2)", "ratio", "float", "label", "varchar")
	model.InputFormat, model.OutputFormat = types.StringUnknown(), types.StringUnknown()
	observed, err := externalTableState(t, c, "create", model, model)
	require.NoError(t, err)
	assert.Equal(t, model.Columns, observed.Columns)
	assert.Equal(t, model.Location, observed.Location)
	assert.Equal(t, "org.apache.hadoop.mapred.TextInputFormat", observed.InputFormat.ValueString())
	assert.JSONEq(t, `{"workgroup_name":"warehouse","database":"admin","schema":"example_external","name":"events"}`, observed.ID.ValueString())
	table := externalTableFakeOf(c)
	assert.Equal(t, []externalTableFakeColumn{{"id", "int"}, {"amount", "decimal(8,2)"}, {"ratio", "double"}, {"label", "varchar(256)"}}, table.columns)
}

// TestExternalTableImportAdoptsCatalog derives the definition after import without managing table properties.
func TestExternalTableImportAdoptsCatalog(t *testing.T) {
	c := fullCatalog()
	r := externalTableTestResource(t, c)
	id := `{"workgroup_name":"warehouse","database":"admin","schema":"example_external","name":"events"}`
	imported := resource.ImportStateResponse{State: emptyState(t, r)}
	r.ImportState(context.Background(), resource.ImportStateRequest{ID: id}, &imported)
	require.False(t, imported.Diagnostics.HasError(), "%v", imported.Diagnostics)
	read := resource.ReadResponse{State: imported.State}
	r.Read(context.Background(), resource.ReadRequest{State: imported.State}, &read)
	require.False(t, read.Diagnostics.HasError(), "%v", read.Diagnostics)
	var observed externalTableModel
	require.False(t, read.State.Get(context.Background(), &observed).HasError())
	expected := externalTableTestModel()
	// Import adopts the catalog types in their canonical uppercase spelling.
	assert.Equal(t, externalTableTestColumns("id", "INTEGER", "label", "VARCHAR(64)"), observed.Columns)
	assert.Equal(t, externalTableTestColumns("event_date", "DATE"), observed.PartitionKeys)
	assert.Equal(t, expected.FieldDelimiter, observed.FieldDelimiter)
	assert.Equal(t, expected.StoredAs, observed.StoredAs)
	assert.Equal(t, expected.Location, observed.Location)
	assert.True(t, observed.Serde.IsNull(), "the implied TEXTFILE SerDe is not configuration")
	assert.True(t, observed.SerdeProperties.IsNull())
	assert.True(t, observed.LineDelimiter.IsNull())
	assert.True(t, observed.TableProperties.IsNull(), "import leaves table properties unmanaged")
}

// TestExternalTableObserveModes covers SerDe adoption, explicit formats, and refresh drift.
func TestExternalTableObserveModes(t *testing.T) {
	catalog := &externalTableCatalog{
		table:           sqlclient.Row{"tablename": "events", "location": "s3://bucket/json", "input_format": "org.example.CustomInput", "output_format": "org.example.CustomOutput", "serialization_lib": "org.openx.data.jsonserde.JsonSerDe"},
		columns:         []externalTableCatalogColumn{{"id", "integer", 1}},
		serdeParameters: map[string]string{"serialization.format": "1", "strip.outer.array": "true"},
		parameters:      map[string]string{"EXTERNAL": "TRUE", "numRows": "5"},
	}
	prior := externalTableModel{Name: types.StringValue("Events"), Columns: types.ListNull(externalTableColumnType), PartitionKeys: types.ListNull(externalTableColumnType), SerdeProperties: types.MapNull(types.StringType), TableProperties: types.MapNull(types.StringType)}
	adopted := observeExternalTable(catalog, prior, externalTableAdopt)
	assert.Equal(t, "Events", adopted.Name.ValueString())
	assert.True(t, adopted.StoredAs.IsNull(), "unknown input formats keep the INPUTFORMAT form")
	assert.Equal(t, "org.openx.data.jsonserde.JsonSerDe", adopted.Serde.ValueString())
	assert.Equal(t, externalTableTestMap("strip.outer.array", "true"), adopted.SerdeProperties)
	assert.True(t, adopted.PartitionKeys.IsNull())
	assert.True(t, adopted.TableProperties.IsNull())
	assert.True(t, adopted.FieldDelimiter.IsNull())
	looked := observeExternalTable(catalog, prior, externalTableLookup)
	assert.Equal(t, externalTableTestMap("EXTERNAL", "TRUE", "numRows", "5"), looked.TableProperties)

	configured := adopted
	configured.Location = types.StringValue("s3://bucket/json/")
	configured.PartitionKeys = externalTableTestColumns()
	configured.SerdeProperties = externalTableTestMap("strip.outer.array", "false", "missing", "kept")
	configured.TableProperties = externalTableTestMap("numRows", "4", "absent", "x")
	refreshed := observeExternalTable(catalog, configured, externalTableRefresh)
	assert.Equal(t, "s3://bucket/json/", refreshed.Location.ValueString(), "the catalog drops the trailing slash")
	assert.Equal(t, externalTableTestColumns(), refreshed.PartitionKeys, "an empty list stays empty")
	assert.Equal(t, externalTableTestMap("strip.outer.array", "true", "missing", "kept"), refreshed.SerdeProperties)
	assert.Equal(t, externalTableTestMap("numRows", "5", "absent", "x"), refreshed.TableProperties)

	catalog.table["serialization_lib"] = "org.example.OtherSerDe"
	catalog.table["input_format"] = externalTableFileFormats["PARQUET"]
	catalog.serdeParameters = map[string]string{"field.delim": "|", "line.delim": "\n"}
	drifted := configured
	drifted.StoredAs, drifted.Serde, drifted.FieldDelimiter, drifted.LineDelimiter = types.StringValue("textfile"), types.StringValue("org.openx.data.jsonserde.JsonSerDe"), types.StringValue(","), types.StringNull()
	refreshed = observeExternalTable(catalog, drifted, externalTableRefresh)
	assert.Equal(t, "PARQUET", refreshed.StoredAs.ValueString())
	assert.Equal(t, "org.example.OtherSerDe", refreshed.Serde.ValueString())
	assert.Equal(t, "|", refreshed.FieldDelimiter.ValueString())
	assert.True(t, refreshed.LineDelimiter.IsNull(), "unconfigured options are not adopted on refresh")

	catalog.table["serialization_lib"] = ""
	catalog.serdeParameters = map[string]string{"field.delim": "\u0001", "line.delim": "\r"}
	adopted = observeExternalTable(catalog, prior, externalTableAdopt)
	assert.Equal(t, "PARQUET", adopted.StoredAs.ValueString())
	assert.True(t, adopted.Serde.IsNull())
	assert.True(t, adopted.FieldDelimiter.IsNull(), "Hive's default field delimiter is not configuration")
	assert.Equal(t, "\r", adopted.LineDelimiter.ValueString())
}

// TestExternalTableUpdateAltersInPlace appends a column, moves the table, switches the format and sets a property.
func TestExternalTableUpdateAltersInPlace(t *testing.T) {
	c := fullCatalog()
	prior := externalTableTestModel()
	prior.ID = types.StringValue(`{"workgroup_name":"warehouse","database":"admin","schema":"example_external","name":"events"}`)
	plan := prior
	plan.Columns = externalTableTestColumns("id", "integer", "label", "varchar(64)", "amount", "decimal(8,2)")
	plan.Location = types.StringValue("s3://example-bucket/events-v2/")
	plan.StoredAs = types.StringValue("PARQUET")
	plan.InputFormat, plan.OutputFormat = types.StringUnknown(), types.StringUnknown()
	plan.TableProperties = externalTableTestMap("skip.header.line.count", "1", "numRows", "42")
	observed, err := externalTableState(t, c, "update", prior, plan)
	require.NoError(t, err)
	assert.Equal(t, plan.Columns, observed.Columns)
	assert.Equal(t, externalTableFileFormats["PARQUET"], observed.InputFormat.ValueString())
	table := externalTableFakeOf(c)
	assert.Equal(t, "s3://example-bucket/events-v2/", table.location)
	assert.Equal(t, "42", table.parameters["numRows"])
	assert.Equal(t, []string{
		`ALTER TABLE "example_external"."events" ADD COLUMN "amount" DECIMAL(8, 2)`,
		`ALTER TABLE "example_external"."events" SET FILE FORMAT PARQUET`,
		`ALTER TABLE "example_external"."events" SET LOCATION 's3://example-bucket/events-v2/'`,
		`ALTER TABLE "example_external"."events" SET TABLE PROPERTIES ('numRows' = '42')`,
	}, c.writes)
}

// TestExternalTableUpdateAfterImportComparesProperties sets only differing settable properties when state holds none.
func TestExternalTableUpdateAfterImportComparesProperties(t *testing.T) {
	prior := externalTableTestModel()
	prior.TableProperties = types.MapNull(types.StringType)
	for name, test := range map[string]struct {
		properties types.Map
		writes     []string
		err        string
	}{
		"matching":    {properties: externalTableTestMap("skip.header.line.count", "1")},
		"settable":    {properties: externalTableTestMap("skip.header.line.count", "1", "numRows", "9"), writes: []string{`ALTER TABLE "example_external"."events" SET TABLE PROPERTIES ('numRows' = '9')`}},
		"create_only": {properties: externalTableTestMap("compression_type", "gzip"), err: `table property "compression_type" can only be set when the table is created`},
	} {
		t.Run(name, func(t *testing.T) {
			c := fullCatalog()
			plan := prior
			plan.TableProperties = test.properties
			_, err := externalTableState(t, c, "update", prior, plan)
			if test.err != "" {
				require.ErrorContains(t, err, test.err)
				assert.Empty(t, c.writes)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.writes, c.writes)
		})
	}
}

// TestExternalTableFailurePaths covers validation before SQL, malformed catalog rows, and non-converging changes.
func TestExternalTableFailurePaths(t *testing.T) {
	model := externalTableTestModel()
	t.Run("validation runs before SQL", func(t *testing.T) {
		invalid := model
		invalid.InputFormat = types.StringValue("org.apache.hadoop.mapred.TextInputFormat")
		calls := 0
		client := queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
			calls++
			return nil, nil
		})
		_, err := externalTableState(t, client, "create", invalid, invalid)
		require.ErrorContains(t, err, "stored_as conflicts with input_format")
		assert.Zero(t, calls)
	})
	rows := func(table, columns []sqlclient.Row) sqlclient.Client {
		return queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
			if strings.Contains(sql, "svv_external_tables") {
				return table, nil
			}
			return columns, nil
		})
	}
	table := sqlclient.Row{"tablename": "events", "location": "s3://example-bucket/events/", "serde_parameters": "{}", "parameters": "{}"}
	column := sqlclient.Row{"columnname": "id", "external_type": "int", "columnnum": "1", "part_key": "0"}
	for name, test := range map[string]struct {
		client sqlclient.Client
		err    string
	}{
		"ambiguous":     {rows([]sqlclient.Row{table, table}, nil), "ambiguous"},
		"no columns":    {rows([]sqlclient.Row{table}, nil), "no columns"},
		"bad column":    {rows([]sqlclient.Row{table}, []sqlclient.Row{{"columnname": "id", "columnnum": "?", "part_key": "0"}}), "incomplete catalog metadata"},
		"bad serde":     {rows([]sqlclient.Row{{"tablename": "events", "serde_parameters": "{"}}, []sqlclient.Row{column}), "SerDe parameters"},
		"bad parameter": {rows([]sqlclient.Row{{"tablename": "events", "parameters": "["}}, []sqlclient.Row{column}), "decode external table parameters"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := externalTableState(t, test.client, "read", model, model)
			require.ErrorContains(t, err, test.err)
		})
	}
	t.Run("create does not converge", func(t *testing.T) {
		c := fullCatalog()
		delete(fakeState[*externalTableFamily](c, "external_table").tables, "events")
		drifting := queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
			rows, err := c.Query(ctx, target, sql, parameters)
			if strings.HasPrefix(sql, "CREATE") {
				externalTableFakeOf(c).location = "s3://elsewhere/"
			}
			return rows, err
		})
		observed, err := externalTableState(t, drifting, "create", model, model)
		require.ErrorContains(t, err, "the catalog does not reflect the planned location")
		assert.False(t, observed.ID.IsNull(), "the created table stays in state")
	})
	t.Run("update of a missing table", func(t *testing.T) {
		c := fullCatalog()
		delete(fakeState[*externalTableFamily](c, "external_table").tables, "events")
		_, err := externalTableState(t, c, "update", model, model)
		require.ErrorContains(t, err, "disappeared")
	})
	t.Run("update judges columns by the catalog mapping", func(t *testing.T) {
		// A plan made before orc.schema.resolution changed outside Terraform still maps the blocks by name.
		c := fullCatalog()
		table := externalTableFakeOf(c)
		table.inputFormat, table.serde = externalTableFileFormats["ORC"], externalTableImpliedSerdes["ORC"]
		table.parameters["orc.schema.resolution"] = "position"
		prior := model
		prior.StoredAs, prior.FieldDelimiter = types.StringValue("ORC"), types.StringNull()
		plan := prior
		plan.Columns = externalTableTestColumns("label", "varchar(64)", "id", "integer")
		_, err := externalTableState(t, c, "update", prior, plan)
		require.ErrorContains(t, err, "replace the table")
		assert.Empty(t, c.writes)
	})
	t.Run("update rejects changes plans would replace", func(t *testing.T) {
		plan := model
		plan.StoredAs = types.StringValue("ORC")
		_, err := externalTableState(t, fullCatalog(), "update", model, plan)
		require.ErrorContains(t, err, "replace the table")
	})
	t.Run("read drops a table whose schema is gone", func(t *testing.T) {
		c := fullCatalog()
		c.external = false
		r := externalTableTestResource(t, c)
		state := testState(t, r, model)
		resp := resource.ReadResponse{State: state}
		r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)
		require.False(t, resp.Diagnostics.HasError())
		assert.True(t, resp.State.Raw.IsNull())
	})
}

// externalTableModifierRequests builds plan-modifier requests from two full models.
func externalTableModifierRequests(t *testing.T, prior, plan externalTableModel) (tfsdk.State, tfsdk.Plan) {
	t.Helper()
	r := newExternalTableResource()
	return testState(t, r, prior), tfsdk.Plan(testState(t, r, plan))
}

// TestExternalTableColumnsReplacement covers both branches of the conditional columns policy.
func TestExternalTableColumnsReplacement(t *testing.T) {
	base := externalTableTestModel()
	insert := []string{"id", "integer", "extra", "date", "label", "varchar(64)"}
	reorder := []string{"label", "varchar(64)", "id", "integer"}
	// prior and plan are the configured orc.schema.resolution; empty leaves it unset.
	for name, test := range map[string]struct {
		storedAs    string
		columns     []string
		prior, plan string
		replace     bool
	}{
		"append":                      {storedAs: "TEXTFILE", columns: []string{"id", "integer", "label", "varchar(64)", "extra", "date"}},
		"drop":                        {storedAs: "TEXTFILE", columns: []string{"label", "varchar(64)"}},
		"respell":                     {storedAs: "TEXTFILE", columns: []string{"ID", "int4", "label", "varchar(64)"}},
		"reorder":                     {storedAs: "TEXTFILE", columns: reorder, replace: true},
		"insert":                      {storedAs: "TEXTFILE", columns: insert, replace: true},
		"parquet insert":              {storedAs: "PARQUET", columns: insert},
		"parquet reorder":             {storedAs: "PARQUET", columns: reorder},
		"parquet drop and add":        {storedAs: "PARQUET", columns: []string{"label", "varchar(64)", "extra", "date"}},
		"parquet retype":              {storedAs: "PARQUET", columns: []string{"id", "bigint", "label", "varchar(64)"}, replace: true},
		"retype":                      {storedAs: "TEXTFILE", columns: []string{"id", "bigint", "label", "varchar(64)"}, replace: true},
		"avro append":                 {storedAs: "AVRO", columns: []string{"id", "integer", "label", "varchar(64)", "extra", "date"}, replace: true},
		"avro respell":                {storedAs: "AVRO", columns: []string{"ID", "int4", "label", "varchar(64)"}},
		"orc insert":                  {storedAs: "ORC", columns: insert},
		"orc reorder":                 {storedAs: "ORC", columns: reorder},
		"orc drop first":              {storedAs: "ORC", columns: []string{"label", "varchar(64)", "extra", "date"}},
		"orc retype":                  {storedAs: "ORC", columns: []string{"label", "varchar(64)", "id", "bigint"}, replace: true},
		"orc by position insert":      {storedAs: "ORC", columns: insert, prior: "position", plan: "position", replace: true},
		"orc switching to position":   {storedAs: "ORC", columns: reorder, plan: "position", replace: true},
		"orc switching from position": {storedAs: "ORC", columns: reorder, prior: "position", plan: "name"},
		"unknown value":               {storedAs: "TEXTFILE", replace: true},
	} {
		t.Run(name, func(t *testing.T) {
			prior := base
			prior.StoredAs = types.StringValue(test.storedAs)
			resolution := func(value string) types.Map {
				if value == "" {
					return types.MapNull(types.StringType)
				}
				return externalTableTestMap("orc.schema.resolution", value)
			}
			prior.TableProperties = resolution(test.prior)
			plan := prior
			plan.Columns = externalTableTestColumns(test.columns...)
			plan.TableProperties = resolution(test.plan)
			if test.columns == nil {
				plan.Columns = types.ListUnknown(externalTableColumnType)
			}
			state, planned := externalTableModifierRequests(t, prior, plan)
			resp := listplanmodifier.RequiresReplaceIfFuncResponse{}
			externalTableColumnsReplace(context.Background(), planmodifier.ListRequest{Path: path.Root("column"), State: state, Plan: planned, StateValue: prior.Columns, PlanValue: plan.Columns}, &resp)
			assert.Equal(t, test.replace, resp.RequiresReplace)
		})
	}
}

// TestExternalTablePartitionKeysReplacement replaces for any change but spelling.
func TestExternalTablePartitionKeysReplacement(t *testing.T) {
	for name, test := range map[string]struct {
		prior, plan types.List
		replace     bool
	}{
		"respell":      {externalTableTestColumns("event_date", "date"), externalTableTestColumns("EVENT_DATE", "date"), false},
		"null to none": {types.ListNull(externalTableColumnType), externalTableTestColumns(), false},
		"retype":       {externalTableTestColumns("event_date", "date"), externalTableTestColumns("event_date", "varchar(10)"), true},
		"add":          {types.ListNull(externalTableColumnType), externalTableTestColumns("event_date", "date"), true},
		"unknown":      {types.ListNull(externalTableColumnType), types.ListUnknown(externalTableColumnType), true},
	} {
		t.Run(name, func(t *testing.T) {
			resp := listplanmodifier.RequiresReplaceIfFuncResponse{}
			externalTablePartitionKeysReplace(context.Background(), planmodifier.ListRequest{StateValue: test.prior, PlanValue: test.plan}, &resp)
			assert.Equal(t, test.replace, resp.RequiresReplace)
		})
	}
}

// TestExternalTableStoredAsReplacement keeps SET FILE FORMAT switches in place and replaces the rest.
func TestExternalTableStoredAsReplacement(t *testing.T) {
	for name, test := range map[string]struct {
		prior, plan types.String
		replace     bool
	}{
		"text to parquet":                {types.StringValue("TEXTFILE"), types.StringValue("PARQUET"), false},
		"parquet to text without layout": {types.StringValue("PARQUET"), types.StringValue("TEXTFILE"), true},
		"case only":                      {types.StringValue("orc"), types.StringValue("ORC"), false},
		"to orc":                         {types.StringValue("TEXTFILE"), types.StringValue("ORC"), true},
		"to input format":                {types.StringValue("PARQUET"), types.StringNull(), true},
		"from import":                    {types.StringNull(), types.StringValue("PARQUET"), true},
		"unknown":                        {types.StringValue("PARQUET"), types.StringUnknown(), true},
	} {
		t.Run(name, func(t *testing.T) {
			resp := stringplanmodifier.RequiresReplaceIfFuncResponse{}
			prior, plan := externalTableTestModel(), externalTableTestModel()
			prior.StoredAs, plan.StoredAs = test.prior, test.plan
			state, planned := externalTableModifierRequests(t, prior, plan)
			externalTableStoredAsReplace(context.Background(), planmodifier.StringRequest{State: state, Plan: planned, StateValue: test.prior, PlanValue: test.plan}, &resp)
			assert.Equal(t, test.replace, resp.RequiresReplace)
		})
	}
}

// TestExternalTablePropertiesReplacement keeps settable additions in place and replaces removals and others,
// including a switch to position mapping whose column order no layout confirms.
func TestExternalTablePropertiesReplacement(t *testing.T) {
	unknown := func(key string) types.Map {
		return types.MapValueMust(types.StringType, map[string]attr.Value{"numRows": types.StringValue("1"), key: types.StringUnknown()})
	}
	for name, test := range map[string]struct {
		storedAs    string
		prior, plan types.Map
		replace     bool
	}{
		"set numRows":                      {"", externalTableTestMap("numRows", "1"), externalTableTestMap("numRows", "2"), false},
		"add header":                       {"", externalTableTestMap("numRows", "1"), externalTableTestMap("numRows", "1", "skip.header.line.count", "1"), false},
		"remove":                           {"", externalTableTestMap("numRows", "1"), externalTableTestMap(), true},
		"remove all":                       {"", externalTableTestMap("numRows", "1"), types.MapNull(types.StringType), true},
		"change compression":               {"", externalTableTestMap("compression_type", "gzip"), externalTableTestMap("compression_type", "none"), true},
		"created without, add compression": {"", types.MapNull(types.StringType), externalTableTestMap("compression_type", "gzip"), true},
		"created without, add numRows":     {"", types.MapNull(types.StringType), externalTableTestMap("numRows", "1"), false},
		"unknown":                          {"", externalTableTestMap("numRows", "1"), types.MapUnknown(types.StringType), true},
		"unknown settable value":           {"", externalTableTestMap("numRows", "1"), unknown("skip.header.line.count"), false},
		"unknown create-only value":        {"", externalTableTestMap("numRows", "1"), unknown("compression_type"), true},
		"orc switching to position":        {"ORC", types.MapNull(types.StringType), externalTableTestMap("orc.schema.resolution", "position"), true},
		"orc switching to unknown":         {"ORC", externalTableTestMap("numRows", "1"), unknown("orc.schema.resolution"), true},
		"orc switching to name":            {"ORC", externalTableTestMap("orc.schema.resolution", "position"), externalTableTestMap("orc.schema.resolution", "name"), false},
	} {
		t.Run(name, func(t *testing.T) {
			prior := externalTableTestModel()
			if test.storedAs != "" {
				prior.StoredAs, prior.FieldDelimiter = types.StringValue(test.storedAs), types.StringNull()
			}
			prior.TableProperties = test.prior
			plan := prior
			plan.TableProperties = test.plan
			state, planned := externalTableModifierRequests(t, prior, plan)
			resp := mapplanmodifier.RequiresReplaceIfFuncResponse{}
			externalTablePropertiesReplace(context.Background(), planmodifier.MapRequest{Path: path.Root("table_properties"), State: state, Plan: planned, StateValue: test.prior, PlanValue: test.plan}, &resp)
			assert.Equal(t, test.replace, resp.RequiresReplace)
		})
	}
}

// TestExternalTableTranscripts records the SQL conversation of format, SerDe and in-place change flows.
func TestExternalTableTranscripts(t *testing.T) {
	empty := func(c *catalog) { delete(fakeState[*externalTableFamily](c, "external_table").tables, "events") }
	jsonTable := externalTableTestModel()
	jsonTable.PartitionKeys, jsonTable.FieldDelimiter, jsonTable.TableProperties = types.ListNull(externalTableColumnType), types.StringNull(), types.MapNull(types.StringType)
	jsonTable.Serde = types.StringValue("org.openx.data.jsonserde.JsonSerDe")
	jsonTable.SerdeProperties = externalTableTestMap("strip.outer.array", "true")
	hudi := jsonTable
	hudi.StoredAs, hudi.Serde, hudi.SerdeProperties = types.StringNull(), types.StringValue("org.apache.hadoop.hive.ql.io.parquet.serde.ParquetHiveSerDe"), types.MapNull(types.StringType)
	hudi.InputFormat = types.StringValue("org.apache.hudi.hadoop.HoodieParquetInputFormat")
	hudi.OutputFormat = types.StringValue("org.apache.hadoop.hive.ql.io.parquet.MapredParquetOutputFormat")
	prior := externalTableTestModel()
	changed := prior
	changed.Columns = externalTableTestColumns("label", "varchar(64)", "amount", "decimal(8,2)")
	changed.Location = types.StringValue("s3://example-bucket/events-v2/")
	changed.StoredAs = types.StringValue("SEQUENCEFILE")
	changed.InputFormat, changed.OutputFormat = types.StringUnknown(), types.StringUnknown()
	changed.TableProperties = externalTableTestMap("skip.header.line.count", "2")
	runTranscripts(t, "spectrum/external_table", newExternalTableResource, []transcriptCase{
		{name: "create_json_serde", operation: "create", catalog: catalogWith(empty), planned: jsonTable},
		{name: "create_input_format", operation: "create", catalog: catalogWith(empty), planned: hudi},
		{name: "update_in_place", operation: "update", catalog: catalogWith(), prior: prior, planned: changed},
	})
}

// TestExternalTablePriorProperties starts a null state from the configured keys' catalog values, and from nothing
// without a recorded catalog.
func TestExternalTablePriorProperties(t *testing.T) {
	catalog := map[string]string{"EXTERNAL": "TRUE", "compression_type": "gzip"}
	plan := externalTableTestMap("compression_type", "gzip", "numRows", "3")
	assert.Equal(t, map[string]string{"numRows": "1"}, externalTablePriorProperties(externalTableTestMap("numRows", "1"), plan, catalog))
	assert.Empty(t, externalTablePriorProperties(types.MapNull(types.StringType), plan, nil))
	assert.Equal(t, map[string]string{"compression_type": "gzip"}, externalTablePriorProperties(types.MapNull(types.StringType), plan, catalog))
}

// TestExternalTableReadsFoldedSchemaName finds a table whose schema is configured with uppercase letters, which
// Redshift folds to lowercase even in quoted identifiers.
func TestExternalTableReadsFoldedSchemaName(t *testing.T) {
	model := externalTableTestModel()
	model.Schema = types.StringValue("Example_External")
	observed, err := externalTableState(t, fullCatalog(), "read", model, model)
	require.NoError(t, err)
	assert.Equal(t, "Example_External", observed.Schema.ValueString())
	assert.Equal(t, model.Columns, observed.Columns)
}

// externalTableProviderConfig renders the representative table with an optional table_properties argument.
func externalTableProviderConfig(properties string) string {
	return fmt.Sprintf(`
provider "redshift" {
  region         = "eu-central-1"
  workgroup_name = "warehouse"
  database       = "admin"
}
resource "redshift_external_table" "events" {
  database        = "admin"
  schema          = "example_external"
  name            = "events"
  field_delimiter = ","
  stored_as       = "TEXTFILE"
  location        = "s3://example-bucket/events/"
  %s

  column {
    name = "id"
    type = "INTEGER"
  }
  column {
    name = "label"
    type = "VARCHAR(64)"
  }
  partition_key {
    name = "event_date"
    type = "DATE"
  }
}`, properties)
}

// externalTableOrderConfig renders an unpartitioned table with optional table_properties and one column block per
// name/type pair.
func externalTableOrderConfig(storedAs, properties string, pairs ...string) string {
	var columns strings.Builder
	for i := 0; i+1 < len(pairs); i += 2 {
		fmt.Fprintf(&columns, "\n  column {\n    name = %q\n    type = %q\n  }", pairs[i], pairs[i+1])
	}
	return fmt.Sprintf(`
provider "redshift" {
  region         = "eu-central-1"
  workgroup_name = "warehouse"
  database       = "admin"
}
resource "redshift_external_table" "events" {
  database  = "admin"
  schema    = "example_external"
  name      = "events"
  stored_as = %q
  location  = "s3://example-bucket/events/"
  %s
%s
}`, storedAs, properties, columns.String())
}

// TestExternalTableObservedColumnOrder keeps the prior block order for tables that map columns by name, appending
// columns the prior blocks do not declare, and reports the catalog order otherwise.
func TestExternalTableObservedColumnOrder(t *testing.T) {
	catalog := &externalTableCatalog{
		table:      sqlclient.Row{"tablename": "events", "location": "s3://bucket/events/", "input_format": externalTableFileFormats["ORC"]},
		columns:    []externalTableCatalogColumn{{"id", "integer", 1}, {"label", "varchar(64)", 2}, {"amount", "decimal(8,2)", 3}, {"extra", "date", 4}},
		parameters: map[string]string{},
	}
	prior := externalTableTestModel()
	prior.StoredAs, prior.FieldDelimiter, prior.TableProperties = types.StringValue("ORC"), types.StringNull(), types.MapNull(types.StringType)
	prior.Columns = externalTableTestColumns("Amount", "numeric(8,2)", "id", "int4", "gone", "date", "label", "varchar(64)")
	observed := observeExternalTable(catalog, prior, externalTableRefresh)
	assert.Equal(t, externalTableTestColumns("Amount", "numeric(8,2)", "id", "int4", "label", "varchar(64)", "extra", "date"), observed.Columns)
	catalog.parameters["orc.schema.resolution"] = "position"
	observed = observeExternalTable(catalog, prior, externalTableRefresh)
	assert.Equal(t, externalTableTestColumns("id", "int4", "label", "varchar(64)", "Amount", "numeric(8,2)", "extra", "date"), observed.Columns)
	catalog.table["input_format"] = externalTableFileFormats["PARQUET"]
	delete(catalog.parameters, "orc.schema.resolution")
	observed = observeExternalTable(catalog, prior, externalTableRefresh)
	assert.Equal(t, externalTableTestColumns("Amount", "numeric(8,2)", "id", "int4", "label", "varchar(64)", "extra", "date"), observed.Columns, "Parquet maps by name")
}

// TestExternalTableValidatesKnownOptionsWithUnknownColumns checks known format and location options while the columns are still unknown.
func TestExternalTableValidatesKnownOptionsWithUnknownColumns(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*externalTableModel)
		want   string
	}{
		{"serde delimiter conflict", func(m *externalTableModel) {
			m.Serde = types.StringValue("custom.Serde")
			m.FieldDelimiter = types.StringValue(",")
		}, "serde conflicts"},
		{"invalid delimiter", func(m *externalTableModel) { m.FieldDelimiter = types.StringValue("long delimiter") }, "delimiter"},
		{"invalid location", func(m *externalTableModel) { m.Location = types.StringValue("file:///local") }, "s3://"},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := externalTableTestModel()
			model.Columns = types.ListUnknown(externalTableColumnType)
			test.change(&model)
			r := &externalTableResource{}
			var response resource.ValidateConfigResponse
			r.ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config(testState(t, r, model))}, &response)
			require.True(t, response.Diagnostics.HasError())
			assert.Contains(t, response.Diagnostics.Errors()[0].Detail(), test.want)
		})
	}
}

// TestExternalTableObservationEdges covers empty layouts, unnamed SerDe properties, unmatched physical columns, renamed tables, and format drift.
func TestExternalTableObservationEdges(t *testing.T) {
	ctx := context.Background()
	assert.Empty(t, recordExternalTableLayout(ctx, nil, nil))
	model := externalTableTestModel()
	model.SerdeProperties = externalTableTestMap("", "value")
	_, err := externalTableSpecFrom(model)
	require.ErrorContains(t, err, "nonempty names")
	layout := &externalTableLayout{Columns: []string{"missing"}}
	_, ok := layout.physical([]externalTableColumnValue{{Name: types.StringValue("id"), Type: types.StringValue("integer")}})
	assert.False(t, ok)
	prior := externalTableTestModel()
	observed := observeExternalTable(&externalTableCatalog{table: map[string]string{"tablename": "renamed", "location": "s3://bucket/path"}}, prior, externalTableLookup)
	assert.Equal(t, "renamed", observed.Name.ValueString())
	prior.StoredAs = types.StringNull()
	observed = prior
	prior.InputFormat = types.StringValue("custom.Input")
	observed.InputFormat = types.StringValue("different.Input")
	assert.Contains(t, externalTableDivergence(prior, observed), "input_format")
}
