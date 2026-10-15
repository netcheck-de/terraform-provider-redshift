package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// rlsPolicyAttachmentResource attaches one RLS policy to one relation for one recipient.
type rlsPolicyAttachmentResource struct {
	// resourceClient provides SQL execution and warehouse ownership checks.
	resourceClient
}

// rlsPolicyAttachmentModel is the Terraform state of one policy/relation/recipient attachment.
type rlsPolicyAttachmentModel struct {
	// ID records the warehouse/database/policy/relation/recipient identity.
	ID types.String `tfsdk:"id"`
	// Policy names the attached RLS policy.
	Policy types.String `tfsdk:"policy"`
	// Database contains the policy and the relation.
	Database types.String `tfsdk:"database"`
	// Schema contains the relation.
	Schema types.String `tfsdk:"schema"`
	// Relation is the protected table or view.
	Relation types.String `tfsdk:"relation"`
	// Grantee is the user or role name, or public.
	Grantee types.String `tfsdk:"grantee"`
	// GranteeType is USER, ROLE, or PUBLIC.
	GranteeType types.String `tfsdk:"grantee_type"`
}

var _ = registerResource(newRlsPolicyAttachmentResource)

// newRlsPolicyAttachmentResource constructs an RLS policy attachment handler.
func newRlsPolicyAttachmentResource() resource.Resource { return &rlsPolicyAttachmentResource{} }

// rlsPolicyAttachmentString defines one replacing identity attribute.
func rlsPolicyAttachmentString(description string, validators ...validator.String) schema.StringAttribute {
	return schema.StringAttribute{Required: true, MarkdownDescription: description, Validators: validators, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}}
}

// Metadata identifies the RLS policy attachment resource to Terraform.
func (r *rlsPolicyAttachmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_rls_policy_attachment"
}

// Schema defines the attachment tuple; every attribute is part of the identity.
func (r *rlsPolicyAttachmentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Attaches one row-level security policy to one table or view for one user, role, or PUBLIC. Attaching never turns row-level security on: the policy filters rows only once `redshift_table_security` enables it for the relation.",
		Attributes: map[string]schema.Attribute{
			"id":           idAttribute(),
			"policy":       rlsPolicyAttachmentString("RLS policy to attach. Changing it replaces the attachment."),
			"database":     rlsPolicyAttachmentString("Local database containing both the policy and the relation. Changing it replaces the attachment."),
			"schema":       rlsPolicyAttachmentString("Schema of the relation. Changing it replaces the attachment."),
			"relation":     rlsPolicyAttachmentString("Table, view, late-binding view, or materialized view the policy filters; it must have every `WITH` column of the policy. Changing it replaces the attachment."),
			"grantee":      rlsPolicyAttachmentString("User or role name the policy applies to; use `public` with `grantee_type = \"PUBLIC\"`. Policies attached to superusers or `sys:secadmin` holders are ignored. Changing it replaces the attachment."),
			"grantee_type": rlsPolicyAttachmentString("`USER`, `ROLE`, or `PUBLIC`. Changing it replaces the attachment.", stringvalidator.OneOf(rlsPolicyAttachmentGranteeTypes...)),
		},
	}
}

// rlsPolicyAttachmentIdentity is the JSON identity shared by the resource, its lookup, and imports.
func (r *rlsPolicyAttachmentResource) rlsPolicyAttachmentIdentity(data rlsPolicyAttachmentModel) types.String {
	return r.identity(data.Database.ValueString(), map[string]string{
		"policy": data.Policy.ValueString(), "schema": data.Schema.ValueString(), "relation": data.Relation.ValueString(),
		"grantee": data.Grantee.ValueString(), "grantee_type": data.GranteeType.ValueString(),
	})
}

// read reports whether the attachment exists; a dropped policy, relation, recipient, or database removes it.
func (r *rlsPolicyAttachmentResource) read(ctx context.Context, data rlsPolicyAttachmentModel) (bool, error) {
	if err := r.bound(data.ID, data.Database.ValueString()); err != nil {
		return false, err
	}
	if exists, err := r.localDatabaseExists(ctx, data.Database.ValueString()); err != nil || !exists {
		return false, err
	}
	rows, err := r.selectRows(ctx, data.Database.ValueString(), readRlsPolicyAttachmentQuery(data))
	return len(rows) > 0, err
}

// Create validates the tuple before the first state write, attaches the policy, and verifies the attachment.
func (r *rlsPolicyAttachmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data rlsPolicyAttachmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	statement, err := createRlsPolicyAttachmentStatement(data)
	if err != nil {
		resp.Diagnostics.AddError("Invalid RLS policy attachment", err.Error())
		return
	}
	if err := r.exec(ctx, data.Database.ValueString(), statement); err != nil {
		resp.Diagnostics.AddError("Attach RLS policy", err.Error())
		return
	}
	data.ID = r.rlsPolicyAttachmentIdentity(data)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	found, err := r.read(ctx, data)
	if err != nil {
		resp.Diagnostics.AddError("Verify RLS policy attachment", err.Error())
	} else if !found {
		resp.Diagnostics.AddError("Verify RLS policy attachment", "The attachment is absent after ATTACH RLS POLICY.")
	}
}

// ValidateConfig reports a tuple Create would reject, during planning once the configuration is known.
func (r *rlsPolicyAttachmentResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data rlsPolicyAttachmentModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !req.Config.Raw.IsFullyKnown() {
		return
	}
	if _, err := createRlsPolicyAttachmentStatement(data); err != nil {
		resp.Diagnostics.AddError("Invalid RLS policy attachment", err.Error())
	}
}

// Read removes an attachment that no longer exists.
func (r *rlsPolicyAttachmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data rlsPolicyAttachmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, data)
	switch {
	case err != nil:
		resp.Diagnostics.AddError("Read RLS policy attachment", err.Error())
	case !found:
		resp.State.RemoveResource(ctx)
	default:
		resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	}
}

// Update verifies that the immutable attachment still exists; every input replaces it.
func (r *rlsPolicyAttachmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data rlsPolicyAttachmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, data)
	switch {
	case err != nil:
		resp.Diagnostics.AddError("Read RLS policy attachment", err.Error())
	case !found:
		resp.Diagnostics.AddError("Update RLS policy attachment", "The attachment disappeared; refresh the plan.")
	default:
		resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	}
}

// Delete detaches the policy from the relation and recipient and verifies removal; the policy, the relation,
// and its row-level security switch stay unchanged.
func (r *rlsPolicyAttachmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data rlsPolicyAttachmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, data)
	if err == nil && found {
		var statement string
		if statement, err = dropRlsPolicyAttachmentStatement(data); err == nil {
			err = r.exec(ctx, data.Database.ValueString(), statement)
		}
		if err == nil {
			found, err = r.read(ctx, data)
			if err == nil && found {
				resp.Diagnostics.AddError("Detach RLS policy", "The attachment remains after DETACH RLS POLICY.")
			}
		}
	}
	if err != nil {
		resp.Diagnostics.AddError("Detach RLS policy", err.Error())
	}
}

// ImportState restores the attachment tuple from JSON.
func (r *rlsPolicyAttachmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importIdentity(ctx, req, resp, "database", "policy", "schema", "relation", "grantee", "grantee_type")
}
