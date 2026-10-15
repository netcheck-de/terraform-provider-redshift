package provider

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/mapvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
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

// procedureResource manages one PL/pgSQL stored procedure overload.
type procedureResource struct {
	// resourceClient provides SQL execution and warehouse ownership checks.
	resourceClient
}

// procedureModel is the Terraform state of a stored procedure.
type procedureModel struct {
	// ID records the warehouse, database, schema, name, and canonical input types.
	ID types.String `tfsdk:"id"`
	// Database contains the procedure.
	Database types.String `tfsdk:"database"`
	// Schema contains the procedure.
	Schema types.String `tfsdk:"schema"`
	// Name is the procedure name; overloads share it.
	Name types.String `tfsdk:"name"`
	// Arguments are the ordered argument blocks with names and modes.
	Arguments types.List `tfsdk:"argument"`
	// Signature is the canonical bare IN and INOUT types identifying the overload.
	Signature types.String `tfsdk:"signature"`
	// Body is the configured PL/pgSQL block.
	Body types.String `tfsdk:"body"`
	// DefinitionFingerprint hashes the catalog body to detect changes made outside Terraform.
	DefinitionFingerprint types.String `tfsdk:"definition_fingerprint"`
	// Security is INVOKER or DEFINER.
	Security types.String `tfsdk:"security"`
	// Nonatomic selects the nonatomic transaction mode.
	Nonatomic types.Bool `tfsdk:"nonatomic"`
	// Configuration holds the SET parameter applied while the procedure runs.
	Configuration types.Map `tfsdk:"configuration"`
	// Owner is the SQL user owning the procedure.
	Owner types.String `tfsdk:"owner"`
}

var _ = registerResource(newProcedureResource)

// newProcedureResource constructs a stored procedure lifecycle handler.
func newProcedureResource() resource.Resource { return &procedureResource{} }

// Metadata identifies the procedure resource to Terraform.
func (r *procedureResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_procedure"
}

// procedureArgumentsChanged replaces the procedure when its arguments differ beyond spelling, because Redshift
// requires dropping a procedure to change its signature or output types.
// https://docs.aws.amazon.com/redshift/latest/dg/stored-procedure-naming.html
func procedureArgumentsChanged(_ context.Context, req planmodifier.ListRequest, resp *listplanmodifier.RequiresReplaceIfFuncResponse) {
	resp.RequiresReplace = req.PlanValue.IsUnknown() || !procedureArgumentsEquivalent(procedureArguments(req.StateValue), procedureArguments(req.PlanValue))
}

// Schema defines the procedure overload, its definition, and its owner.
func (r *procedureResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages one PL/pgSQL stored procedure overload.",
		Attributes: map[string]schema.Attribute{
			"id": idAttribute(),
			"database": schema.StringAttribute{
				Required: true, MarkdownDescription: "Local database containing the procedure. Changing it replaces the procedure.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"schema": schema.StringAttribute{
				Required: true, MarkdownDescription: "Schema containing the procedure. Changing it replaces the procedure.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"name": schema.StringAttribute{
				Required: true, MarkdownDescription: "Procedure name; AWS recommends the reserved `sp_` prefix. Overloads share a name and differ in their input argument types. Changing it replaces the procedure.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"signature": schema.StringAttribute{
				Computed: true, MarkdownDescription: "Canonical `IN` and `INOUT` argument types without modifiers, as `ALTER PROCEDURE`, `DROP PROCEDURE`, and `GRANT ... ON PROCEDURE` identify the procedure, for example `INTEGER, CHARACTER VARYING`.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"body": schema.StringAttribute{
				Required: true, MarkdownDescription: "PL/pgSQL block, usually `BEGIN ... END;` with an optional `DECLARE` section, sent dollar-quoted and verbatim. Changed in place with `CREATE OR REPLACE PROCEDURE`. A body changed outside Terraform appears here as the catalog text.",
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"definition_fingerprint": definitionFingerprintAttribute(),
			"security": schema.StringAttribute{
				Optional: true, Computed: true, Default: stringdefault.StaticString("INVOKER"),
				MarkdownDescription: "`INVOKER` (default) runs with the caller's privileges, `DEFINER` with the owner's. `DEFINER` is not supported with `nonatomic`. Changed in place with `CREATE OR REPLACE PROCEDURE`.",
				Validators:          []validator.String{stringvalidator.OneOf("INVOKER", "DEFINER")},
			},
			"nonatomic": schema.BoolAttribute{
				Optional: true, MarkdownDescription: "Creates the procedure in `NONATOMIC` transaction mode, which commits each statement automatically. Redshift does not report the mode in its catalog, so drift is not detected and an import leaves it `null`. Changed in place with `CREATE OR REPLACE PROCEDURE`.",
			},
			"configuration": schema.MapAttribute{
				ElementType: types.StringType, Optional: true,
				MarkdownDescription: "One configuration parameter set while the procedure runs, rendered as `SET <name> TO '<value>'`, for example `{ search_path = \"analytics, public\" }`. A `search_path` value is a comma-separated list of schema names, each rendered as its own literal, such as `SET search_path TO 'analytics', 'public'`; any other value is one literal. Not supported with `nonatomic`. Redshift does not report it in its catalog, so drift is not detected and an import leaves it `null`. Changed in place with `CREATE OR REPLACE PROCEDURE`.",
				Validators:          []validator.Map{mapvalidator.SizeAtMost(1)},
			},
			"owner": schema.StringAttribute{
				Optional: true, Computed: true, MarkdownDescription: "SQL user owning the procedure. When set, applied with `ALTER PROCEDURE ... OWNER TO`, which requires a superuser; when omitted, the catalog owner is reported.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
		Blocks: map[string]schema.Block{
			"argument": schema.ListNestedBlock{
				MarkdownDescription: "One block per argument, in order: at most 32 input (`IN`, `INOUT`) and 32 output (`OUT`, `INOUT`) arguments. Omit for a procedure without arguments. The input types identify the procedure; another spelling of the same types, " +
					"a `null` mode for `IN`, or a name differing only in case is no change, and a modifier such as `VARCHAR(64)` added to a bare type in state, as after an import, is restated in place. Changing it replaces the procedure.",
				PlanModifiers: []planmodifier.List{listplanmodifier.RequiresReplaceIf(procedureArgumentsChanged, "Changing the arguments replaces the procedure.", "Changing the arguments replaces the procedure.")},
				Validators:    []validator.List{listvalidator.SizeAtMost(2 * routineMaxArguments)},
				NestedObject: schema.NestedBlockObject{Attributes: map[string]schema.Attribute{
					"name": schema.StringAttribute{Optional: true, MarkdownDescription: "Argument name used in `body`; omit to reference the argument as `$n`."},
					"mode": schema.StringAttribute{
						Optional: true, MarkdownDescription: "`IN` (when omitted), `OUT`, or `INOUT`. `OUT` arguments are returned by `CALL` and are not part of the signature.",
						Validators: []validator.String{stringvalidator.OneOf("IN", "OUT", "INOUT")},
					},
					"type": schema.StringAttribute{Required: true, MarkdownDescription: "Argument data type, such as `INTEGER`, `VARCHAR(256)`, or `REFCURSOR`."},
				}},
			},
		},
	}
}

// ValidateConfig reports an invalid procedure definition during planning once the configuration is known.
func (r *procedureResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data procedureModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !req.Config.Raw.IsFullyKnown() {
		return
	}
	if _, err := newProcedureSpec(data); err != nil {
		resp.Diagnostics.AddError("Invalid procedure definition", err.Error())
	}
}

// procedureRow is the catalog state of a procedure overload.
type procedureRow struct {
	// signature, owner, and body are the pg_proc_info values.
	signature, owner, body string
	// definer reports SECURITY DEFINER.
	definer bool
	// arguments are the SHOW PARAMETERS rows in ordinal order.
	arguments []procedureArgumentModel
}

// procedureParameters converts SHOW PARAMETERS rows into arguments ordered by position, with a null mode for IN as
// configuration usually omits it.
func procedureParameters(rows []sqlclient.Row) ([]procedureArgumentModel, error) {
	type parameter struct {
		position int
		argument procedureArgumentModel
	}
	var parameters []parameter
	for _, row := range rows {
		mode := strings.ToUpper(row["parameter_type"])
		if mode == "RETURN" {
			continue
		}
		position, err := strconv.Atoi(row["ordinal_position"])
		if err != nil || row["data_type"] == "" {
			return nil, fmt.Errorf("procedure parameter %q has incomplete catalog metadata", row["parameter_name"])
		}
		argument := procedureArgumentModel{Name: types.StringNull(), Mode: types.StringNull(), Type: types.StringValue(routineCatalogType(row["data_type"]))}
		if row["parameter_name"] != "" {
			argument.Name = types.StringValue(row["parameter_name"])
		}
		if mode != "IN" {
			argument.Mode = types.StringValue(mode)
		}
		parameters = append(parameters, parameter{position: position, argument: argument})
	}
	slices.SortFunc(parameters, func(a, b parameter) int { return a.position - b.position })
	arguments := make([]procedureArgumentModel, len(parameters))
	for i, parameter := range parameters {
		arguments[i] = parameter.argument
	}
	return arguments, nil
}

// observe reads the procedure overload and its arguments, reporting false when it or its database is gone.
func (r *procedureResource) observe(ctx context.Context, data procedureModel) (procedureRow, bool, error) {
	if err := r.bound(data.ID, data.Database.ValueString()); err != nil {
		return procedureRow{}, false, err
	}
	signature, err := procedureSignature(procedureArguments(data.Arguments))
	if err != nil {
		return procedureRow{}, false, err
	}
	database, schema, name := data.Database.ValueString(), data.Schema.ValueString(), data.Name.ValueString()
	if exists, err := r.localDatabaseExists(ctx, database); err != nil || !exists {
		return procedureRow{}, false, err
	}
	rows, err := r.selectRows(ctx, database, readProcedureQuery(schema, name, signature))
	if err != nil || len(rows) == 0 {
		return procedureRow{}, false, err
	}
	if len(rows) != 1 || rows[0]["owner"] == "" {
		return procedureRow{}, false, fmt.Errorf("procedure %q has incomplete or ambiguous catalog metadata", name)
	}
	definer, err := strconv.ParseBool(rows[0]["security_definer"])
	if err != nil {
		return procedureRow{}, false, fmt.Errorf("procedure %q has an invalid security mode: %w", name, err)
	}
	parameters, err := r.queryDatabase(ctx, database, showProcedureParametersStatement(schema, name, signature), nil)
	if err != nil {
		return procedureRow{}, false, err
	}
	arguments, err := procedureParameters(parameters)
	if err != nil {
		return procedureRow{}, false, err
	}
	return procedureRow{signature: routineCatalogSignature(rows[0]["arguments"]), owner: rows[0]["owner"], body: rows[0]["body"], definer: definer, arguments: arguments}, true, nil
}

// procedureCatalogArguments keeps the configured arguments the catalog confirms and otherwise reports the
// catalog's. A catalog argument without a name matches any configured name, because Redshift may keep an OUT
// argument's name only as an alias inside the body.
func procedureCatalogArguments(configured types.List, catalog []procedureArgumentModel) types.List {
	stored := procedureArguments(configured)
	matches := !configured.IsUnknown() && len(stored) == len(catalog)
	for i := 0; matches && i < len(stored); i++ {
		name := catalog[i].Name.ValueString()
		matches = procedureMode(stored[i]) == procedureMode(catalog[i]) && routineCatalogTypeMatches(stored[i].Type.ValueString(), catalog[i].Type.ValueString()) &&
			(name == "" || strings.EqualFold(stored[i].Name.ValueString(), name))
	}
	if matches {
		return configured
	}
	return procedureArgumentList(catalog)
}

// apply copies a catalog row into the model. nonatomic and configuration keep their state because the catalog
// does not report them. applied selects recordDefinition after Create or Update, and reconcileDefinition after a
// refresh.
func (row procedureRow) apply(data *procedureModel, applied bool) {
	data.Arguments = procedureCatalogArguments(data.Arguments, row.arguments)
	data.Signature = types.StringValue(row.signature)
	data.Security = types.StringValue(procedureSecurity(row.definer))
	data.Owner = routineOwner(data.Owner, row.owner)
	if applied {
		data.Body, data.DefinitionFingerprint = recordDefinition(data.Body, row.body)
	} else {
		data.Body, data.DefinitionFingerprint = reconcileDefinition(data.Body, data.DefinitionFingerprint, row.body)
	}
}

// procedureSecurity maps pg_proc.prosecdef to its CREATE PROCEDURE keyword.
func procedureSecurity(definer bool) string {
	if definer {
		return "DEFINER"
	}
	return "INVOKER"
}

// read refreshes the procedure overload; false means it or its database is gone.
func (r *procedureResource) read(ctx context.Context, data *procedureModel) (bool, error) {
	row, found, err := r.observe(ctx, *data)
	if err == nil && found {
		row.apply(data, false)
	}
	return found, err
}

// identityOf builds the JSON identity, including the canonical input types that select the overload.
func (r *procedureResource) identityOf(data procedureModel, signature sqlclient.Keyword) types.String {
	return r.identity(data.Database.ValueString(), map[string]string{"schema": data.Schema.ValueString(), "name": data.Name.ValueString(), routineIdentityArguments: string(signature)})
}

// verify re-reads the procedure after a change and checks that the planned security mode and owner converged.
func (r *procedureResource) verify(ctx context.Context, data *procedureModel) error {
	row, found, err := r.observe(ctx, *data)
	switch {
	case err != nil:
		return err
	case !found:
		return errors.New("the procedure is absent after the change")
	case procedureSecurity(row.definer) != data.Security.ValueString():
		return fmt.Errorf("the procedure security is %s, not %s", procedureSecurity(row.definer), data.Security.ValueString())
	case !data.Owner.IsUnknown() && !data.Owner.IsNull() && !routineOwnerMatches(data.Owner, row.owner):
		return fmt.Errorf("the procedure owner is %q, not %q", row.owner, data.Owner.ValueString())
	}
	row.apply(data, true)
	return nil
}

// Create validates the definition, creates the procedure, applies a configured owner, and verifies the result.
func (r *procedureResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data procedureModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	spec, err := newProcedureSpec(data)
	if err != nil {
		resp.Diagnostics.AddError("Invalid procedure definition", err.Error())
		return
	}
	if err := r.exec(ctx, data.Database.ValueString(), createProcedureStatement(spec, false)); err != nil {
		resp.Diagnostics.AddError("Create procedure", err.Error())
		return
	}
	data.ID = r.identityOf(data, spec.signature)
	data.Signature = types.StringValue(string(spec.signature))
	// A failed owner change or verification must still return serializable state for the created procedure.
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
	owner := alterStatements(procedureModel{Owner: types.StringNull()}, planned, []alterStep[procedureModel]{procedureOwnerStep})
	if err := r.exec(ctx, data.Database.ValueString(), owner...); err != nil {
		resp.Diagnostics.AddError("Set procedure owner", err.Error())
		return
	}
	if err := r.verify(ctx, &planned); err != nil {
		resp.Diagnostics.AddError("Verify procedure", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &planned)...)
}

// Read refreshes the procedure or removes it from state when it or its database is gone.
func (r *procedureResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data procedureModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, &data)
	if err != nil {
		resp.Diagnostics.AddError("Read procedure", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update redefines the procedure in place and changes its owner, then verifies the result.
func (r *procedureResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data, previous procedureModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &previous)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.bound(previous.ID, data.Database.ValueString()); err != nil {
		resp.Diagnostics.AddError("Update procedure", err.Error())
		return
	}
	statements, err := alterProcedureStatements(previous, data)
	if err != nil {
		resp.Diagnostics.AddError("Invalid procedure definition", err.Error())
		return
	}
	if err := r.exec(ctx, data.Database.ValueString(), statements...); err != nil {
		resp.Diagnostics.AddError("Update procedure", err.Error())
		return
	}
	if err := r.verify(ctx, &data); err != nil {
		resp.Diagnostics.AddError("Verify procedure", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Delete drops the procedure and verifies its removal.
func (r *procedureResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data procedureModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	_, found, err := r.observe(ctx, data)
	if err == nil && found {
		var statement string
		if statement, err = dropProcedureStatement(data); err == nil {
			err = r.exec(ctx, data.Database.ValueString(), statement)
		}
		if err == nil {
			if _, found, err = r.observe(ctx, data); err == nil && found {
				resp.Diagnostics.AddError("Delete procedure", "The procedure remains after deletion.")
			}
		}
	}
	if err != nil {
		resp.Diagnostics.AddError("Delete procedure", err.Error())
	}
}

// ImportState restores the overload from its JSON identity. The identity holds only the input types, so they are
// imported as IN argument blocks, and the refresh reports the actual names, modes, and OUT arguments.
func (r *procedureResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importIdentity(ctx, req, resp, "database", "schema", "name")
	if resp.Diagnostics.HasError() {
		return
	}
	inputs, signature, err := routineImportArguments(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import identity", err.Error())
		return
	}
	arguments := make([]procedureArgumentModel, len(inputs))
	for i, input := range inputs {
		arguments[i] = procedureArgumentModel{Name: types.StringNull(), Mode: types.StringNull(), Type: types.StringValue(input)}
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("argument"), procedureArgumentList(arguments))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("signature"), string(signature))...)
}
