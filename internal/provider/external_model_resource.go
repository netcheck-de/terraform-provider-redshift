package provider

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// externalModelResource manages an Amazon Bedrock model created with CREATE EXTERNAL MODEL.
type externalModelResource struct {
	// resourceClient provides SQL execution and warehouse ownership checks.
	resourceClient
}

// externalModelModel is the Terraform state of one external model.
type externalModelModel struct {
	// ID records the warehouse, database, schema, and name.
	ID types.String `tfsdk:"id"`
	// Database contains the model.
	Database types.String `tfsdk:"database"`
	// Schema contains the model and its inference function.
	Schema types.String `tfsdk:"schema"`
	// Name identifies the model within its schema.
	Name types.String `tfsdk:"name"`
	// Function names the inference function the model creates.
	Function types.String `tfsdk:"function"`
	// IAMRole is DEFAULT or the ARN of the role that invokes Amazon Bedrock.
	IAMRole types.String `tfsdk:"iam_role"`
	// ModelID is the Amazon Bedrock model identifier.
	ModelID types.String `tfsdk:"model_id"`
	// Prompt is the static prefix of every UNIFIED request.
	Prompt types.String `tfsdk:"prompt"`
	// Suffix is the static suffix of every UNIFIED request.
	Suffix types.String `tfsdk:"suffix"`
	// RequestType is RAW or UNIFIED.
	RequestType types.String `tfsdk:"request_type"`
	// ResponseType is VARCHAR or SUPER.
	ResponseType types.String `tfsdk:"response_type"`
	// Owner is the SQL user owning the model.
	Owner types.String `tfsdk:"owner"`
}

var _ = registerResource(newExternalModelResource)

// newExternalModelResource constructs an external model lifecycle handler.
func newExternalModelResource() resource.Resource { return &externalModelResource{} }

// Metadata identifies the external model resource to Terraform.
func (r *externalModelResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_external_model"
}

// externalModelRoleChanged replaces the model unless DEFAULT is only respelled in another case; Redshift has no
// statement that changes a model's role. An unread role after import is adopted without replacement.
func externalModelRoleChanged(ctx context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
	if externalModelUnread(ctx, req.Private, "iam_role") {
		return
	}
	before, after := req.StateValue.ValueString(), req.PlanValue.ValueString()
	respelled := strings.EqualFold(before, "DEFAULT") && strings.EqualFold(after, "DEFAULT")
	resp.RequiresReplace = req.PlanValue.IsUnknown() || (before != after && !respelled)
}

// Import cannot recover settings that SHOW MODEL omits. Keep their adoption distinct from later changes.
const externalModelUnreadPrefix = "external_model_unread_"

type externalModelPrivate interface {
	GetKey(context.Context, string) ([]byte, diag.Diagnostics)
}

func externalModelUnread(ctx context.Context, private externalModelPrivate, name string) bool {
	if private == nil {
		return false
	}
	value, _ := private.GetKey(ctx, externalModelUnreadPrefix+name)
	return len(value) != 0
}

func externalModelResponseChanged(ctx context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
	if !externalModelUnread(ctx, req.Private, "response_type") {
		keywordChanged(ctx, req, resp)
	}
}

// externalModelString is a string argument that replaces the model, because Redshift has no ALTER MODEL.
func externalModelString(required bool, description string) schema.StringAttribute {
	return schema.StringAttribute{
		Required: required, Optional: !required, MarkdownDescription: description,
		Validators:    []validator.String{stringvalidator.LengthAtLeast(1)},
		PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
	}
}

// externalModelFormat is a keyword-like format with a Redshift default; another case is recorded in place.
func externalModelFormat(description, defaultValue string, choices ...string) schema.StringAttribute {
	modifier := keywordChanged
	if defaultValue == "VARCHAR" {
		modifier = externalModelResponseChanged
	}
	return schema.StringAttribute{
		Optional: true, Computed: true, Default: stringdefault.StaticString(defaultValue), MarkdownDescription: description,
		Validators:    []validator.String{stringvalidator.OneOfCaseInsensitive(choices...)},
		PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplaceIf(modifier, "Changing it replaces the model.", "Changing it replaces the model.")},
	}
}

// Schema defines the model identity, its inference function, and its Amazon Bedrock settings.
func (r *externalModelResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages an Amazon Bedrock model created with `CREATE EXTERNAL MODEL` and the inference function it creates. Redshift has no `ALTER MODEL`, so every argument replaces the model.",
		Attributes: map[string]schema.Attribute{
			"id":       idAttribute(),
			"database": externalModelString(true, "Local database containing the model. Changing it replaces the model."),
			"schema":   externalModelString(true, "Existing schema containing the model and its inference function. Changing it replaces the model."),
			"name":     externalModelString(true, "Model name, unique among the models of the schema. Changing it replaces the model."),
			"function": externalModelString(true, "Name of the inference function the model creates in `schema`. The model owns the function: `DROP MODEL` drops it, `EXECUTE` is granted on the model, and `redshift_function` must not manage it. Changing it replaces the model."),
			"iam_role": schema.StringAttribute{
				Required: true, MarkdownDescription: "`DEFAULT`, in any case, for the warehouse's default IAM role, or the ARN of an associated IAM role allowed to invoke the Amazon Bedrock model. Changing it replaces the model; another case of `DEFAULT` is recorded in place.",
				Validators:    []validator.String{stringvalidator.LengthAtLeast(1)},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplaceIf(externalModelRoleChanged, "Changing it replaces the model.", "Changing it replaces the model.")},
			},
			"model_id": externalModelString(true, "Amazon Bedrock model ID, such as `anthropic.claude-v2`. Changing it replaces the model."),
			"prompt":   externalModelString(false, "Static prompt prefix added to the beginning of every request; only with `request_type = \"UNIFIED\"`. Changing it replaces the model."),
			"suffix":   externalModelString(false, "Static prompt suffix added to the end of every request; only with `request_type = \"UNIFIED\"`. Changing it replaces the model."),
			"request_type": externalModelFormat("Request format: `UNIFIED` (the default) uses the Amazon Bedrock Converse API and an inference function taking the input text with optional settings; `RAW` passes one `SUPER` request in the model's own format. Changing it replaces the model; another case is recorded in place.",
				"UNIFIED", "UNIFIED", "RAW"),
			"response_type": externalModelFormat("Response format: `VARCHAR` (the default) returns the generated text, `SUPER` the whole response. `RAW` requests require `SUPER`. Changing it replaces the model; another case is recorded in place.",
				"VARCHAR", "VARCHAR", "SUPER"),
			"owner": schema.StringAttribute{
				Computed: true, MarkdownDescription: "SQL user owning the model, as `SHOW MODEL` reports it; null when it is not reported.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

// ValidateConfig reports settings Create would reject, during planning once the configuration is known.
func (r *externalModelResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data externalModelModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !data.IAMRole.IsUnknown() && !data.IAMRole.IsNull() {
		if _, err := externalModelIAMRole(data.IAMRole.ValueString()); err != nil {
			resp.Diagnostics.AddError("Invalid external model", err.Error())
		}
	}
	if strings.EqualFold(knownString(data.RequestType), "RAW") {
		if !data.ResponseType.IsUnknown() && !strings.EqualFold(knownString(data.ResponseType), "SUPER") {
			resp.Diagnostics.AddError("Invalid external model", "request_type RAW requires response_type SUPER")
		}
		if knownString(data.Prompt) != "" || knownString(data.Suffix) != "" {
			resp.Diagnostics.AddError("Invalid external model", "prompt and suffix are only supported with request_type UNIFIED")
		}
	}
	if resp.Diagnostics.HasError() || !req.Config.Raw.IsFullyKnown() {
		return
	}
	if _, err := createExternalModelStatement(data); err != nil {
		resp.Diagnostics.AddError("Invalid external model", err.Error())
	}
}

// readExternalModelCatalog finds a model in SHOW MODEL ALL and returns its SHOW MODEL settings. A missing database
// or model is reported as not found, so refresh removes the state of a model whose parent is gone.
func readExternalModelCatalog(ctx context.Context, client *resourceClient, database, schemaName, name string) (map[string]string, bool, error) {
	statement, err := readExternalModelStatement(schemaName, name)
	if err != nil {
		return nil, false, err
	}
	if exists, err := client.localDatabaseExists(ctx, database); err != nil || !exists {
		return nil, false, err
	}
	rows, err := client.queryDatabase(ctx, database, listModelsStatement(), nil)
	if err != nil {
		return nil, false, err
	}
	models, err := parseShowModels(rows)
	if err != nil {
		return nil, false, err
	}
	if !slices.Contains(models, showModelEntry{schema: schemaName, name: name}) {
		return nil, false, nil
	}
	rows, err = client.queryDatabase(ctx, database, statement, nil)
	if err != nil || len(rows) == 0 {
		return nil, false, err
	}
	return parseShowModel(rows), true, nil
}

// externalModelDefaultRole is how SHOW MODEL prints IAM_ROLE DEFAULT to the model's owner; other users see no role.
const externalModelDefaultRole = "default-aws-iam-role"

// observeExternalModel updates data with the settings SHOW MODEL reports and keeps the others. The keys of
// external models are undocumented, so each setting is read only where its key is present, and a configured
// spelling that names the reported value is kept: a function name without its schema, DEFAULT for whichever
// role the catalog shows, and keywords in another case.
func observeExternalModel(data *externalModelModel, settings map[string]string) {
	if owner := settings["owner"]; owner != "" {
		data.Owner = types.StringValue(owner)
	} else if data.Owner.IsUnknown() {
		data.Owner = types.StringNull()
	}
	if function := settings["function_name"]; function != "" {
		parts := strings.Split(function, ".")
		function = strings.Trim(parts[len(parts)-1], `"`)
		if knownString(data.Function) != function {
			data.Function = types.StringValue(function)
		}
	}
	if role := settings["iam_role"]; role != "" {
		switch {
		case strings.EqualFold(role, "DEFAULT") || role == externalModelDefaultRole:
			data.IAMRole = keywordValue(data.IAMRole, "DEFAULT")
		case !strings.EqualFold(knownString(data.IAMRole), "DEFAULT") && knownString(data.IAMRole) != role:
			data.IAMRole = types.StringValue(role)
		}
	}
	// SHOW MODEL values arrive trimmed, so a configured prompt such as "Translate: " is kept while it matches after
	// the same normalization.
	for key, field := range map[string]*types.String{"model_id": &data.ModelID, "prompt": &data.Prompt, "suffix": &data.Suffix} {
		if value := settings[key]; value != "" && showModelValue(knownString(*field)) != value {
			*field = types.StringValue(value)
		}
	}
	for key, field := range map[string]*types.String{"request_type": &data.RequestType, "response_type": &data.ResponseType} {
		if value := settings[key]; value != "" {
			*field = keywordValue(*field, strings.ToUpper(value))
		}
	}
}

// defaultExternalModelResponse preserves the defaulting behavior of imports without private adoption markers.
// New imports and lookups leave an unread response type null instead of guessing.
func defaultExternalModelResponse(data *externalModelModel) {
	if !data.ResponseType.IsNull() && !data.ResponseType.IsUnknown() {
		return
	}
	data.ResponseType = types.StringValue("VARCHAR")
	if strings.EqualFold(knownString(data.RequestType), "RAW") {
		data.ResponseType = types.StringValue("SUPER")
	}
}

// read checks the binding and reads the model's catalog settings into data.
func (r *externalModelResource) read(ctx context.Context, data *externalModelModel) (bool, error) {
	if err := r.bound(data.ID, data.Database.ValueString()); err != nil {
		return false, err
	}
	settings, found, err := readExternalModelCatalog(ctx, &r.resourceClient, data.Database.ValueString(), data.Schema.ValueString(), data.Name.ValueString())
	if err == nil && found {
		observeExternalModel(data, settings)
	}
	return found, err
}

// converge re-reads the model after apply and fails when a setting the catalog reports differs from the plan.
func (r *externalModelResource) converge(ctx context.Context, data *externalModelModel, planned externalModelModel) error {
	found, err := r.read(ctx, data)
	switch {
	case err != nil:
		return err
	case !found:
		return errors.New("the model is absent after apply")
	}
	for name, values := range map[string][2]types.String{
		"function": {data.Function, planned.Function}, "iam_role": {data.IAMRole, planned.IAMRole}, "model_id": {data.ModelID, planned.ModelID},
		"prompt": {data.Prompt, planned.Prompt}, "suffix": {data.Suffix, planned.Suffix},
		"request_type": {data.RequestType, planned.RequestType}, "response_type": {data.ResponseType, planned.ResponseType},
	} {
		if !values[0].Equal(values[1]) {
			return fmt.Errorf("the catalog reports %s %s instead of %s", name, values[0], values[1])
		}
	}
	return nil
}

// Create validates the settings, creates the model, and verifies the result.
func (r *externalModelResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data externalModelModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	statement, err := createExternalModelStatement(data)
	if err != nil {
		resp.Diagnostics.AddError("Invalid external model", err.Error())
		return
	}
	if err := r.exec(ctx, data.Database.ValueString(), statement); err != nil {
		resp.Diagnostics.AddError("Create external model", err.Error())
		return
	}
	planned := data
	data.ID = r.identity(data.Database.ValueString(), map[string]string{"schema": data.Schema.ValueString(), "name": data.Name.ValueString()})
	// A failed verification must still record the created model in serializable state.
	if data.Owner.IsUnknown() {
		data.Owner = types.StringNull()
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.converge(ctx, &data, planned); err != nil {
		resp.Diagnostics.AddError("Verify external model", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Read refreshes the reported settings or removes a model that is gone.
func (r *externalModelResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data externalModelModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.bound(data.ID, data.Database.ValueString()); err != nil {
		resp.Diagnostics.AddError("Read external model", err.Error())
		return
	}
	settings, found, err := readExternalModelCatalog(ctx, &r.resourceClient, data.Database.ValueString(), data.Schema.ValueString(), data.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Read external model", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	observeExternalModel(&data, settings)
	// An unread import must plan an adoption update even when configuration uses the default response type.
	// Guessing VARCHAR here could make that apply a no-op and leave the adoption exemption active indefinitely.
	if externalModelUnread(ctx, req.Private, "response_type") && settings["response_type"] == "" {
		data.ResponseType = types.StringNull()
	} else {
		defaultExternalModelResponse(&data)
	}
	if resp.Private != nil {
		for _, name := range []string{"iam_role", "response_type"} {
			if settings[name] != "" {
				resp.Diagnostics.Append(resp.Private.SetKey(ctx, externalModelUnreadPrefix+name, nil)...)
			}
		}
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update runs no SQL: every real change replaces the model, so only a keyword respelled in another case reaches it.
// The model must still exist, so the recorded spelling never describes a dropped model.
func (r *externalModelResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data externalModelModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.converge(ctx, &data, data); err != nil {
		resp.Diagnostics.AddError("Update external model", err.Error())
		return
	}
	if resp.Private != nil {
		for _, name := range []string{"iam_role", "response_type"} {
			resp.Diagnostics.Append(resp.Private.SetKey(ctx, externalModelUnreadPrefix+name, nil)...)
		}
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Delete drops the model, which drops its inference function too, and verifies catalog removal.
func (r *externalModelResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data externalModelModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, &data)
	if err == nil && found {
		var statement string
		if statement, err = dropExternalModelStatement(data); err == nil {
			err = r.exec(ctx, data.Database.ValueString(), statement)
		}
		if err == nil {
			if found, err = r.read(ctx, &data); err == nil && found {
				resp.Diagnostics.AddError("Delete external model", "The model remains after deletion.")
			}
		}
	}
	if err != nil {
		resp.Diagnostics.AddError("Delete external model", err.Error())
	}
}

// ImportState restores the database/schema/name binding from JSON; Read then fills the settings SHOW MODEL reports.
func (r *externalModelResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importIdentity(ctx, req, resp, "database", "schema", "name")
	if !resp.Diagnostics.HasError() && resp.Private != nil {
		for _, name := range []string{"iam_role", "response_type"} {
			resp.Diagnostics.Append(resp.Private.SetKey(ctx, externalModelUnreadPrefix+name, []byte("true"))...)
		}
	}
}
