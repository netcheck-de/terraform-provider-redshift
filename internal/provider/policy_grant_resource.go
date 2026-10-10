package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// policyGrantResource manages SELECT on a lookup table for a row-level security or masking policy. It reuses the
// privilege contract for schema, validation, and GRANT/REVOKE rendering, but not its catalog reads: the AWS reference
// documents no catalog that lists grants to policies, because SHOW GRANTS and SVV_RELATION_PRIVILEGES name only
// users, roles, groups, and PUBLIC. State is therefore the record of what Terraform granted, and refresh checks only
// that the database, schema, table, and policy still exist.
// https://docs.aws.amazon.com/redshift/latest/dg/r_SHOW_GRANTS.html
// https://docs.aws.amazon.com/redshift/latest/dg/r_SVV_RELATION_PRIVILEGES.html
type policyGrantResource struct {
	*privilegeResource
}

var _ = registerResource(newPolicyGrantResource)

// newPolicyGrantResource defines SELECT on a lookup table for a row-level security or masking policy.
func newPolicyGrantResource() resource.Resource {
	attributes := privilegeAttributes()
	attributes["privileges"] = schema.SetAttribute{Required: true, ElementType: types.StringType, MarkdownDescription: "Privileges Terraform grants: `[\"SELECT\"]`, the only permission a policy can hold, or empty to revoke it. Redshift does not report grants to policies, so this records what Terraform applied and does not detect changes made outside Terraform."}
	attributes["database_name"] = privilegeString("Local database that holds the lookup table and the policy. Changing it replaces the grant.", false)
	attributes["schema_name"] = privilegeString("Schema of the lookup table. Changing it replaces the grant.", false)
	attributes["object_name"] = privilegeString("Lookup table that the policy's expression reads. Changing it replaces the grant.", false)
	attributes["policy_type"] = privilegeString("`RLS` for a row-level security policy or `MASKING` for a dynamic data masking policy. Changing it replaces the grant.", false, "RLS", "MASKING")
	attributes["policy_name"] = privilegeString("Policy that receives the permission. Changing it replaces the grant.", false)
	return &policyGrantResource{privilegeResource: &privilegeResource{
		name: "policy_grant", attributes: attributes,
		fields:  []string{"database_name", "schema_name", "object_name", "policy_type", "policy_name"},
		prepare: policyGrantTarget, recipient: policyGrantRecipient,
	}}
}

// Schema describes the grant as applied by Terraform rather than read back from the catalog.
func (r *policyGrantResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	r.privilegeResource.Schema(ctx, req, resp)
	resp.Schema.MarkdownDescription = "Grants `SELECT` on one lookup table to one row-level security or masking policy. Redshift does not report grants to policies, so the provider tracks the grant in state and refresh only checks that the table and policy still exist."
}

// parents checks that the tuple's database, schema, table, and policy exist, the only catalog facts the grant can be
// tied to.
func (r *policyGrantResource) parents(ctx context.Context, data types.Object) (privilegeTarget, bool, error) {
	if err := r.bound(data.Attributes()["id"].(types.String), r.database.ValueString()); err != nil {
		return privilegeTarget{}, false, err
	}
	target, err := r.target(data)
	if err != nil {
		return target, false, err
	}
	for _, check := range target.checks {
		rows, err := r.queryDatabase(ctx, r.database.ValueString(), check.sql, check.parameters)
		if err != nil || len(rows) == 0 {
			return target, false, err
		}
	}
	return target, true, nil
}

// apply moves the grant from the privileges Terraform recorded to the desired ones. The recorded set stands in for
// the catalog, so a grant changed outside Terraform is repaired only when the configuration next changes it.
func (r *policyGrantResource) apply(ctx context.Context, data types.Object, recorded []string) error {
	if err := r.validate(data); err != nil {
		return err
	}
	target, found, err := r.parents(ctx, data)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("the database, schema, lookup table, or policy does not exist")
	}
	return r.revise(ctx, target, recorded, knownStrings(data.Attributes()["privileges"].(types.Set)))
}

// revise runs the GRANT and REVOKE statements that turn the recorded privileges into the desired ones.
func (r *policyGrantResource) revise(ctx context.Context, target privilegeTarget, recorded, desired []string) error {
	statements, err := privilegeStatements(target.grant, target.allowed, recorded, desired)
	if err != nil {
		return err
	}
	return r.exec(ctx, r.targetDatabase(target), statements...)
}

// Create records the grant before applying it, so a failed GRANT leaves state that Delete can revoke.
func (r *policyGrantResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data types.Object
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Invalid tuples must fail before ownership is recorded, or Read and Delete could never succeed.
	if err := r.validate(data); err != nil {
		resp.Diagnostics.AddError("Create policy_grant", err.Error())
		return
	}
	fields := map[string]string{}
	for _, field := range r.fields {
		fields[field] = objectString(data, field)
	}
	attributes := data.Attributes()
	attributes["id"] = r.identity(r.database.ValueString(), fields)
	data = types.ObjectValueMust(data.AttributeTypes(ctx), attributes)
	resp.Diagnostics.Append(resp.State.Set(ctx, data)...)
	// Nothing is recorded as granted yet, so the configured privileges are granted even if the policy holds them.
	if err := r.apply(ctx, data, nil); err != nil {
		resp.Diagnostics.AddError("Create policy_grant", err.Error())
	}
}

// Read keeps the recorded privileges and removes the grant when its database, schema, table, or policy is gone.
func (r *policyGrantResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data types.Object
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	_, found, err := r.parents(ctx, data)
	if err != nil {
		resp.Diagnostics.AddError("Read policy_grant", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	// An import cannot learn the grant, so it records none and the next apply grants the configured privileges.
	if data.Attributes()["privileges"].IsNull() {
		data = withPrivileges(data, nil)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, data)...)
}

// Update grants and revokes the difference between the recorded and the planned privileges.
func (r *policyGrantResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, prior types.Object
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.apply(ctx, plan, knownStrings(prior.Attributes()["privileges"].(types.Set))); err != nil {
		resp.Diagnostics.AddError("Update policy_grant", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// Delete revokes the recorded privileges; a grant whose table or policy is gone has nothing left to revoke.
func (r *policyGrantResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data types.Object
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	target, found, err := r.parents(ctx, data)
	if err == nil && found {
		err = r.revise(ctx, target, knownStrings(data.Attributes()["privileges"].(types.Set)), nil)
	}
	if err != nil {
		resp.Diagnostics.AddError("Delete policy_grant", err.Error())
	}
}
