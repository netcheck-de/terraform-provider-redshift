package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
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

// externalFunctionResource manages a scalar Lambda UDF created with CREATE EXTERNAL FUNCTION.
type externalFunctionResource struct {
	// resourceClient provides SQL execution and warehouse ownership checks.
	resourceClient
}

// externalFunctionModel is the Terraform state of one external function overload.
type externalFunctionModel struct {
	// ID records the warehouse, database, schema, name, and canonical signature.
	ID types.String `tfsdk:"id"`
	// Database contains the function.
	Database types.String `tfsdk:"database"`
	// Schema contains the function.
	Schema types.String `tfsdk:"schema"`
	// Name is the SQL function name, shared by its overloads.
	Name types.String `tfsdk:"name"`
	// Arguments are the input types in declaration order; with Name they identify the overload.
	Arguments types.List `tfsdk:"arguments"`
	// ReturnType is the result type.
	ReturnType types.String `tfsdk:"return_type"`
	// Volatility is VOLATILE or STABLE.
	Volatility types.String `tfsdk:"volatility"`
	// LambdaFunction is the Lambda function Redshift invokes.
	LambdaFunction types.String `tfsdk:"lambda_function"`
	// IAMRole authorizes the invocation: default or chained role ARNs.
	IAMRole types.String `tfsdk:"iam_role"`
	// RetryTimeout bounds the total retry backoff in milliseconds.
	RetryTimeout types.Int64 `tfsdk:"retry_timeout"`
	// MaxBatchRows bounds the rows per Lambda invocation.
	MaxBatchRows types.Int64 `tfsdk:"max_batch_rows"`
	// MaxBatchSize bounds the payload per Lambda invocation in MaxBatchSizeUnit.
	MaxBatchSize types.Int64 `tfsdk:"max_batch_size"`
	// MaxBatchSizeUnit is KB or MB.
	MaxBatchSizeUnit types.String `tfsdk:"max_batch_size_unit"`
	// Owner is the SQL user owning the function.
	Owner types.String `tfsdk:"owner"`
}

var _ = registerResource(newExternalFunctionResource)

// newExternalFunctionResource constructs an external function lifecycle handler.
func newExternalFunctionResource() resource.Resource { return &externalFunctionResource{} }

// Metadata identifies the external function resource to Terraform.
func (r *externalFunctionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_external_function"
}

// externalFunctionArgumentsChange replaces the function only when the canonical signature changes, so a
// different spelling of the same types, such as after import, updates state in place.
func externalFunctionArgumentsChange(_ context.Context, req planmodifier.ListRequest, resp *listplanmodifier.RequiresReplaceIfFuncResponse) {
	// An unknown list has no elements, so it would otherwise compare equal to an empty signature and let
	// CREATE OR REPLACE add a second overload instead of replacing the first.
	if req.PlanValue.IsUnknown() {
		resp.RequiresReplace = true
		return
	}
	for _, element := range req.PlanValue.Elements() {
		if element.IsUnknown() {
			resp.RequiresReplace = true
			return
		}
	}
	before, errBefore := externalFunctionSignature(externalFunctionTypeList(req.StateValue))
	after, errAfter := externalFunctionSignature(externalFunctionTypeList(req.PlanValue))
	resp.RequiresReplace = errBefore != nil || errAfter != nil || before != after
}

// externalFunctionReturnChange replaces the function only when the result base type changes; OR REPLACE cannot
// change it, while a new length or a different spelling is restated in place.
func externalFunctionReturnChange(_ context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
	before, errBefore := externalFunctionType(req.StateValue.ValueString())
	after, errAfter := externalFunctionType(req.PlanValue.ValueString())
	resp.RequiresReplace = errBefore != nil || errAfter != nil || externalFunctionBaseType(before) != externalFunctionBaseType(after)
}

// Schema defines the function identity, its Lambda invocation options, and its owner.
func (r *externalFunctionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	argumentsNote := "Changing the canonical argument types replaces the function; a different spelling of the same types, such as `INT` for `INTEGER`, is recorded in place."
	returnNote := "Changing the base type replaces the function; a new length or precision is applied in place with `CREATE OR REPLACE`."
	configurationOnly := " Redshift does not expose it in a documented catalog view, so changes made outside Terraform are not detected and import leaves it unset until the next apply restates the function."
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a scalar Lambda user-defined function created with `CREATE EXTERNAL FUNCTION`. The Lambda function and its IAM permissions belong to the AWS provider.",
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
				Required: true, MarkdownDescription: "Function name; AWS recommends the reserved `f_` prefix to avoid conflicts with built-in functions. Changing it replaces the function.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"arguments": schema.ListAttribute{
				ElementType: types.StringType, Required: true,
				MarkdownDescription: "Input argument data types in order, at most 32, from the types Lambda UDFs support: `SMALLINT`, `INTEGER`, `BIGINT`, `DECIMAL`, `REAL`, `DOUBLE PRECISION`, `CHAR`, `VARCHAR`, `BOOLEAN`, `DATE`, `TIMESTAMP`, and their aliases. Use `[]` for a function without arguments. With `name` they identify the overload. " + argumentsNote,
				PlanModifiers:       []planmodifier.List{listplanmodifier.RequiresReplaceIf(externalFunctionArgumentsChange, argumentsNote, argumentsNote)},
				Validators:          []validator.List{listvalidator.SizeAtMost(externalFunctionMaxArguments)},
			},
			"return_type": schema.StringAttribute{
				Required: true, MarkdownDescription: "Result data type, from the same types as `arguments`. " + returnNote,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplaceIf(externalFunctionReturnChange, returnNote, returnNote)},
			},
			"volatility": schema.StringAttribute{
				Optional: true, Computed: true, Default: stringdefault.StaticString("VOLATILE"),
				MarkdownDescription: "`VOLATILE` (default) or `STABLE`; Redshift does not support `IMMUTABLE` for Lambda UDFs. Applied in place with `CREATE OR REPLACE`, which requires a superuser.",
				Validators:          []validator.String{stringvalidator.OneOf("VOLATILE", "STABLE")},
			},
			"lambda_function": schema.StringAttribute{
				Required: true, MarkdownDescription: "Name or ARN of the Lambda function Redshift invokes. Applied in place with `CREATE OR REPLACE`, which requires a superuser." + configurationOnly,
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"iam_role": schema.StringAttribute{
				Required: true, MarkdownDescription: "`DEFAULT`, in any case, for the warehouse's default IAM role, or the ARN of an associated IAM role allowed to invoke the Lambda function; chain roles with commas and no spaces. Applied in place with `CREATE OR REPLACE`." + configurationOnly,
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"retry_timeout": schema.Int64Attribute{
				Optional: true, MarkdownDescription: "Total retry backoff in milliseconds; `0` disables retries. Unset uses the Redshift default of 20,000. Applied in place with `CREATE OR REPLACE`." + configurationOnly,
				Validators: []validator.Int64{int64validator.AtLeast(0)},
			},
			"max_batch_rows": schema.Int64Attribute{
				Optional: true, MarkdownDescription: "Maximum rows per Lambda invocation, from 1 to 2,147,483,647 (the default). Applied in place with `CREATE OR REPLACE`." + configurationOnly,
				Validators: []validator.Int64{int64validator.Between(1, externalFunctionMaxBatchRows)},
			},
			"max_batch_size": schema.Int64Attribute{
				Optional: true, MarkdownDescription: "Maximum payload per Lambda invocation in `max_batch_size_unit`, from 1 KB to 5 MB (the default). Applied in place with `CREATE OR REPLACE`." + configurationOnly,
				Validators: []validator.Int64{int64validator.AtLeast(1)},
			},
			"max_batch_size_unit": schema.StringAttribute{
				Optional: true, MarkdownDescription: "Unit of `max_batch_size`: `KB` or `MB`. Unset means KB, as in Redshift. Requires `max_batch_size`.",
				Validators: []validator.String{stringvalidator.OneOf("KB", "MB"), stringvalidator.AlsoRequires(path.MatchRoot("max_batch_size"))},
			},
			"owner": schema.StringAttribute{
				Optional: true, Computed: true,
				MarkdownDescription: "SQL user owning the function, applied with `ALTER FUNCTION ... OWNER TO`, which requires a superuser. Unset keeps the creating user and reports it.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
		},
	}
}

// externalFunctionCheck validates the configuration exactly as Create renders it.
func externalFunctionCheck(data externalFunctionModel) error {
	_, err := createExternalFunctionStatements(data)
	return err
}

// ValidateConfig reports definitions Create would reject, during planning once the configuration is known.
func (r *externalFunctionResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data externalFunctionModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !req.Config.Raw.IsFullyKnown() {
		return
	}
	if err := externalFunctionCheck(data); err != nil {
		resp.Diagnostics.AddError("Invalid external function", err.Error())
	}
}

// identityOf returns the JSON identity of the overload; the signature is canonical, so equivalent spellings of the
// argument types share one identity.
func (r *externalFunctionResource) identityOf(data externalFunctionModel) (types.String, error) {
	signature, err := externalFunctionSignature(externalFunctionTypeList(data.Arguments))
	if err != nil {
		return types.StringNull(), err
	}
	return r.identity(data.Database.ValueString(), map[string]string{"schema": data.Schema.ValueString(), "name": data.Name.ValueString(), "arguments": string(signature)}), nil
}

// read refreshes the observable catalog state: the result type, volatility, and owner. The Lambda options are
// kept from state because no documented catalog view reports them.
func (r *externalFunctionResource) read(ctx context.Context, data *externalFunctionModel) (bool, error) {
	database := data.Database.ValueString()
	if err := r.bound(data.ID, database); err != nil {
		return false, err
	}
	if exists, err := r.localDatabaseExists(ctx, database); err != nil || !exists {
		return false, err
	}
	query, err := readExternalFunctionQuery(*data)
	if err != nil {
		return false, err
	}
	rows, err := r.selectRows(ctx, database, query)
	if err != nil || len(rows) == 0 {
		return false, err
	}
	row := rows[0]
	if len(rows) != 1 || row["owner"] == "" || row["return_type"] == "" {
		return false, fmt.Errorf("external function %s.%s has incomplete or ambiguous catalog metadata", data.Schema.ValueString(), data.Name.ValueString())
	}
	if !strings.EqualFold(row["language"], "EXFUNC") {
		return false, fmt.Errorf("function %s.%s(%s) exists but is a %s function, not an external function", data.Schema.ValueString(), data.Name.ValueString(),
			routineCatalogSignature(row["arguments"]), strings.ToUpper(row["language"]))
	}
	volatility, err := externalFunctionVolatility(row["volatility"])
	if err != nil {
		return false, err
	}
	// Keep the configured spelling while it names the catalog's base type; otherwise surface the drift.
	returnType := sqlclient.CatalogType(row["return_type"])
	if configured, err := externalFunctionType(data.ReturnType.ValueString()); err != nil || externalFunctionBaseType(configured) != returnType {
		data.ReturnType = types.StringValue(returnType)
	}
	data.Volatility, data.Owner = types.StringValue(volatility), types.StringValue(row["owner"])
	return true, nil
}

// converge re-reads the function after a change and fails when the catalog does not show the planned state.
func (r *externalFunctionResource) converge(ctx context.Context, data *externalFunctionModel, planned externalFunctionModel) error {
	found, err := r.read(ctx, data)
	switch {
	case err != nil:
		return err
	case !found:
		return fmt.Errorf("the external function is absent after the change")
	case data.ReturnType.ValueString() != planned.ReturnType.ValueString():
		return fmt.Errorf("the catalog reports return type %q instead of %q", data.ReturnType.ValueString(), planned.ReturnType.ValueString())
	case knownString(planned.Volatility) != "" && data.Volatility.ValueString() != planned.Volatility.ValueString():
		return fmt.Errorf("the catalog reports volatility %s instead of %s", data.Volatility.ValueString(), planned.Volatility.ValueString())
	case knownString(planned.Owner) != "" && data.Owner.ValueString() != planned.Owner.ValueString():
		return fmt.Errorf("the catalog reports owner %q instead of %q", data.Owner.ValueString(), planned.Owner.ValueString())
	}
	return nil
}

// Create creates the function, assigns a configured owner, and verifies the catalog.
func (r *externalFunctionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data externalFunctionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Invalid definitions must fail before ownership is recorded, or Read and Delete could never succeed.
	statements, err := createExternalFunctionStatements(data)
	if err != nil {
		resp.Diagnostics.AddError("Create external function", err.Error())
		return
	}
	id, err := r.identityOf(data)
	if err != nil {
		resp.Diagnostics.AddError("Create external function", err.Error())
		return
	}
	database := data.Database.ValueString()
	if err := r.exec(ctx, database, statements[0]); err != nil {
		resp.Diagnostics.AddError("Create external function", err.Error())
		return
	}
	planned := data
	data.ID = id
	// A failed owner change or verification must still return serializable state for the created function.
	if data.Owner.IsUnknown() {
		data.Owner = types.StringNull()
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.exec(ctx, database, statements[1:]...); err != nil {
		resp.Diagnostics.AddError("Set external function owner", err.Error())
		return
	}
	if err := r.converge(ctx, &data, planned); err != nil {
		resp.Diagnostics.AddError("Verify external function", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Read refreshes the function or removes it from state when it or its database is gone.
func (r *externalFunctionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data externalFunctionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, &data)
	if err != nil {
		resp.Diagnostics.AddError("Read external function", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update restates the definition or changes the owner in place and verifies the catalog.
func (r *externalFunctionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state externalFunctionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.bound(state.ID, state.Database.ValueString()); err != nil {
		resp.Diagnostics.AddError("Update external function", err.Error())
		return
	}
	statements, err := alterExternalFunctionStatements(state, plan)
	if err != nil {
		resp.Diagnostics.AddError("Update external function", err.Error())
		return
	}
	if err := r.exec(ctx, plan.Database.ValueString(), statements...); err != nil {
		resp.Diagnostics.AddError("Update external function", err.Error())
		return
	}
	planned := plan
	if err := r.converge(ctx, &plan, planned); err != nil {
		resp.Diagnostics.AddError("Verify external function", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete drops the function restrictively and verifies catalog removal.
func (r *externalFunctionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data externalFunctionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, &data)
	if err == nil && found {
		var statement string
		if statement, err = dropExternalFunctionStatement(data); err == nil {
			err = r.exec(ctx, data.Database.ValueString(), statement)
		}
		if err == nil {
			found, err = r.read(ctx, &data)
			if err == nil && found {
				resp.Diagnostics.AddError("Delete external function", "The external function remains after deletion.")
			}
		}
	}
	if err != nil {
		resp.Diagnostics.AddError("Delete external function", err.Error())
	}
}

// ImportState restores the overload from the same JSON identity Create records. arguments holds the
// canonical signature and may be empty for a function without arguments.
func (r *externalFunctionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importIdentity(ctx, req, resp, "database", "schema", "name")
	if resp.Diagnostics.HasError() {
		return
	}
	var fields map[string]any
	_ = json.Unmarshal([]byte(req.ID), &fields) // importIdentity already parsed the same JSON.
	signature, ok := fields["arguments"].(string)
	if !ok {
		resp.Diagnostics.AddError("Invalid import identity", "Missing string field arguments; use \"\" for a function without arguments.")
		return
	}
	argumentTypes := externalFunctionSignatureTypes(signature)
	if canonical, err := externalFunctionSignature(argumentTypes); err != nil || string(canonical) != signature {
		resp.Diagnostics.AddError("Invalid import identity", fmt.Sprintf("arguments must be the canonical signature that Create records, such as \"INTEGER, CHARACTER VARYING\"; got %q.", signature))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("arguments"), externalFunctionTypeValues(argumentTypes))...)
}
