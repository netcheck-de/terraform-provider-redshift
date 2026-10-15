package provider

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	testresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
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
			assert.True(t, invoke(t, r, "read", templateLifecycleModel, false).HasError())
		})
	}
	r := newTemplateResource()
	configureTestResource(t, r, respond([]sqlclient.Row{{"template_name": "csvxload", "template_owner": "admin"}}, nil))
	assert.False(t, invoke(t, r, "read", templateLifecycleModel, false).HasError(), "a name the LIKE pattern also matches is not the template")
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
	assert.True(t, invoke(t, r, "update", model, false).HasError(), "update validates before ALTER")
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

// TestTemplateRenamePlan drives real plans: a rename updates in place and records the new identity, and repeated plans
// after apply are empty.
func TestTemplateRenamePlan(t *testing.T) {
	c := &catalog{identity: true, privileges: map[string]bool{}}
	configuration := func(name, parameters string) string {
		return fmt.Sprintf(`
provider "redshift" {
  region = "eu-central-1"
  workgroup_name = "warehouse"
  database = "admin"
}
resource "redshift_template" "csv" {
  database = "admin"
  schema = "serving"
  name = %q
  format = "CSV"
  parameters = %s
}`, name, parameters)
	}
	first := configuration("csv_load", `{ IGNOREHEADER = "1" }`)
	renamed := configuration("csv_import", `{ IGNOREHEADER = "1", GZIP = "" }`)
	testresource.UnitTest(t, testresource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(&redshiftProvider{version: "test", client: c})},
		Steps: []testresource.TestStep{
			{Config: configuration("csv_load", `{ MANIFEST = "" }`), ExpectError: regexp.MustCompile(`unsupported COPY parameter`), PlanOnly: true},
			{Config: first, Check: testresource.TestCheckResourceAttr("redshift_template.csv", "owner", "admin")},
			{Config: first, PlanOnly: true},
			{
				Config: renamed,
				ConfigPlanChecks: testresource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("redshift_template.csv", plancheck.ResourceActionUpdate),
					plancheck.ExpectUnknownValue("redshift_template.csv", tfjsonpath.New("id")),
				}},
				Check: testresource.TestCheckResourceAttr("redshift_template.csv", "id", `{"database":"admin","name":"csv_import","schema":"serving","workgroup_name":"warehouse"}`),
			},
			{Config: renamed, PlanOnly: true},
		},
	})
	assert.Equal(t, "csv_import", fakeState[*templateFake](c, templateFakeFamily).name)
}
