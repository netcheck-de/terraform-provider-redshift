package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// identityProviderResource manages the SQL side of an AWS Identity Center or Microsoft Entra ID (Azure) integration.
type identityProviderResource struct {
	// resourceClient provides SQL execution and warehouse ownership checks.
	resourceClient
}

// identityProviderModel is the Terraform state for one SQL identity provider. Audience is a plain slice rather than
// types.Set because a zero-value types.Set has no element type and cannot be stored, while a nil slice is null.
type identityProviderModel struct {
	// ID records the administration binding and integration name.
	ID types.String `tfsdk:"id"`
	// Name identifies the SQL identity provider.
	Name types.String `tfsdk:"name"`
	// Type selects AWSIDC or Azure.
	Type types.String `tfsdk:"type"`
	// Namespace prefixes federated SQL identities and group roles.
	Namespace types.String `tfsdk:"namespace"`
	// ApplicationARN binds the existing AWS managed application (AWSIDC).
	ApplicationARN types.String `tfsdk:"application_arn"`
	// IAMRoleARN selects the attached Identity Center integration role (AWSIDC).
	IAMRoleARN types.String `tfsdk:"iam_role_arn"`
	// Issuer is the Azure token issuer URL.
	Issuer types.String `tfsdk:"issuer"`
	// ClientID is the Azure application (client) ID.
	ClientID types.String `tfsdk:"client_id"`
	// Audience lists the accepted Azure token audiences.
	Audience []string `tfsdk:"audience"`
	// ClientSecret is the write-only Azure client secret and stays null in plan and state.
	ClientSecret types.String `tfsdk:"client_secret_wo"`
	// ClientSecretVersion explicitly requests sending a new client secret when changed.
	ClientSecretVersion types.Int64 `tfsdk:"client_secret_wo_version"`
	// AutoCreateRoles turns automatic group-role creation on or off; null keeps the type default.
	AutoCreateRoles types.Bool `tfsdk:"auto_create_roles"`
	// IncludeGroups limits automatic role creation to groups matching a LIKE pattern.
	IncludeGroups types.String `tfsdk:"auto_create_roles_include_groups"`
	// ExcludeGroups skips automatic role creation for groups matching a LIKE pattern.
	ExcludeGroups types.String `tfsdk:"auto_create_roles_exclude_groups"`
	// Enabled controls SQL identity-provider availability.
	Enabled types.Bool `tfsdk:"enabled"`
	// ProviderID is the catalog uid.
	ProviderID types.Int64 `tfsdk:"provider_id"`
	// InstanceID is the catalog instance identifier.
	InstanceID types.String `tfsdk:"instance_id"`
	// IdentityCenterInstanceARN is the Identity Center instance of an AWSIDC provider.
	IdentityCenterInstanceARN types.String `tfsdk:"identity_center_instance_arn"`
}

var _ = registerResource(newIdentityProviderResource)

// newIdentityProviderResource constructs an identity provider integration handler.
func newIdentityProviderResource() resource.Resource { return &identityProviderResource{} }

// Metadata identifies the identity provider resource to Terraform.
func (r *identityProviderResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_identity_provider"
}

// identityProviderGroupPattern accepts the characters r_CREATE_IDENTITY_PROVIDER allows in a group filter pattern.
var identityProviderGroupPattern = regexp.MustCompile(`^[A-Za-z0-9_%^*+?{},$]+$`)

// identityProviderGroupFilter defines one of the mutually exclusive AUTO_CREATE_ROLES group filters.
func identityProviderGroupFilter(keyword, other string) schema.StringAttribute {
	return schema.StringAttribute{
		Optional: true,
		MarkdownDescription: "Case-sensitive `LIKE` pattern (`" + keyword + " GROUPS LIKE`) of the identity-provider groups for which Redshift " +
			map[string]string{"INCLUDE": "creates", "EXCLUDE": "does not create"}[keyword] +
			" roles automatically. Requires `auto_create_roles = true`; conflicts with `" + other + "`. Patterns use letters, digits, and `_ % ^ * + ? { } , $`. Not reported by the catalog, like `auto_create_roles`. Updated in place.",
		Validators: []validator.String{
			stringvalidator.RegexMatches(identityProviderGroupPattern, "must contain only letters, digits, and _ % ^ * + ? { } , $"),
			stringvalidator.ConflictsWith(path.MatchRoot(other)),
		},
	}
}

// Schema defines the provider type, its type-specific binding, role creation, and enabled state.
func (r *identityProviderResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a SQL identity provider: an AWS IAM Identity Center (`awsidc`) or Microsoft Entra ID (`azure`) integration. AWS applications, IAM roles, and directory groups are managed separately.",
		Attributes: map[string]schema.Attribute{
			"id": idAttribute(),
			"name": schema.StringAttribute{
				Required: true, MarkdownDescription: "SQL identity provider name. Changing it replaces the identity provider.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"type": schema.StringAttribute{
				Optional: true, Computed: true, Default: stringdefault.StaticString(identityProviderAWSIDC),
				MarkdownDescription: "Identity provider type: `awsidc` (AWS IAM Identity Center) or `azure` (Microsoft Entra ID, native IdP federation). Defaults to `awsidc`. Changing it replaces the identity provider.",
				Validators:          []validator.String{stringvalidator.OneOf(identityProviderAWSIDC, identityProviderAzure)},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"namespace": schema.StringAttribute{
				Required: true, MarkdownDescription: "Prefix of federated users and group roles (`namespace:name`). Updated in place with `ALTER IDENTITY PROVIDER ... NAMESPACE`; existing users and roles keep their names, so the change is refused while federated users with the previous prefix exist. Rename or remove them first.",
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"application_arn": schema.StringAttribute{
				Optional: true, MarkdownDescription: "Identity Center managed application ARN. Required for `awsidc`; not allowed for `azure`. Changing it replaces the identity provider.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"iam_role_arn": schema.StringAttribute{
				Optional: true, MarkdownDescription: "IAM role attached to the Redshift namespace for the Identity Center connection. Required for `awsidc`; not allowed for `azure`. Updated in place and reapplied on every update.",
			},
			"issuer": schema.StringAttribute{
				Optional: true, MarkdownDescription: "Token issuer URL of the Microsoft Entra ID tenant (`issuer` in `PARAMETERS`). Required for `azure`; not allowed for `awsidc`. Updated in place together with `client_secret_wo`.",
			},
			"client_id": schema.StringAttribute{
				Optional: true, MarkdownDescription: "Application (client) ID of the Redshift application registered in Microsoft Entra ID (`client_id` in `PARAMETERS`). Required for `azure`; not allowed for `awsidc`. Updated in place together with `client_secret_wo`.",
			},
			"audience": schema.SetAttribute{
				Optional: true, ElementType: types.StringType,
				MarkdownDescription: "Accepted token audiences (`audience` in `PARAMETERS`), for example the Power BI connector. Only for `azure`. Updated in place together with `client_secret_wo`.",
				Validators:          []validator.Set{setvalidator.SizeAtLeast(1)},
			},
			"client_secret_wo": schema.StringAttribute{
				Optional: true, WriteOnly: true, Sensitive: true,
				MarkdownDescription: "Write-only client secret of the Microsoft Entra ID application (requires Terraform 1.11 or later); not allowed for `awsidc`. Required to create an `azure` provider and whenever `issuer`, `client_id`, `audience`, or `client_secret_wo_version` change, because `ALTER IDENTITY PROVIDER ... PARAMETERS` replaces every parameter. Never stored in plan or state, and the catalog never returns it.",
			},
			"client_secret_wo_version": schema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: "Secret rotation trigger. Required for `azure`; not allowed for `awsidc`. Change it to send `client_secret_wo` again. An imported provider has no version in state, so setting it for the first time after an import does not send the secret.",
			},
			"auto_create_roles": schema.BoolAttribute{
				Optional:            true,
				MarkdownDescription: "Whether Redshift creates roles for the provider's groups automatically (`AUTO_CREATE_ROLES`). Unset keeps the type default, `false` for `awsidc` and `true` for `azure`, and removing the attribute restores that default. The catalog does not report this setting, so Terraform keeps the configured value, changes made outside Terraform are not detected, and imports and lookups report null. Updated in place.",
			},
			"auto_create_roles_include_groups": identityProviderGroupFilter("INCLUDE", "auto_create_roles_exclude_groups"),
			"auto_create_roles_exclude_groups": identityProviderGroupFilter("EXCLUDE", "auto_create_roles_include_groups"),
			"enabled": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(true),
				MarkdownDescription: "Whether the provider is enabled; defaults to `true`. Updated in place and reapplied on every update.",
			},
			"provider_id": schema.Int64Attribute{
				Computed: true, MarkdownDescription: "Catalog ID of the identity provider (`svv_identity_providers.uid`).",
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"instance_id": schema.StringAttribute{
				Computed: true, PlanModifiers: []planmodifier.String{identityProviderInstanceModifier{}}, MarkdownDescription: "Catalog instance identifier (`svv_identity_providers.instanceid`): the application ARN for `awsidc` and the tenant ID for `azure`.",
			},
			"identity_center_instance_arn": schema.StringAttribute{
				Computed: true, PlanModifiers: []planmodifier.String{identityProviderInstanceModifier{}}, MarkdownDescription: "IAM Identity Center instance ARN of an `awsidc` provider (`instance_arn` in the catalog parameters); null for `azure`.",
			},
		},
	}
}

// identityProviderInstanceModifier keeps the catalog instance identifiers through updates. For awsidc they follow the
// application ARN, which replaces the provider; for azure the instance ID is the tenant of the issuer, so an issuer
// change leaves them unknown.
type identityProviderInstanceModifier struct{}

// Description explains the modifier in plain text.
func (identityProviderInstanceModifier) Description(context.Context) string {
	return "Keeps the prior value unless the issuer changes."
}

// MarkdownDescription explains the modifier in Markdown.
func (m identityProviderInstanceModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

// PlanModifyString copies the prior value into an unknown plan value while the issuer stays the same.
func (identityProviderInstanceModifier) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if req.State.Raw.IsNull() || !req.PlanValue.IsUnknown() || req.ConfigValue.IsUnknown() {
		return
	}
	var prior, planned types.String
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("issuer"), &prior)...)
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("issuer"), &planned)...)
	if !resp.Diagnostics.HasError() && prior.Equal(planned) {
		resp.PlanValue = req.StateValue
	}
}

// identityProviderCatalogParameters are the catalog parameters of either provider type.
type identityProviderCatalogParameters struct {
	// IAMRole is the AWSIDC integration role.
	IAMRole string `json:"iam_role"`
	// InstanceARN is the AWSIDC Identity Center instance.
	InstanceARN string `json:"instance_arn"`
	// Issuer is the Azure token issuer.
	Issuer string `json:"issuer"`
	// ClientID is the Azure application ID.
	ClientID string `json:"client_id"`
	// Audience lists the Azure token audiences.
	Audience []string `json:"audience"`
}

// identityProviderRedactedSecret matches the redacted Azure secret, which svv_identity_providers and DESC IDENTITY
// PROVIDER print as "client_secret":, or "client_secret":”, neither of which is valid JSON.
var identityProviderRedactedSecret = regexp.MustCompile(`("client_secret"\s*:\s*)(?:'')?(\s*[,}])`)

// decodeIdentityProviderParameters parses the catalog parameters after making the redacted secret valid JSON.
func decodeIdentityProviderParameters(text string) (identityProviderCatalogParameters, error) {
	var parameters identityProviderCatalogParameters
	err := json.Unmarshal([]byte(identityProviderRedactedSecret.ReplaceAllString(text, "${1}null${2}")), &parameters)
	return parameters, err
}

// identityProviderOptional maps an empty catalog value, which is how both transports return NULL, to null.
func identityProviderOptional(value string) types.String {
	if value == "" {
		return types.StringNull()
	}
	return types.StringValue(value)
}

// read verifies the provider binding and refreshes its observable settings. auto_create_roles, its filters, and the
// secret version are not in the catalog and keep their state.
func (r *identityProviderResource) read(ctx context.Context, data *identityProviderModel) (bool, error) {
	if err := r.bound(data.ID, r.database.ValueString()); err != nil {
		return false, err
	}
	rows, err := r.selectRows(ctx, r.database.ValueString(), readIdentityProviderQuery(*data))
	if err != nil || len(rows) == 0 {
		return false, err
	}
	row := rows[0]
	if len(rows) != 1 || row["namespc"] == "" {
		return false, fmt.Errorf("identity provider %q has incomplete or ambiguous catalog metadata", data.Name.ValueString())
	}
	parameters, err := decodeIdentityProviderParameters(row["params"])
	if err != nil {
		return false, fmt.Errorf("decode identity provider parameters: %w", err)
	}
	enabled, err := strconv.ParseBool(row["enabled"])
	if err != nil {
		return false, fmt.Errorf("decode identity provider enabled flag: %w", err)
	}
	data.ApplicationARN, data.IAMRoleARN, data.IdentityCenterInstanceARN = types.StringNull(), types.StringNull(), types.StringNull()
	data.Issuer, data.ClientID, data.Audience = types.StringNull(), types.StringNull(), nil
	switch row["type"] {
	case identityProviderAWSIDC:
		if row["instanceid"] == "" {
			return false, fmt.Errorf("identity provider %q has incomplete or ambiguous catalog metadata", data.Name.ValueString())
		}
		if parameters.IAMRole == "" {
			return false, fmt.Errorf("identity provider has no IAM role in its catalog parameters")
		}
		data.ApplicationARN = types.StringValue(row["instanceid"])
		data.IAMRoleARN = types.StringValue(parameters.IAMRole)
		data.IdentityCenterInstanceARN = identityProviderOptional(parameters.InstanceARN)
	case identityProviderAzure:
		if parameters.Issuer == "" || parameters.ClientID == "" {
			return false, fmt.Errorf("identity provider %q has no issuer or client_id in its catalog parameters", data.Name.ValueString())
		}
		data.Issuer, data.ClientID = types.StringValue(parameters.Issuer), types.StringValue(parameters.ClientID)
		if len(parameters.Audience) > 0 {
			data.Audience = identityProviderAudience(parameters.Audience)
		}
	default:
		return false, fmt.Errorf("identity provider %q has unsupported type %q", data.Name.ValueString(), row["type"])
	}
	data.ProviderID = types.Int64Null()
	if row["uid"] != "" {
		id, err := strconv.ParseInt(row["uid"], 10, 64)
		if err != nil {
			return false, fmt.Errorf("decode identity provider uid: %w", err)
		}
		data.ProviderID = types.Int64Value(id)
	}
	data.Name = types.StringValue(row["name"])
	data.Type = types.StringValue(row["type"])
	data.Namespace = types.StringValue(row["namespc"])
	data.InstanceID = identityProviderOptional(row["instanceid"])
	data.Enabled = types.BoolValue(enabled)
	return true, nil
}

// identityProviderConverged reports whether the observed provider matches every planned, observable setting.
func identityProviderConverged(planned, observed identityProviderModel) bool {
	for _, pair := range [][2]types.String{
		{planned.Namespace, observed.Namespace}, {planned.ApplicationARN, observed.ApplicationARN}, {planned.IAMRoleARN, observed.IAMRoleARN},
		{planned.Issuer, observed.Issuer}, {planned.ClientID, observed.ClientID},
	} {
		if knownString(pair[0]) != knownString(pair[1]) {
			return false
		}
	}
	enabled := knownBool(planned.Enabled) == nil || planned.Enabled.ValueBool()
	return identityProviderType(planned) == identityProviderType(observed) && enabled == observed.Enabled.ValueBool() &&
		slices.Equal(identityProviderAudience(planned.Audience), identityProviderAudience(observed.Audience))
}

// identityProviderSecret returns the configured client secret, which the plan never carries; a missing secret is
// reported by the renderers that need it.
func identityProviderSecret(ctx context.Context, config tfsdk.Config) string {
	var value types.String
	if config.Schema == nil || config.GetAttribute(ctx, path.Root("client_secret_wo"), &value).HasError() {
		return ""
	}
	return knownString(value)
}

// ValidateConfig reports type-specific attribute combinations during planning. The attributes are read one by one,
// because an unknown audience cannot be decoded into the model's slice, and only checks whose inputs are unknown are
// deferred to apply. The configuration is the only place the write-only secret is visible at plan time.
func (r *identityProviderResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	inputs := identityProviderInputs{}
	for _, name := range identityProviderInputNames {
		var value attr.Value
		resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root(name), &value)...)
		inputs[name] = value
	}
	if resp.Diagnostics.HasError() {
		return
	}
	if err := validateIdentityProviderInputs(inputs); err != nil {
		resp.Diagnostics.AddError("Invalid identity provider", err.Error())
	}
}

// Create registers the SQL identity provider and applies its enabled state.
func (r *identityProviderResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data identityProviderModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	statement, err := createIdentityProviderStatement(data, identityProviderSecret(ctx, req.Config))
	if err == nil {
		err = r.exec(ctx, r.database.ValueString(), statement)
	}
	if err != nil {
		resp.Diagnostics.AddError("Create identity provider", err.Error())
		return
	}
	data.ID = r.identity(r.database.ValueString(), map[string]string{"name": data.Name.ValueString()})
	// The provider exists now, so its identity is saved before anything else can fail; unknown values cannot be saved.
	data.ProviderID, data.InstanceID, data.IdentityCenterInstanceARN = types.Int64Null(), types.StringNull(), types.StringNull()
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	if !data.Enabled.ValueBool() {
		if err := r.exec(ctx, r.database.ValueString(), identityProviderStatusStatement(data)); err != nil {
			resp.Diagnostics.AddError("Disable identity provider", err.Error())
			return
		}
	}
	expected := data
	found, err := r.read(ctx, &data)
	if err != nil {
		resp.Diagnostics.AddError("Verify identity provider", err.Error())
		return
	}
	if !found || !identityProviderConverged(expected, data) {
		resp.Diagnostics.AddError("Verify identity provider", "The catalog does not match the identity provider configuration after creation.")
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Read refreshes the integration or removes a missing provider from state.
func (r *identityProviderResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data identityProviderModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, &data)
	if err != nil {
		resp.Diagnostics.AddError("Read identity provider", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update reconciles the integration in place without rebinding the application or changing the type.
func (r *identityProviderResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data, previous identityProviderModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &previous)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.bound(previous.ID, r.database.ValueString()); err != nil {
		resp.Diagnostics.AddError("Update identity provider", err.Error())
		return
	}
	role, options, status, err := identityProviderAlterBatches(previous, data, identityProviderSecret(ctx, req.Config))
	if err != nil {
		resp.Diagnostics.AddError("Update identity provider", err.Error())
		return
	}
	// Redshift keeps the names of existing users when the namespace changes, so Delete, which looks for the current
	// prefix, would no longer see them.
	if !previous.Namespace.Equal(data.Namespace) && r.federatedUsersRemain(ctx, previous, "Update identity provider", &resp.Diagnostics) {
		return
	}
	// Each batch keeps its own summary so a failure names the setting that Redshift refused.
	for _, batch := range []struct {
		summary    string
		statements []string
	}{{"Update identity provider role", role}, {"Update identity provider", options}, {"Update identity provider status", status}} {
		if err := r.exec(ctx, r.database.ValueString(), batch.statements...); err != nil {
			resp.Diagnostics.AddError(batch.summary, err.Error())
			return
		}
	}
	expected := data
	found, err := r.read(ctx, &data)
	if err != nil {
		resp.Diagnostics.AddError("Verify identity provider", err.Error())
		return
	}
	if !found || !identityProviderConverged(expected, data) {
		resp.Diagnostics.AddError("Verify identity provider", "The catalog does not match the identity provider configuration after updating.")
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Delete refuses unmanaged namespaced identities before removing the integration.
func (r *identityProviderResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data identityProviderModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	expectedNamespace, expectedApplication := data.Namespace, data.ApplicationARN
	found, err := r.read(ctx, &data)
	if err != nil {
		resp.Diagnostics.AddError("Read identity provider", err.Error())
		return
	}
	if !found {
		return
	}
	if !data.Namespace.Equal(expectedNamespace) || knownString(data.ApplicationARN) != knownString(expectedApplication) {
		resp.Diagnostics.AddError("Delete identity provider", "The identity binding changed; migrate it explicitly before deletion.")
		return
	}
	if r.federatedUsersRemain(ctx, data, "Delete identity provider", &resp.Diagnostics) {
		return
	}
	if err := r.exec(ctx, r.database.ValueString(), dropIdentityProviderStatement(data)); err != nil {
		resp.Diagnostics.AddError("Delete identity provider", err.Error())
		return
	}
	found, err = r.read(ctx, &data)
	if err != nil {
		resp.Diagnostics.AddError("Verify identity provider deletion", err.Error())
	} else if found {
		resp.Diagnostics.AddError("Verify identity provider deletion", "The identity provider remains after deletion.")
	}
}

// federatedUsersRemain reports, with an error under summary, whether users carrying data's namespace prefix exist or
// cannot be listed.
func (r *identityProviderResource) federatedUsersRemain(ctx context.Context, data identityProviderModel, summary string, diagnostics *diag.Diagnostics) bool {
	users, err := r.selectRows(ctx, r.database.ValueString(), readIdentityProviderUsersQuery(data))
	if err != nil {
		diagnostics.AddError("Inspect federated users", err.Error())
		return true
	}
	if len(users) > 0 {
		diagnostics.AddError(summary, fmt.Sprintf("Federated users with the namespace prefix %q still exist; migrate or remove them explicitly first.", data.Namespace.ValueString()+":"))
		return true
	}
	return false
}

// ImportState restores identity provider ownership from its stable JSON identity.
func (r *identityProviderResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importIdentity(ctx, req, resp, "name")
}
