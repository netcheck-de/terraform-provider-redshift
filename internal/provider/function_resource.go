package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// functionResource manages one scalar SQL user-defined function overload.
type functionResource struct {
	// resourceClient provides SQL execution and warehouse ownership checks.
	resourceClient
}

// functionModel is the Terraform state of a SQL UDF.
type functionModel struct {
	// ID records the warehouse, database, schema, name, and canonical input types.
	ID types.String `tfsdk:"id"`
	// Database contains the function.
	Database types.String `tfsdk:"database"`
	// Schema contains the function.
	Schema types.String `tfsdk:"schema"`
	// Name is the function name; overloads share it.
	Name types.String `tfsdk:"name"`
	// Arguments are the ordered input types.
	Arguments types.List `tfsdk:"arguments"`
	// Signature is the canonical bare input types identifying the overload.
	Signature types.String `tfsdk:"signature"`
	// ReturnType is the result type.
	ReturnType types.String `tfsdk:"return_type"`
	// Volatility is VOLATILE, STABLE, or IMMUTABLE.
	Volatility types.String `tfsdk:"volatility"`
	// Language is always SQL, in any case.
	Language types.String `tfsdk:"language"`
	// Body is the configured SELECT clause.
	Body types.String `tfsdk:"body"`
	// DefinitionFingerprint hashes the catalog body to detect changes made outside Terraform.
	DefinitionFingerprint types.String `tfsdk:"definition_fingerprint"`
	// Owner is the SQL user owning the function.
	Owner types.String `tfsdk:"owner"`
}

var _ = registerResource(newFunctionResource)

// newFunctionResource constructs a SQL UDF lifecycle handler.
func newFunctionResource() resource.Resource { return &functionResource{} }

// Metadata identifies the function resource to Terraform.
func (r *functionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_function"
}

// functionArgumentsChanged replaces the function when the input types differ beyond their spelling or an unknown
// modifier, because they form the overload's identity and CREATE OR REPLACE cannot change them.
func functionArgumentsChanged(_ context.Context, req planmodifier.ListRequest, resp *listplanmodifier.RequiresReplaceIfFuncResponse) {
	if req.PlanValue.IsUnknown() {
		resp.RequiresReplace = true
		return
	}
	before, after := routineStrings(req.StateValue), routineStrings(req.PlanValue)
	if len(before) != len(after) {
		resp.RequiresReplace = true
		return
	}
	for i := range before {
		if routineTypeReplaces(before[i], after[i]) {
			resp.RequiresReplace = true
			return
		}
	}
}

// functionReturnTypeChanged replaces the function when the return type differs beyond its spelling or an unknown
// modifier, because CREATE OR REPLACE must keep the return type.
func functionReturnTypeChanged(_ context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
	resp.RequiresReplace = req.PlanValue.IsUnknown() || routineTypeReplaces(req.StateValue.ValueString(), req.PlanValue.ValueString())
}

// Schema defines the function overload, its definition, and its owner.
func (r *functionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages one scalar SQL user-defined function (`LANGUAGE SQL`) overload. Python UDFs are rejected because AWS ends their support after June 30, 2026.",
		Attributes: map[string]schema.Attribute{
			"id": idAttribute(),
			"database": schema.StringAttribute{
				Required: true, MarkdownDescription: "Local database containing the function. Changing it replaces the function.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"schema": schema.StringAttribute{
				Required: true, MarkdownDescription: "Schema containing the function. Changing it replaces the function.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"name": schema.StringAttribute{
				Required: true, MarkdownDescription: "Function name; AWS recommends the reserved `f_` prefix. Overloads share a name and differ in `arguments`. Changing it replaces the function.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"arguments": schema.ListAttribute{
				ElementType: types.StringType, Optional: true,
				MarkdownDescription: "Ordered input argument data types, at most 32, referenced in `body` as `$1`, `$2`, and so on. SQL UDF arguments have no names. Omit for a function without arguments. The types identify the overload; another spelling of the same types, such as `INT` for `INTEGER`, is no change, and a modifier such as `VARCHAR(64)` added to a bare type in state, as after an import, is restated in place. Changing it replaces the function.",
				PlanModifiers:       []planmodifier.List{listplanmodifier.RequiresReplaceIf(functionArgumentsChanged, "Changing the argument types replaces the function.", "Changing the argument types replaces the function.")},
				Validators:          []validator.List{listvalidator.SizeAtMost(routineMaxArguments)},
			},
			"signature": schema.StringAttribute{
				Computed: true, MarkdownDescription: "Canonical input argument types without modifiers, as `ALTER FUNCTION`, `DROP FUNCTION`, and `GRANT ... ON FUNCTION` identify the overload, for example `INTEGER, CHARACTER VARYING`.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"return_type": schema.StringAttribute{
				Required: true, MarkdownDescription: "Data type of the returned value; another spelling of the same type is no change, and a modifier added to a bare type in state, as after an import, is restated in place. Changing it replaces the function.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplaceIf(functionReturnTypeChanged, "Changing the return type replaces the function.", "Changing the return type replaces the function.")},
			},
			"volatility": schema.StringAttribute{
				Optional: true, Computed: true, Default: stringdefault.StaticString("VOLATILE"),
				MarkdownDescription: "Optimizer volatility: `VOLATILE` (default), `STABLE`, or `IMMUTABLE`. Changed in place with `CREATE OR REPLACE FUNCTION`.",
				Validators:          []validator.String{stringvalidator.OneOf("VOLATILE", "STABLE", "IMMUTABLE")},
			},
			"language": schema.StringAttribute{
				Optional: true, Computed: true, Default: stringdefault.StaticString(functionLanguageSQL),
				MarkdownDescription: "Function language; only `SQL`, in any case, is accepted, and another spelling of it is recorded in place. `PLPYTHONU` is rejected because AWS ends Python UDF support after June 30, 2026. Changing it replaces the function.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplaceIf(keywordChanged, "Changing the language replaces the function.", "Changing the language replaces the function.")},
			},
			"body": schema.StringAttribute{
				Required: true, MarkdownDescription: "SQL `SELECT` clause without `FROM`, `INTO`, `WHERE`, `GROUP BY`, `ORDER BY`, or `LIMIT`, sent dollar-quoted and verbatim. Changed in place with `CREATE OR REPLACE FUNCTION`. A body changed outside Terraform appears here as the catalog text.",
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"definition_fingerprint": definitionFingerprintAttribute(),
			"owner": schema.StringAttribute{
				Optional: true, Computed: true, MarkdownDescription: "SQL user owning the function. When set, applied with `ALTER FUNCTION ... OWNER TO`, which requires a superuser; when omitted, the catalog owner is reported.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

// ValidateConfig reports an invalid function definition during planning once the configuration is known.
func (r *functionResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data functionModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// The language is checked first and alone, so a Python UDF is explained even while other values are unknown.
	if err := functionLanguage(data); err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("language"), "Unsupported function language", err.Error())
		return
	}
	if !req.Config.Raw.IsFullyKnown() {
		return
	}
	if _, err := newFunctionSpec(data); err != nil {
		resp.Diagnostics.AddError("Invalid function definition", err.Error())
	}
}

// functionRow is one pg_proc_info row of a function overload.
type functionRow struct {
	// arguments, returnType, volatility, language, owner, and body are the catalog values; types and the language
	// in their canonical uppercase spelling.
	arguments, returnType, volatility, language, owner, body string
}

// observe reads the function overload, reporting false when it or its database is gone.
func (r *functionResource) observe(ctx context.Context, data functionModel) (functionRow, bool, error) {
	if err := r.bound(data.ID, data.Database.ValueString()); err != nil {
		return functionRow{}, false, err
	}
	signature, err := functionSignature(data)
	if err != nil {
		return functionRow{}, false, err
	}
	if exists, err := r.localDatabaseExists(ctx, data.Database.ValueString()); err != nil || !exists {
		return functionRow{}, false, err
	}
	rows, err := r.selectRows(ctx, data.Database.ValueString(), readFunctionQuery(data.Schema.ValueString(), data.Name.ValueString(), signature))
	if err != nil || len(rows) == 0 {
		return functionRow{}, false, err
	}
	if len(rows) != 1 || rows[0]["owner"] == "" || rows[0]["return_type"] == "" {
		return functionRow{}, false, fmt.Errorf("function %q has incomplete or ambiguous catalog metadata", data.Name.ValueString())
	}
	volatility, err := routineVolatility(rows[0]["volatility"])
	if err != nil {
		return functionRow{}, false, err
	}
	row := rows[0]
	return functionRow{
		arguments: routineCatalogSignature(row["arguments"]), returnType: sqlclient.CatalogType(row["return_type"]), volatility: volatility,
		language: strings.ToUpper(row["language"]), owner: row["owner"], body: row["body"],
	}, true, nil
}

// functionCatalogArguments keeps the configured spelling of input types the catalog confirms, and otherwise
// reports the catalog's types, as after an import with differently spelled identity types.
func functionCatalogArguments(configured types.List, catalog string) types.List {
	stored, observed := routineStrings(configured), routineSignatureTypes(catalog)
	matches := len(stored) == len(observed)
	for i := 0; matches && i < len(stored); i++ {
		matches = routineCatalogTypeMatches(stored[i], observed[i])
	}
	if matches && !configured.IsUnknown() {
		return configured
	}
	if len(observed) == 0 {
		return types.ListNull(types.StringType)
	}
	values := make([]attr.Value, len(observed))
	for i, observedType := range observed {
		values[i] = types.StringValue(routineCatalogType(observedType))
	}
	return types.ListValueMust(types.StringType, values)
}

// apply copies a catalog row into the model. applied selects recordDefinition after Create or Update, and
// reconcileDefinition after a refresh.
func (row functionRow) apply(data *functionModel, applied bool) {
	data.Arguments = functionCatalogArguments(data.Arguments, row.arguments)
	data.Signature = types.StringValue(row.arguments)
	if data.ReturnType.IsNull() || data.ReturnType.IsUnknown() || !routineCatalogTypeMatches(data.ReturnType.ValueString(), row.returnType) {
		data.ReturnType = types.StringValue(routineCatalogType(row.returnType))
	}
	data.Volatility, data.Language = types.StringValue(row.volatility), keywordValue(data.Language, row.language)
	data.Owner = routineOwner(data.Owner, row.owner)
	if applied {
		data.Body, data.DefinitionFingerprint = recordDefinition(data.Body, row.body)
	} else {
		data.Body, data.DefinitionFingerprint = reconcileDefinition(data.Body, data.DefinitionFingerprint, row.body)
	}
}

// read refreshes the function overload; false means it or its database is gone.
func (r *functionResource) read(ctx context.Context, data *functionModel) (bool, error) {
	row, found, err := r.observe(ctx, *data)
	if err == nil && found {
		row.apply(data, false)
	}
	return found, err
}

// identityOf builds the JSON identity, including the canonical input types that select the overload.
func (r *functionResource) identityOf(data functionModel, signature sqlclient.Keyword) types.String {
	return r.identity(data.Database.ValueString(), map[string]string{"schema": data.Schema.ValueString(), "name": data.Name.ValueString(), routineIdentityArguments: string(signature)})
}

// verify re-reads the function after a change and checks that the planned definition and owner converged.
func (r *functionResource) verify(ctx context.Context, data *functionModel) error {
	planned := *data
	row, found, err := r.observe(ctx, *data)
	switch {
	case err != nil:
		return err
	case !found:
		return errors.New("the function is absent after the change")
	case row.volatility != planned.Volatility.ValueString():
		return fmt.Errorf("the function volatility is %s, not %s", row.volatility, planned.Volatility.ValueString())
	case !planned.Owner.IsUnknown() && !planned.Owner.IsNull() && !routineOwnerMatches(planned.Owner, row.owner):
		return fmt.Errorf("the function owner is %q, not %q", row.owner, planned.Owner.ValueString())
	}
	row.apply(data, true)
	return nil
}

// Create validates the definition, creates the function, applies a configured owner, and verifies the result.
func (r *functionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data functionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	spec, err := newFunctionSpec(data)
	if err != nil {
		resp.Diagnostics.AddError("Invalid function definition", err.Error())
		return
	}
	if err := r.exec(ctx, data.Database.ValueString(), createFunctionStatement(spec, false)); err != nil {
		resp.Diagnostics.AddError("Create function", err.Error())
		return
	}
	data.ID = r.identityOf(data, spec.signature)
	data.Signature = types.StringValue(string(spec.signature))
	// A failed owner change or verification must still return serializable state for the created function.
	planned := data
	data.DefinitionFingerprint = types.StringNull()
	if data.Owner.IsUnknown() {
		data.Owner = types.StringNull()
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// CREATE makes the executing user the owner; a configured owner is applied as a change from none.
	owner := alterStatements(functionModel{Owner: types.StringNull()}, planned, []alterStep[functionModel]{functionOwnerStep})
	if err := r.exec(ctx, data.Database.ValueString(), owner...); err != nil {
		resp.Diagnostics.AddError("Set function owner", err.Error())
		return
	}
	if err := r.verify(ctx, &planned); err != nil {
		resp.Diagnostics.AddError("Verify function", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &planned)...)
}

// Read refreshes the function or removes it from state when it or its database is gone.
func (r *functionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data functionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, &data)
	if err != nil {
		resp.Diagnostics.AddError("Read function", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update redefines the function in place and changes its owner, then verifies the result.
func (r *functionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data, previous functionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &previous)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.bound(previous.ID, data.Database.ValueString()); err != nil {
		resp.Diagnostics.AddError("Update function", err.Error())
		return
	}
	statements, err := alterFunctionStatements(previous, data)
	if err != nil {
		resp.Diagnostics.AddError("Invalid function definition", err.Error())
		return
	}
	if err := r.exec(ctx, data.Database.ValueString(), statements...); err != nil {
		resp.Diagnostics.AddError("Update function", err.Error())
		return
	}
	if err := r.verify(ctx, &data); err != nil {
		resp.Diagnostics.AddError("Verify function", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Delete drops the function without CASCADE and verifies its removal.
func (r *functionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data functionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	_, found, err := r.observe(ctx, data)
	if err == nil && found {
		var statement string
		if statement, err = dropFunctionStatement(data); err == nil {
			err = r.exec(ctx, data.Database.ValueString(), statement)
		}
		if err == nil {
			if _, found, err = r.observe(ctx, data); err == nil && found {
				resp.Diagnostics.AddError("Delete function", "The function remains after deletion.")
			}
		}
	}
	if err != nil {
		resp.Diagnostics.AddError("Delete function", err.Error())
	}
}

// ImportState restores the overload from its JSON identity, whose arguments key selects the input types.
func (r *functionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importIdentity(ctx, req, resp, "database", "schema", "name")
	if resp.Diagnostics.HasError() {
		return
	}
	arguments, signature, err := routineImportArguments(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import identity", err.Error())
		return
	}
	list := types.ListNull(types.StringType)
	if len(arguments) > 0 {
		values := make([]attr.Value, len(arguments))
		for i, argument := range arguments {
			values[i] = types.StringValue(argument)
		}
		list = types.ListValueMust(types.StringType, values)
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("arguments"), list)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("signature"), string(signature))...)
}
