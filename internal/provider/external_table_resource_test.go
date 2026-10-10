package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	testresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
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
	"column":           replaceConditional("TestExternalTableColumnsReplacement"),
	"partition_key":    replaceConditional("TestExternalTablePartitionKeysReplacement"),
	"field_delimiter":  replaceAlways,
	"line_delimiter":   replaceAlways,
	"serde":            replaceAlways,
	"serde_properties": replaceAlways,
	"stored_as":        replaceConditional("TestExternalTableStoredAsReplacement"),
	"input_format":     replaceAlways,
	"output_format":    replaceAlways,
	"location":         replaceNever,
	"table_properties": replaceConditional("TestExternalTablePropertiesReplacement"),
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
func externalTableTestResource(t *testing.T, client dataapi.Client) *externalTableResource {
	t.Helper()
	r := &externalTableResource{}
	configureTestResource(t, r, client)
	return r
}

// externalTableState runs one lifecycle operation and decodes the resulting state.
func externalTableState(t *testing.T, client dataapi.Client, operation string, prior, planned externalTableModel) (externalTableModel, error) {
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
	assert.Equal(t, expected.Columns, observed.Columns)
	assert.Equal(t, expected.PartitionKeys, observed.PartitionKeys)
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
		table:           dataapi.Row{"tablename": "events", "location": "s3://bucket/json", "input_format": "org.example.CustomInput", "output_format": "org.example.CustomOutput", "serialization_lib": "org.openx.data.jsonserde.JsonSerDe"},
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
		`ALTER TABLE "example_external"."events" ADD COLUMN "amount" decimal(8, 2)`,
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
		client := queryFunc(func(context.Context, dataapi.Connection, string, map[string]string) ([]dataapi.Row, error) {
			calls++
			return nil, nil
		})
		_, err := externalTableState(t, client, "create", invalid, invalid)
		require.ErrorContains(t, err, "stored_as conflicts with input_format")
		assert.Zero(t, calls)
	})
	rows := func(table, columns []dataapi.Row) dataapi.Client {
		return queryFunc(func(_ context.Context, _ dataapi.Connection, sql string, _ map[string]string) ([]dataapi.Row, error) {
			if strings.Contains(sql, "svv_external_tables") {
				return table, nil
			}
			return columns, nil
		})
	}
	table := dataapi.Row{"tablename": "events", "location": "s3://example-bucket/events/", "serde_parameters": "{}", "parameters": "{}"}
	column := dataapi.Row{"columnname": "id", "external_type": "int", "columnnum": "1", "part_key": "0"}
	for name, test := range map[string]struct {
		client dataapi.Client
		err    string
	}{
		"ambiguous":     {rows([]dataapi.Row{table, table}, nil), "ambiguous"},
		"no columns":    {rows([]dataapi.Row{table}, nil), "no columns"},
		"bad column":    {rows([]dataapi.Row{table}, []dataapi.Row{{"columnname": "id", "columnnum": "?", "part_key": "0"}}), "incomplete catalog metadata"},
		"bad serde":     {rows([]dataapi.Row{{"tablename": "events", "serde_parameters": "{"}}, []dataapi.Row{column}), "SerDe parameters"},
		"bad parameter": {rows([]dataapi.Row{{"tablename": "events", "parameters": "["}}, []dataapi.Row{column}), "decode external table parameters"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := externalTableState(t, test.client, "read", model, model)
			require.ErrorContains(t, err, test.err)
		})
	}
	t.Run("create does not converge", func(t *testing.T) {
		c := fullCatalog()
		delete(fakeState[*externalTableFamily](c, "external_table").tables, "events")
		drifting := queryFunc(func(ctx context.Context, target dataapi.Connection, sql string, parameters map[string]string) ([]dataapi.Row, error) {
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
		"insert":                      {storedAs: "PARQUET", columns: insert, replace: true},
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
		"text to parquet": {types.StringValue("TEXTFILE"), types.StringValue("PARQUET"), false},
		"case only":       {types.StringValue("orc"), types.StringValue("ORC"), false},
		"to orc":          {types.StringValue("TEXTFILE"), types.StringValue("ORC"), true},
		"to input format": {types.StringValue("PARQUET"), types.StringNull(), true},
		"from import":     {types.StringNull(), types.StringValue("PARQUET"), true},
		"unknown":         {types.StringValue("PARQUET"), types.StringUnknown(), true},
	} {
		t.Run(name, func(t *testing.T) {
			resp := stringplanmodifier.RequiresReplaceIfFuncResponse{}
			externalTableStoredAsReplace(context.Background(), planmodifier.StringRequest{StateValue: test.prior, PlanValue: test.plan}, &resp)
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
    type = "integer"
  }
  column {
    name = "label"
    type = "varchar(64)"
  }
  partition_key {
    name = "event_date"
    type = "date"
  }
}`, properties)
}

// TestExternalTablePropertiesPlan runs Terraform so the plan sees the catalog properties recorded in private state:
// adding a create-only property the catalog lacks must plan a replacement, not an update that fails, while an
// imported table keeps the catalog's matching properties in place.
func TestExternalTablePropertiesPlan(t *testing.T) {
	const address = "redshift_external_table.events"
	gzip := `table_properties = { compression_type = "gzip" }`
	providers := func(c *catalog) map[string]func() (tfprotov6.ProviderServer, error) {
		return map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(&redshiftProvider{version: "test", client: c})}
	}
	expect := func(action plancheck.ResourceActionType) testresource.ConfigPlanChecks {
		return testresource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, action)}}
	}
	parameter := func(c *catalog, key, value string) testresource.TestCheckFunc {
		return func(*terraform.State) error {
			// fakeState locks the catalog itself, so the table is looked up before reading it under the lock.
			table := externalTableFakeOf(c)
			c.mu.Lock()
			defer c.mu.Unlock()
			assert.Equal(t, value, table.parameters[key])
			return nil
		}
	}
	t.Run("created without properties", func(t *testing.T) {
		c := fullCatalog()
		delete(fakeState[*externalTableFamily](c, "external_table").tables, "events")
		testresource.UnitTest(t, testresource.TestCase{
			ProtoV6ProviderFactories: providers(c),
			Steps: []testresource.TestStep{
				{Config: externalTableProviderConfig("")},
				{Config: externalTableProviderConfig(gzip), ConfigPlanChecks: expect(plancheck.ResourceActionDestroyBeforeCreate), Check: parameter(c, "compression_type", "gzip")},
				{Config: externalTableProviderConfig(gzip), PlanOnly: true},
			},
		})
	})
	for name, test := range map[string]struct {
		properties string
		action     plancheck.ResourceActionType
		key, value string
	}{
		// Only the catalog values recorded at import tell the matching compression_type apart from an addition.
		"imported, settable change": {`table_properties = { compression_type = "gzip", numRows = "7" }`, plancheck.ResourceActionUpdate, "numRows", "7"},
		"imported, create-only":     {`table_properties = { compression_type = "snappy" }`, plancheck.ResourceActionDestroyBeforeCreate, "compression_type", "snappy"},
	} {
		t.Run(name, func(t *testing.T) {
			c := fullCatalog()
			externalTableFakeOf(c).parameters["compression_type"] = "gzip"
			testresource.UnitTest(t, testresource.TestCase{
				ProtoV6ProviderFactories: providers(c),
				Steps: []testresource.TestStep{
					{
						Config: externalTableProviderConfig(""), ResourceName: address, ImportState: true, ImportStatePersist: true,
						ImportStateId: `{"workgroup_name":"warehouse","database":"admin","schema":"example_external","name":"events"}`,
					},
					{Config: externalTableProviderConfig(""), PlanOnly: true},
					{Config: externalTableProviderConfig(test.properties), ConfigPlanChecks: expect(test.action), Check: parameter(c, test.key, test.value)},
					{Config: externalTableProviderConfig(test.properties), PlanOnly: true},
				},
			})
		})
	}
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

// TestExternalTableColumnOrderPlan runs Terraform over block edits: an ORC table that maps columns by name adds and
// drops columns anywhere in place and keeps the configured order in state although the catalog appends, while a
// positional table, including an ORC table whose catalog says position, replaces for the same edits. Switching to
// position mapping stays in place only while the blocks follow the catalog order.
func TestExternalTableColumnOrderPlan(t *testing.T) {
	const address = "redshift_external_table.events"
	providers := func(c *catalog) map[string]func() (tfprotov6.ProviderServer, error) {
		return map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(&redshiftProvider{version: "test", client: c})}
	}
	expect := func(action plancheck.ResourceActionType) testresource.ConfigPlanChecks {
		return testresource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, action)}}
	}
	// catalogColumns checks the physical column order and the statements since the previous check.
	catalogColumns := func(c *catalog, writes []string, names ...string) testresource.TestCheckFunc {
		return func(*terraform.State) error {
			table := externalTableFakeOf(c)
			c.mu.Lock()
			defer c.mu.Unlock()
			var physical []string
			for _, column := range table.columns {
				physical = append(physical, column.name)
			}
			assert.Equal(t, names, physical)
			assert.Equal(t, writes, c.writes)
			c.writes = nil
			return nil
		}
	}
	empty := func() *catalog {
		c := fullCatalog()
		delete(fakeState[*externalTableFamily](c, "external_table").tables, "events")
		return c
	}
	alter := `ALTER TABLE "example_external"."events" `
	t.Run("orc maps by name", func(t *testing.T) {
		c := empty()
		created := externalTableOrderConfig("ORC", "", "id", "integer", "label", "varchar(64)")
		inserted := externalTableOrderConfig("ORC", "", "id", "integer", "amount", "decimal(8,2)", "label", "varchar(64)")
		reordered := externalTableOrderConfig("ORC", "", "label", "varchar(64)", "amount", "decimal(8,2)", "id", "integer")
		dropped := externalTableOrderConfig("ORC", "", "label", "varchar(64)", "id", "integer")
		testresource.UnitTest(t, testresource.TestCase{
			ProtoV6ProviderFactories: providers(c),
			Steps: []testresource.TestStep{
				{Config: created, Check: catalogColumns(c, []string{`CREATE EXTERNAL TABLE "example_external"."events" ("id" integer, "label" varchar(64)) STORED AS ORC LOCATION 's3://example-bucket/events/'`}, "id", "label")},
				{Config: created, PlanOnly: true},
				{Config: inserted, ConfigPlanChecks: expect(plancheck.ResourceActionUpdate), Check: testresource.ComposeTestCheckFunc(
					catalogColumns(c, []string{alter + `ADD COLUMN "amount" decimal(8, 2)`}, "id", "label", "amount"),
					testresource.TestCheckResourceAttr(address, "column.1.name", "amount"),
				)},
				{Config: inserted, PlanOnly: true},
				{Config: reordered, ConfigPlanChecks: expect(plancheck.ResourceActionUpdate), Check: testresource.ComposeTestCheckFunc(
					catalogColumns(c, nil, "id", "label", "amount"),
					testresource.TestCheckResourceAttr(address, "column.0.name", "label"),
				)},
				{Config: reordered, PlanOnly: true},
				{Config: dropped, ConfigPlanChecks: expect(plancheck.ResourceActionUpdate), Check: catalogColumns(c, []string{alter + `DROP COLUMN "amount"`}, "id", "label")},
				{Config: dropped, PlanOnly: true},
			},
		})
	})
	t.Run("textfile maps by position", func(t *testing.T) {
		inserted := externalTableOrderConfig("TEXTFILE", "", "id", "integer", "amount", "decimal(8,2)", "label", "varchar(64)")
		testresource.UnitTest(t, testresource.TestCase{
			ProtoV6ProviderFactories: providers(empty()),
			Steps: []testresource.TestStep{
				{Config: externalTableOrderConfig("TEXTFILE", "", "id", "integer", "label", "varchar(64)")},
				{Config: inserted, ConfigPlanChecks: expect(plancheck.ResourceActionDestroyBeforeCreate)},
				{Config: inserted, PlanOnly: true},
			},
		})
	})
	t.Run("imported orc by position", func(t *testing.T) {
		c := fullCatalog()
		table := externalTableFakeOf(c)
		table.inputFormat, table.serde, table.partitionKeys, table.partitions = externalTableFileFormats["ORC"], externalTableImpliedSerdes["ORC"], nil, nil
		table.serdeParameters, table.parameters = map[string]string{}, map[string]string{"EXTERNAL": "TRUE", "orc.schema.resolution": "position"}
		imported := externalTableOrderConfig("ORC", "", "id", "integer", "label", "varchar(64)")
		testresource.UnitTest(t, testresource.TestCase{
			ProtoV6ProviderFactories: providers(c),
			Steps: []testresource.TestStep{
				{
					Config: imported, ResourceName: address, ImportState: true, ImportStatePersist: true,
					ImportStateId: `{"workgroup_name":"warehouse","database":"admin","schema":"example_external","name":"events"}`,
				},
				{Config: imported, PlanOnly: true},
				{Config: externalTableOrderConfig("ORC", "", "label", "varchar(64)", "id", "integer"), ConfigPlanChecks: expect(plancheck.ResourceActionDestroyBeforeCreate)},
			},
		})
	})
	position := `table_properties = { "orc.schema.resolution" = "position" }`
	create := `CREATE EXTERNAL TABLE "example_external"."events" ("id" integer, "label" varchar(64)) STORED AS ORC LOCATION 's3://example-bucket/events/'`
	t.Run("orc switching to position out of catalog order", func(t *testing.T) {
		c := empty()
		switched := externalTableOrderConfig("ORC", position, "id", "integer", "amount", "decimal(8,2)", "label", "varchar(64)")
		testresource.UnitTest(t, testresource.TestCase{
			ProtoV6ProviderFactories: providers(c),
			Steps: []testresource.TestStep{
				{Config: externalTableOrderConfig("ORC", "", "id", "integer", "label", "varchar(64)"), Check: catalogColumns(c, []string{create}, "id", "label")},
				{Config: externalTableOrderConfig("ORC", "", "id", "integer", "amount", "decimal(8,2)", "label", "varchar(64)"), Check: catalogColumns(c, []string{alter + `ADD COLUMN "amount" decimal(8, 2)`}, "id", "label", "amount")},
				{Config: switched, ConfigPlanChecks: expect(plancheck.ResourceActionDestroyBeforeCreate), Check: catalogColumns(c, []string{
					`DROP TABLE "example_external"."events"`,
					`CREATE EXTERNAL TABLE "example_external"."events" ("id" integer, "amount" decimal(8, 2), "label" varchar(64)) STORED AS ORC LOCATION 's3://example-bucket/events/' TABLE PROPERTIES ('orc.schema.resolution' = 'position')`,
				}, "id", "amount", "label")},
				{Config: switched, PlanOnly: true},
			},
		})
	})
	t.Run("orc switching to position in catalog order", func(t *testing.T) {
		c := empty()
		switched := externalTableOrderConfig("ORC", position, "id", "integer", "label", "varchar(64)", "amount", "decimal(8,2)")
		testresource.UnitTest(t, testresource.TestCase{
			ProtoV6ProviderFactories: providers(c),
			Steps: []testresource.TestStep{
				{Config: externalTableOrderConfig("ORC", "", "id", "integer", "label", "varchar(64)"), Check: catalogColumns(c, []string{create}, "id", "label")},
				{Config: externalTableOrderConfig("ORC", "", "id", "integer", "amount", "decimal(8,2)", "label", "varchar(64)"), Check: catalogColumns(c, []string{alter + `ADD COLUMN "amount" decimal(8, 2)`}, "id", "label", "amount")},
				{Config: switched, ConfigPlanChecks: expect(plancheck.ResourceActionUpdate), Check: catalogColumns(c, []string{alter + `SET TABLE PROPERTIES ('orc.schema.resolution' = 'position')`}, "id", "label", "amount")},
				{Config: switched, PlanOnly: true},
			},
		})
	})
	// The catalog's mapping still decides once table_properties is configured without orc.schema.resolution.
	for name, edited := range map[string][]string{
		"reorder": {"label", "varchar(64)", "id", "integer"},
		"insert":  {"id", "integer", "amount", "decimal(8,2)", "label", "varchar(64)"},
	} {
		t.Run("imported orc by position with managed properties, "+name, func(t *testing.T) {
			c := fullCatalog()
			table := externalTableFakeOf(c)
			table.inputFormat, table.serde, table.partitionKeys, table.partitions = externalTableFileFormats["ORC"], externalTableImpliedSerdes["ORC"], nil, nil
			table.serdeParameters, table.parameters = map[string]string{}, map[string]string{"EXTERNAL": "TRUE", "orc.schema.resolution": "position"}
			managed := `table_properties = { numRows = "7" }`
			testresource.UnitTest(t, testresource.TestCase{
				ProtoV6ProviderFactories: providers(c),
				Steps: []testresource.TestStep{
					{
						Config: externalTableOrderConfig("ORC", "", "id", "integer", "label", "varchar(64)"), ResourceName: address, ImportState: true, ImportStatePersist: true,
						ImportStateId: `{"workgroup_name":"warehouse","database":"admin","schema":"example_external","name":"events"}`,
					},
					{Config: externalTableOrderConfig("ORC", managed, "id", "integer", "label", "varchar(64)"), ConfigPlanChecks: expect(plancheck.ResourceActionUpdate)},
					{Config: externalTableOrderConfig("ORC", managed, edited...), ConfigPlanChecks: expect(plancheck.ResourceActionDestroyBeforeCreate)},
				},
			})
		})
	}
	t.Run("orc set to position outside terraform", func(t *testing.T) {
		c := empty()
		testresource.UnitTest(t, testresource.TestCase{
			ProtoV6ProviderFactories: providers(c),
			Steps: []testresource.TestStep{
				{Config: externalTableOrderConfig("ORC", "", "id", "integer", "label", "varchar(64)")},
				{
					PreConfig: func() {
						table := externalTableFakeOf(c)
						c.mu.Lock()
						defer c.mu.Unlock()
						table.parameters["orc.schema.resolution"] = "position"
					},
					Config: externalTableOrderConfig("ORC", "", "label", "varchar(64)", "id", "integer"), ConfigPlanChecks: expect(plancheck.ResourceActionDestroyBeforeCreate),
				},
			},
		})
	})
}

// TestExternalTableObservedColumnOrder keeps the prior block order for tables that map columns by name, appending
// columns the prior blocks do not declare, and reports the catalog order otherwise.
func TestExternalTableObservedColumnOrder(t *testing.T) {
	catalog := &externalTableCatalog{
		table:      dataapi.Row{"tablename": "events", "location": "s3://bucket/events/", "input_format": externalTableFileFormats["ORC"]},
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
	assert.Equal(t, externalTableTestColumns("id", "int4", "label", "varchar(64)", "Amount", "numeric(8,2)", "extra", "date"), observed.Columns, "Parquet maps by position")
}
