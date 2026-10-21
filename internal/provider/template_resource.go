package provider

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// templateResource manages a COPY template.
type templateResource struct {
	// resourceClient provides SQL execution and warehouse ownership checks.
	resourceClient
}

// templateModel is the Terraform state of one template.
type templateModel struct {
	// ID records the warehouse/database/schema/name identity.
	ID types.String `tfsdk:"id"`
	// Database contains the template.
	Database types.String `tfsdk:"database"`
	// Schema contains the template.
	Schema types.String `tfsdk:"schema"`
	// Name identifies the template within its schema; a change renames it.
	Name types.String `tfsdk:"name"`
	// Owner is the template's SQL owner.
	Owner types.String `tfsdk:"owner"`
	// Format is the COPY data format keyword, or null for pipe-delimited text.
	Format types.String `tfsdk:"format"`
	// FormatArgument is the format's string argument, such as auto for JSON.
	FormatArgument types.String `tfsdk:"format_argument"`
	// Parameters maps UPPERCASE COPY parameter keywords to their arguments.
	Parameters types.Map `tfsdk:"parameters"`
}

var _ = registerResource(newTemplateResource)

// newTemplateResource constructs a template lifecycle handler.
func newTemplateResource() resource.Resource { return &templateResource{} }

// Metadata identifies the template resource to Terraform.
func (r *templateResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_template"
}

// templateIdentity keeps the planned import identity, which includes the name, unless the template is renamed in
// place; the identity is then known only after the rename, and a kept value would make Terraform reject the apply.
type templateIdentity struct{}

// Description explains the modifier in plans.
func (templateIdentity) Description(context.Context) string {
	return "Keeps the identity unless the template is renamed."
}

// MarkdownDescription explains the modifier in documentation.
func (m templateIdentity) MarkdownDescription(ctx context.Context) string { return m.Description(ctx) }

// PlanModifyString keeps the prior identity while the name stays the same.
func (templateIdentity) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if req.StateValue.IsNull() || !req.PlanValue.IsUnknown() {
		return
	}
	var prior, planned types.String
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("name"), &prior)...)
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("name"), &planned)...)
	if planned.Equal(prior) {
		resp.PlanValue = req.StateValue
	}
}

// Schema defines the template identity, its COPY options, and its owner.
func (r *templateResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	id := idAttribute()
	id.PlanModifiers = []planmodifier.String{templateIdentity{}}
	nonEmpty := []validator.String{stringvalidator.LengthAtLeast(1)}
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a reusable template of `COPY` options in a local schema.",
		Attributes: map[string]schema.Attribute{
			"id":       id,
			"database": schema.StringAttribute{Required: true, Validators: nonEmpty, PlanModifiers: replace, MarkdownDescription: "Local database containing the template; templates cannot be referenced from other databases. Changing it replaces the template."},
			"schema":   schema.StringAttribute{Required: true, Validators: nonEmpty, PlanModifiers: replace, MarkdownDescription: "Existing schema containing the template. Changing it replaces the template."},
			"name": schema.StringAttribute{
				Required: true, MarkdownDescription: "Template name, in lowercase. A change renames the template in place with `ALTER TEMPLATE ... RENAME TO`.",
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"owner": schema.StringAttribute{
				Optional: true, Computed: true,
				MarkdownDescription: "SQL user owning the template. When set, it is applied with `ALTER TEMPLATE ... OWNER TO`; when unset, the creating user owns the template and the attribute reports the catalog owner.",
				Validators:          nonEmpty,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"format": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Data format of `FORMAT AS`: `" + strings.Join(copyFormatNames(), "`, `") + "`. Omit it for pipe-delimited text. A change runs `CREATE OR REPLACE TEMPLATE`.",
				Validators:          []validator.String{stringvalidator.OneOfCaseInsensitive(copyFormatNames()...)},
			},
			"format_argument": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Argument of the format, required for `JSON` and `AVRO` (`auto`, `auto ignorecase`, `noshred`, or an `s3://` JSONPaths file) and `FIXEDWIDTH` (the column specification). For `SHAPEFILE`, optionally use `SIMPLIFY [AUTO] [tolerance]`, with a finite non-negative numeric tolerance. Rejected for other formats. A change runs `CREATE OR REPLACE TEMPLATE`.",
			},
			"parameters": schema.MapAttribute{
				Optional: true, ElementType: types.StringType,
				MarkdownDescription: "COPY parameters keyed by their UPPERCASE keyword, with `\"\"` for keywords without an argument, such as `{ IGNOREHEADER = \"1\", GZIP = \"\" }`. " +
					"Accepted: `" + strings.Join(copyParameterNames(false), "`, `") + "`. Text arguments are written as their value without quotes, " +
					"integers as digits, and `COMPUPDATE`, `STATUPDATE`, and `ENCODING` as their keyword. Source paths, credentials, and `MANIFEST` cannot be part of a template. " +
					"Changes run `ALTER TEMPLATE ... ADD`, `DROP`, or `SET` per parameter.",
			},
		},
	}
}

// templateCatalog is what refresh reads about one template.
type templateCatalog struct {
	// owner is the catalog owner name.
	owner string
	// definition is the parsed SHOW TEMPLATE statement.
	definition copyDefinition
}

// readTemplateCatalog reads one template from SHOW TEMPLATES and SHOW TEMPLATE in its database. A missing database,
// schema, or template is reported as not found, so refresh removes the state of a template whose parent is gone.
func readTemplateCatalog(ctx context.Context, client *resourceClient, data templateModel) (templateCatalog, bool, error) {
	database, schemaName, name := data.Database.ValueString(), data.Schema.ValueString(), data.Name.ValueString()
	if exists, err := client.localDatabaseExists(ctx, database); err != nil || !exists {
		return templateCatalog{}, false, err
	}
	if rows, err := client.selectRows(ctx, database, privilegeSchemaQuery(database, schemaName)); err != nil || len(rows) == 0 {
		return templateCatalog{}, false, err
	}
	rows, err := client.queryDatabase(ctx, database, showTemplatesStatement("", schemaName, name), nil)
	if err != nil {
		return templateCatalog{}, false, err
	}
	var matches []sqlclient.Row
	for _, row := range rows {
		if row["template_name"] == name {
			matches = append(matches, row)
		}
	}
	switch {
	case len(matches) == 0:
		return templateCatalog{}, false, nil
	case len(matches) > 1:
		return templateCatalog{}, false, fmt.Errorf("template %s.%s has ambiguous catalog metadata", schemaName, name)
	}
	definitions, err := client.queryDatabase(ctx, database, showTemplateStatement(data), nil)
	if err != nil {
		return templateCatalog{}, false, err
	}
	if len(definitions) != 1 {
		return templateCatalog{}, false, fmt.Errorf("SHOW TEMPLATE returned %d rows for %s.%s", len(definitions), schemaName, name)
	}
	text, err := templateDefinitionText(definitions[0])
	if err != nil {
		return templateCatalog{}, false, err
	}
	definition, err := parseTemplateDefinition(text)
	if err != nil {
		return templateCatalog{}, false, err
	}
	return templateCatalog{owner: matches[0]["template_owner"], definition: definition}, true, nil
}

// applyCatalog stores the catalog values in data, keeping configured spellings the catalog reports differently.
func (entry templateCatalog) applyCatalog(data *templateModel) {
	data.Owner = types.StringValue(entry.owner)
	configured := data.Format
	data.Format = types.StringNull()
	if entry.definition.format != "" {
		data.Format = keywordValue(configured, entry.definition.format)
	}
	data.FormatArgument = copyFormatArgumentValue(data.FormatArgument, entry.definition.format, entry.definition.formatArgument)
	data.Parameters = copyParametersValue(data.Parameters, entry.definition.parameters, false)
}

// read checks the binding and reads the template.
func (r *templateResource) read(ctx context.Context, data templateModel) (templateCatalog, bool, error) {
	if err := r.bound(data.ID, data.Database.ValueString()); err != nil {
		return templateCatalog{}, false, err
	}
	return readTemplateCatalog(ctx, &r.resourceClient, data)
}

// converge re-reads the template after apply and checks that the planned definition and a configured owner took
// effect; state then keeps the configured spellings.
func (r *templateResource) converge(ctx context.Context, data *templateModel) error {
	entry, found, err := r.read(ctx, *data)
	switch {
	case err != nil:
		return err
	case !found:
		return errors.New("the template is absent after apply")
	case knownString(data.Owner) != "" && entry.owner != data.Owner.ValueString():
		return fmt.Errorf("the template is owned by %q after apply, expected %q", entry.owner, data.Owner.ValueString())
	case !strings.EqualFold(entry.definition.format, knownString(data.Format)) || !copyFormatArgumentValue(data.FormatArgument, entry.definition.format, entry.definition.formatArgument).Equal(data.FormatArgument):
		return fmt.Errorf("the template's format is %q %q after apply, expected %q %q", entry.definition.format, entry.definition.formatArgument, knownString(data.Format), knownString(data.FormatArgument))
	}
	planned, _ := copyCanonicalParameters(knownMap(data.Parameters), false) // validated before apply.
	if !maps.Equal(planned, entry.definition.parameters) {
		return fmt.Errorf("the template's parameters differ from the configuration after apply: %v", entry.definition.parameters)
	}
	entry.applyCatalog(data)
	return nil
}

// ValidateConfig reports a definition Create would reject, during planning once the configuration is known.
func (r *templateResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data templateModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := templateValidate(data); err != nil {
		resp.Diagnostics.AddError("Invalid template", err.Error())
	}
}

// Create validates the definition, creates the template, applies a configured owner, and verifies the result.
func (r *templateResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data templateModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	statements, err := createTemplateStatements(data)
	if err != nil {
		resp.Diagnostics.AddError("Invalid template", err.Error())
		return
	}
	if err := r.exec(ctx, data.Database.ValueString(), statements[0]); err != nil {
		resp.Diagnostics.AddError("Create template", err.Error())
		return
	}
	data.ID = r.identity(data.Database.ValueString(), map[string]string{"schema": data.Schema.ValueString(), "name": data.Name.ValueString()})
	// A failed ownership change or verification must still record the created template in serializable state.
	if data.Owner.IsUnknown() {
		data.Owner = types.StringNull()
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	if err := r.exec(ctx, data.Database.ValueString(), statements[1:]...); err != nil {
		resp.Diagnostics.AddError("Set template owner", err.Error())
		return
	}
	if err := r.converge(ctx, &data); err != nil {
		resp.Diagnostics.AddError("Verify template", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Read refreshes the owner and definition, or removes a missing template.
func (r *templateResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data templateModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	entry, found, err := r.read(ctx, data)
	if err != nil {
		resp.Diagnostics.AddError("Read template", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	entry.applyCatalog(&data)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update renames, redefines, and transfers the template in place, then verifies the result.
func (r *templateResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data, previous templateModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &previous)...)
	if resp.Diagnostics.HasError() {
		return
	}
	statements, err := alterTemplateStatements(previous, data)
	if err != nil {
		resp.Diagnostics.AddError("Invalid template", err.Error())
		return
	}
	// CREATE OR REPLACE would silently recreate a template dropped since the plan, so it must still exist.
	_, found, err := r.read(ctx, previous)
	switch {
	case err != nil:
		resp.Diagnostics.AddError("Read template", err.Error())
		return
	case !found:
		resp.Diagnostics.AddError("Update template", "The template disappeared during the update; refresh the plan.")
		return
	}
	if err := r.exec(ctx, data.Database.ValueString(), statements...); err != nil {
		resp.Diagnostics.AddError("Update template", err.Error())
		return
	}
	data.ID = r.identity(data.Database.ValueString(), map[string]string{"schema": data.Schema.ValueString(), "name": data.Name.ValueString()})
	if err := r.converge(ctx, &data); err != nil {
		resp.Diagnostics.AddError("Verify template", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Delete drops the template and verifies catalog removal.
func (r *templateResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data templateModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	_, found, err := r.read(ctx, data)
	if err == nil && found {
		var statement string
		if statement, err = dropTemplateStatement(data); err == nil {
			err = r.exec(ctx, data.Database.ValueString(), statement)
		}
		if err == nil {
			if _, found, err = r.read(ctx, data); err == nil && found {
				err = errors.New("the template remains after deletion")
			}
		}
	}
	if err != nil {
		resp.Diagnostics.AddError("Delete template", err.Error())
	}
}

// ImportState restores the database/schema/name binding from JSON; Read then fills the definition and owner.
func (r *templateResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importIdentity(ctx, req, resp, "database", "schema", "name")
}
