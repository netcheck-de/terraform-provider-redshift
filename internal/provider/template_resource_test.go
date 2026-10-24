package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerLifecycleCase(lifecycleCase{
	name: "template", new: newTemplateResource, model: templateLifecycleModel,
	absent: func(c *catalog) { fakeState[*templateFake](c, templateFakeFamily).exists = false },
})

var _ = registerReplacementPolicy("redshift_template", map[string]replaceRule{
	"database":        replaceAlways,
	"schema":          replaceAlways,
	"name":            replaceNever,
	"owner":           replaceNever,
	"format":          replaceNever,
	"format_argument": replaceNever,
	"parameters":      replaceNever,
})

var _ = registerValidateConfigCase("template", validateConfigCase{
	new:     newTemplateResource,
	valid:   templateLifecycleModel,
	invalid: templateTestModel("csv_load", "CSV", "", map[string]string{"MANIFEST": ""}),
	unknown: func() templateModel {
		model := templateTestModel("csv_load", "", "", nil)
		model.Parameters = types.MapUnknown(types.StringType)

		return model
	}(),
})

// templateFakeChange returns a catalog change applied to the template fake.
func templateFakeChange(change func(*templateFake)) func(*catalog) {
	return func(c *catalog) { change(fakeState[*templateFake](c, templateFakeFamily)) }
}

// TestTemplateTranscripts records renames, parameter and format changes, drift, a missing schema, and import.
func TestTemplateTranscripts(t *testing.T) {
	renamed := templateLifecycleModel
	renamed.Name = types.StringValue("csv_import")
	changed := templateTestModel("csv_load", "CSV", "", map[string]string{"IGNOREHEADER": "2", "GZIP": "", "NULL": ""})
	json := templateTestModel("csv_load", "JSON", "auto ignorecase", map[string]string{"GZIP": ""})
	json.Owner = types.StringValue("loader")
	prior := templateLifecycleModel
	prior.Owner = types.StringValue("admin")
	absent := templateFakeChange(func(f *templateFake) { f.exists = false })
	runTranscripts(t, "template/flows", newTemplateResource, []transcriptCase{
		{name: "update_rename", operation: "update", catalog: catalogWith(), prior: prior, planned: renamed},
		{name: "update_parameters", operation: "update", catalog: catalogWith(), prior: prior, planned: changed},
		{name: "update_format_and_owner", operation: "update", catalog: catalogWith(), prior: prior, planned: json},
		{name: "read_drift", operation: "read", catalog: catalogWith(templateFakeChange(func(f *templateFake) {
			f.owner = "loader"
			f.definition = copyDefinition{format: "JSON", formatArgument: "auto", parameters: map[string]string{"MAXERROR": "10"}}
		})), prior: prior},
		{name: "create_quoted", operation: "create", catalog: catalogWith(absent), planned: templateTestModel(`it's`, "", "", map[string]string{"NULL": `\N`, "DELIMITER": `'`})},
		{name: "import_without_configuration", operation: "import", catalog: catalogWith(absent), planned: templateLifecycleModel},
		{name: "delete_missing", operation: "delete", catalog: catalogWith(absent), prior: prior},
	})
}

// TestTemplateRenameRecordedWhenLaterStepFails keeps the new name in state when a statement after the rename fails, so
// the next refresh finds the renamed template instead of dropping it from state.
func TestTemplateRenameRecordedWhenLaterStepFails(t *testing.T) {
	prior := templateLifecycleModel
	prior.Owner = types.StringValue("admin")
	planned := prior
	planned.Name, planned.Owner = types.StringValue("csv_import"), types.StringValue("loader")
	c := catalogWith()()
	r := newTemplateResource().(*templateResource)
	configureTestResource(t, r, queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		if strings.Contains(sql, " OWNER TO ") {
			return nil, errors.New("user loader does not exist")
		}

		return c.Query(ctx, target, sql, parameters)
	}))
	prior.ID = r.identity("admin", map[string]string{"schema": prior.Schema.ValueString(), "name": prior.Name.ValueString()})
	state, diagnostics := applyOperation(t, r, "update", prior, planned, nil)
	require.True(t, diagnostics.HasError())

	var recorded templateModel
	require.False(t, state.Get(context.Background(), &recorded).HasError())
	assert.Equal(t, "csv_import", recorded.Name.ValueString())
	assert.Equal(t, r.identity("admin", map[string]string{"schema": prior.Schema.ValueString(), "name": "csv_import"}), recorded.ID)
	assert.Equal(t, "admin", recorded.Owner.ValueString(), "the failed owner change is not recorded")
	refreshed, diagnostics := applyOperation(t, r, "read", recorded, nil, nil)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.False(t, refreshed.Raw.IsNull(), "refresh finds the renamed template")
}

// TestTemplateReadReconciles keeps configured spellings the catalog reports canonically, surfaces outside changes,
// and removes state when the schema is gone.
func TestTemplateReadReconciles(t *testing.T) {
	read := func(t *testing.T, c *catalog, model templateModel) (templateModel, bool) {
		t.Helper()

		r := newTemplateResource()
		configureTestResource(t, r, c)
		state := testState(t, r, model)
		resp := resource.ReadResponse{State: state}
		r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)
		require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)

		if resp.State.Raw.IsNull() {
			return templateModel{}, false
		}

		var observed templateModel
		require.False(t, resp.State.Get(context.Background(), &observed).HasError())

		return observed, true
	}
	spelled := templateTestModel("csv_load", "csv", "", map[string]string{"IGNOREHEADER": "01", "DELIMITER": ","})
	observed, found := read(t, fullCatalog(), spelled)
	require.True(t, found)
	assert.Equal(t, "csv", observed.Format.ValueString(), "another case of the format keyword is kept")
	assert.True(t, observed.Parameters.Equal(spelled.Parameters), "equivalent parameter spellings are kept")
	assert.Equal(t, "admin", observed.Owner.ValueString())

	partly := templateTestModel("csv_load", "CSV", "", map[string]string{"IGNOREHEADER": "01", "DELIMITER": ";"})
	observed, found = read(t, fullCatalog(), partly)
	require.True(t, found)
	assert.Equal(t, map[string]string{"IGNOREHEADER": "01", "DELIMITER": ","}, knownMap(observed.Parameters), "changed values surface; equal ones keep their spelling")

	empty := fullCatalog()
	fakeState[*templateFake](empty, templateFakeFamily).definition = copyDefinition{format: "CSV", parameters: map[string]string{}}
	observed, found = read(t, empty, templateTestModel("csv_load", "CSV", "", nil))
	require.True(t, found)
	assert.True(t, observed.Parameters.IsNull(), "no parameters keep an unset map")

	missing := fullCatalog()
	fakeState[*templateFake](missing, templateFakeFamily).schemaMissing = true
	_, found = read(t, missing, templateLifecycleModel)
	assert.False(t, found, "a dropped schema removes the template from state")
}

// TestTemplateCatalogFailures reports unreadable or ambiguous catalog output instead of guessing.
func TestTemplateCatalogFailures(t *testing.T) {
	respond := func(templates []sqlclient.Row, definitions []sqlclient.Row) sqlclient.Client {
		return queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
			switch {
			case strings.HasPrefix(sql, "SELECT schema_name"):
				return []sqlclient.Row{{"schema_name": templateFakeSchema}}, nil
			case strings.HasPrefix(sql, "SHOW TEMPLATES"):
				return templates, nil
			case strings.HasPrefix(sql, "SHOW TEMPLATE "):
				return definitions, nil
			}

			return nil, fmt.Errorf("unexpected SQL %q", sql)
		})
	}
	row := sqlclient.Row{"template_name": "csv_load", "template_owner": "admin"}

	good := sqlclient.Row{"statement": "CREATE OR REPLACE TEMPLATE admin.serving.csv_load FOR COPY AS CSV"}
	for name, client := range map[string]sqlclient.Client{
		"ambiguous":         respond([]sqlclient.Row{row, row}, []sqlclient.Row{good}),
		"no definition":     respond([]sqlclient.Row{row}, nil),
		"two definitions":   respond([]sqlclient.Row{row}, []sqlclient.Row{good, good}),
		"unknown parameter": respond([]sqlclient.Row{row}, []sqlclient.Row{{"statement": "CREATE OR REPLACE TEMPLATE x FOR COPY AS SOMETHING_NEW"}}),
		"no statement":      respond([]sqlclient.Row{row}, []sqlclient.Row{{"a": "1", "b": "2"}}),
	} {
		t.Run(name, func(t *testing.T) {
			r := newTemplateResource()
			configureTestResource(t, r, client)
			assert.True(t, invoke(t, r, "read", templateLifecycleModel).HasError())
		})
	}

	r := newTemplateResource()
	configureTestResource(t, r, respond([]sqlclient.Row{{"template_name": "csvxload", "template_owner": "admin"}}, nil))
	assert.False(t, invoke(t, r, "read", templateLifecycleModel).HasError(), "a name the LIKE pattern also matches is not the template")
}

// TestTemplateConvergence fails an apply whose catalog result differs from the plan.
func TestTemplateConvergence(t *testing.T) {
	for name, change := range map[string]func(*templateFake){
		"parameters": func(f *templateFake) { f.definition.parameters["GZIP"] = "" },
		"format":     func(f *templateFake) { f.definition.format = "JSON" },
		"owner":      func(f *templateFake) { f.owner = "someone" },
	} {
		t.Run(name, func(t *testing.T) {
			c := fullCatalog()
			client := queryFunc(func(ctx context.Context, connection sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
				rows, err := c.Query(ctx, connection, sql, parameters)
				if strings.HasPrefix(sql, "ALTER TEMPLATE") {
					change(fakeState[*templateFake](c, templateFakeFamily))
				}

				return rows, err
			})
			r := newTemplateResource()
			configureTestResource(t, r, client)

			prior := templateLifecycleModel
			prior.Owner = types.StringValue("admin")
			planned := prior
			planned.Owner = types.StringValue("loader")
			_, diagnostics := applyOperation(t, r, "update", prior, planned, nil)
			assert.True(t, diagnostics.HasError())
		})
	}

	r := newTemplateResource()
	configureTestResource(t, r, catalogWith(templateFakeChange(func(f *templateFake) { f.exists = false }))())
	_, diagnostics := applyOperation(t, r, "update", templateLifecycleModel, templateLifecycleModel, nil)
	assert.True(t, diagnostics.HasError(), "an update never recreates a template dropped since the plan")
}

// TestTemplateCreateRejectsInvalidBeforeState keeps invalid templates out of state and SQL.
func TestTemplateCreateRejectsInvalidBeforeState(t *testing.T) {
	r := newTemplateResource()
	configureTestResource(t, r, queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
		return nil, fmt.Errorf("unexpected SQL %q", sql)
	}))

	model := templateTestModel("csv_load", "CSV", "", map[string]string{"MAXERROR": "many"})
	resp := resource.CreateResponse{State: emptyState(t, r)}
	r.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan(testState(t, r, model))}, &resp)
	require.True(t, resp.Diagnostics.HasError())
	assert.True(t, resp.State.Raw.IsNull(), "an invalid template must not be recorded in state")
	assert.True(t, invoke(t, r, "update", model).HasError(), "update validates before ALTER")
}

// TestTemplateImport accepts the JSON identity Create records and rejects incomplete ones.
func TestTemplateImport(t *testing.T) {
	r := newTemplateResource()
	for id, valid := range map[string]bool{
		`{"workgroup_name":"warehouse","database":"admin","schema":"serving","name":"csv_load"}`: true,
		`{"workgroup_name":"warehouse","database":"admin","name":"csv_load"}`:                    false,
		`csv_load`: false,
	} {
		resp := resource.ImportStateResponse{State: emptyState(t, r)}
		r.(resource.ResourceWithImportState).ImportState(context.Background(), resource.ImportStateRequest{ID: id}, &resp)
		assert.Equal(t, valid, !resp.Diagnostics.HasError(), "%s: %v", id, resp.Diagnostics)

		if valid {
			var schemaName types.String
			require.False(t, resp.State.GetAttribute(context.Background(), path.Root("schema"), &schemaName).HasError())
			assert.Equal(t, templateFakeSchema, schemaName.ValueString())
		}
	}
}

// TestTemplateLifecycleEdges treats a missing database as absence, keeps the recorded identity, and reports a failed ALTER.
func TestTemplateLifecycleEdges(t *testing.T) {
	ctx := context.Background()
	r := &templateResource{testResourceClient(fullCatalog())}
	data := templateLifecycleModel
	data.Database = types.StringValue("missing")
	_, found, err := readTemplateCatalog(ctx, &r.resourceClient, data)
	require.NoError(t, err)
	assert.False(t, found)
	state := testState(t, r, templateLifecycleModel)
	response := planmodifier.StringResponse{PlanValue: types.StringUnknown()}
	templateIdentity{}.PlanModifyString(ctx, planmodifier.StringRequest{StateValue: types.StringValue("identity"), PlanValue: types.StringUnknown(), State: state, Plan: tfsdk.Plan(state)}, &response)
	require.False(t, response.Diagnostics.HasError())
	assert.Equal(t, types.StringValue("identity"), response.PlanValue)

	for _, operation := range []string{"create", "update"} {
		t.Run(operation, func(t *testing.T) {
			c := fullCatalog()
			if operation == "create" {
				fakeState[*templateFake](c, templateFakeFamily).exists = false
			}

			r := &templateResource{testResourceClient(queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
				if strings.HasPrefix(sql, "ALTER TEMPLATE") {
					return nil, errors.New("alter denied")
				}

				return c.Query(ctx, target, sql, parameters)
			}))}
			planned := templateLifecycleModel
			planned.Owner = types.StringValue("other_owner")
			_, diagnostics := applyOperation(t, r, operation, templateLifecycleModel, planned, nil)
			require.True(t, diagnostics.HasError())
			assert.Contains(t, diagnostics.Errors()[0].Detail(), "alter denied")
		})
	}
}
