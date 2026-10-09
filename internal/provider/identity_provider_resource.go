package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// identityProviderResource manages the SQL side of an AWS Identity Center integration.
type identityProviderResource struct {
	// resourceClient provides SQL execution and warehouse ownership checks.
	resourceClient
}

// identityProviderModel is the Terraform state for one AWSIDC SQL integration.
type identityProviderModel struct {
	// ID records the administration binding and integration name.
	ID types.String `tfsdk:"id"`
	// Name identifies the SQL identity provider.
	Name types.String `tfsdk:"name"`
	// Namespace prefixes federated SQL identities and group roles.
	Namespace types.String `tfsdk:"namespace"`
	// ApplicationARN binds the existing AWS managed application.
	ApplicationARN types.String `tfsdk:"application_arn"`
	// IAMRoleARN selects the attached Identity Center integration role.
	IAMRoleARN types.String `tfsdk:"iam_role_arn"`
	// Enabled controls SQL identity-provider availability.
	Enabled types.Bool `tfsdk:"enabled"`
}

var _ = registerResource(newIdentityProviderResource)

// newIdentityProviderResource constructs an AWS Identity Center SQL integration handler.
func newIdentityProviderResource() resource.Resource { return &identityProviderResource{} }

// Metadata identifies the identity provider resource to Terraform.
func (r *identityProviderResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_identity_provider"
}

// Schema defines the AWSIDC binding, IAM integration role, and enabled state.
func (r *identityProviderResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages an AWSIDC SQL identity provider. AWS application and IAM resources are managed separately.",
		Attributes: map[string]schema.Attribute{
			"id": idAttribute(),
			"name": schema.StringAttribute{
				Required: true, MarkdownDescription: "SQL identity provider name; changing it replaces the resource.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"namespace": schema.StringAttribute{
				Required: true, MarkdownDescription: "Stable prefix for federated users and group roles; changing it replaces the resource.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"application_arn": schema.StringAttribute{
				Required: true, MarkdownDescription: "Identity Center managed application ARN; changing it replaces the resource.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"iam_role_arn": schema.StringAttribute{
				Required: true, MarkdownDescription: "IAM role attached to the Redshift namespace for Identity Center integration; updated in place.",
			},
			"enabled": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(true),
				MarkdownDescription: "Whether the provider is enabled; defaults to `true`. Updated in place.",
			},
		},
	}
}

// read verifies the AWSIDC application binding and refreshes mutable integration settings.
func (r *identityProviderResource) read(ctx context.Context, data *identityProviderModel) (bool, error) {
	if err := r.bound(data.ID, r.database.ValueString()); err != nil {
		return false, err
	}
	rows, err := r.selectRows(ctx, r.database.ValueString(), readIdentityProviderQuery(*data))
	if err != nil || len(rows) == 0 {
		return false, err
	}
	row := rows[0]
	if len(rows) != 1 || row["namespc"] == "" || row["instanceid"] == "" {
		return false, fmt.Errorf("identity provider %q has incomplete or ambiguous catalog metadata", data.Name.ValueString())
	}
	if row["type"] != "awsidc" {
		return false, fmt.Errorf("identity provider %q is not AWSIDC", data.Name.ValueString())
	}
	var parameters struct {
		IAMRole string `json:"iam_role"`
	}
	if err := json.Unmarshal([]byte(row["params"]), &parameters); err != nil {
		return false, fmt.Errorf("decode identity provider parameters: %w", err)
	}
	if parameters.IAMRole == "" {
		return false, fmt.Errorf("identity provider has no IAM role in its catalog parameters")
	}
	enabled, err := strconv.ParseBool(row["enabled"])
	if err != nil {
		return false, fmt.Errorf("decode identity provider enabled flag: %w", err)
	}
	data.Name = types.StringValue(row["name"])
	data.Namespace = types.StringValue(row["namespc"])
	data.ApplicationARN = types.StringValue(row["instanceid"])
	data.IAMRoleARN = types.StringValue(parameters.IAMRole)
	data.Enabled = types.BoolValue(enabled)
	return true, nil
}

// Create registers the SQL identity provider and applies its enabled state.
func (r *identityProviderResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data identityProviderModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	statement, err := createIdentityProviderStatement(data)
	if err == nil {
		err = r.exec(ctx, r.database.ValueString(), statement)
	}
	if err != nil {
		resp.Diagnostics.AddError("Create identity provider", err.Error())
		return
	}
	data.ID = r.identity(r.database.ValueString(), map[string]string{"name": data.Name.ValueString()})
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
	if !found || !data.Namespace.Equal(expected.Namespace) || !data.ApplicationARN.Equal(expected.ApplicationARN) || !data.IAMRoleARN.Equal(expected.IAMRoleARN) || !data.Enabled.Equal(expected.Enabled) {
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

// Update reconciles the integration role and enabled state without rebinding the application.
func (r *identityProviderResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data identityProviderModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.bound(data.ID, r.database.ValueString()); err != nil {
		resp.Diagnostics.AddError("Update identity provider", err.Error())
		return
	}
	if err := r.exec(ctx, r.database.ValueString(), alterIdentityProviderStatements(data)...); err != nil {
		resp.Diagnostics.AddError("Update identity provider", err.Error())
		return
	}
	expected := data
	found, err := r.read(ctx, &data)
	if err != nil {
		resp.Diagnostics.AddError("Verify identity provider", err.Error())
		return
	}
	if !found || !data.Namespace.Equal(expected.Namespace) || !data.ApplicationARN.Equal(expected.ApplicationARN) || !data.IAMRoleARN.Equal(expected.IAMRoleARN) || !data.Enabled.Equal(expected.Enabled) {
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
	if !data.Namespace.Equal(expectedNamespace) || !data.ApplicationARN.Equal(expectedApplication) {
		resp.Diagnostics.AddError("Delete identity provider", "The identity binding changed; migrate it explicitly before deletion.")
		return
	}
	users, err := r.selectRows(ctx, r.database.ValueString(), readIdentityProviderUsersQuery(data))
	if err != nil {
		resp.Diagnostics.AddError("Inspect federated users", err.Error())
		return
	}
	if len(users) > 0 {
		resp.Diagnostics.AddError("Delete identity provider", "Federated users still exist; migrate or remove them explicitly before deletion.")
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

// ImportState restores identity provider ownership from its stable JSON identity.
func (r *identityProviderResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importIdentity(ctx, req, resp, "name")
}
