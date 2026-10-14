package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// debugGrantResource manages DEBUG on one database with federated permissions for one grantee.
type debugGrantResource struct {
	// resourceClient provides SQL execution and warehouse ownership checks.
	resourceClient
}

var _ = registerResource(newDebugGrantResource)

// newDebugGrantResource constructs a DEBUG grant handler.
func newDebugGrantResource() resource.Resource { return &debugGrantResource{} }

// Metadata identifies the DEBUG grant resource to Terraform.
func (r *debugGrantResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_debug_grant"
}

// Schema defines the database and the one grantee; every change replaces the grant.
func (r *debugGrantResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Grants `DEBUG` on one database in a warehouse with federated permissions, so the grantee sees secure logging records unredacted, to a global identity, an Identity Center group, or the administrators of an account.",
		Attributes: map[string]schema.Attribute{
			"id": idAttribute(),
			"database": privilegeString("Database with federated permissions that `DEBUG` applies to, such as `sales_db` on the warehouse that owns it or "+
				"`sales_db@finance-catalog` from another warehouse. Changing it replaces the grant.", false),
			"grantee": privilegeString("Receiving identity: a global identity `<prefix>:<name>` (such as `IAM:sales_auditor` or `IAMR:audit_role`) for `USER`, "+
				"an Identity Center group `<prefix>:<group>` for `ROLE`, or a 12-digit AWS account ID for `ACCOUNT`, which must own the producer. Changing it replaces the grant.", false),
			"grantee_type": privilegeKeyword("`USER` for a global identity (`TO \"<prefix>:<name>\"`), `ROLE` for an Identity Center group (`TO ROLE \"<prefix>:<group>\"`), "+
				"or `ACCOUNT` for the superusers of an account (`TO ADMINS OF ACCOUNT '<account_id>'`). Accepted in any case; another case of the same keyword is recorded in place. "+
				"Changing it replaces the grant.", "USER", "ROLE", "ACCOUNT"),
		},
	}
}

// listed reports whether SHOW DATABASES still lists the database.
func (r *debugGrantResource) listed(ctx context.Context, database string) (bool, error) {
	rows, err := r.query(ctx, readDatabaseStatement(database), nil)
	if err != nil {
		return false, err
	}
	for _, row := range rows {
		if row["database_name"] == database {
			return true, nil
		}
	}
	return false, nil
}

// read checks the DEBUG row of the tuple. SHOW GRANTS fails for a dropped database, so a failure counts as a
// missing parent only when SHOW DATABASES no longer lists the database; any other failure stays an error. Checking
// the listing only on failure keeps cross-warehouse names, which the listing may not report, readable.
func (r *debugGrantResource) read(ctx context.Context, data debugGrantModel) (bool, error) {
	if err := r.bound(data.ID, data.Database.ValueString()); err != nil {
		return false, err
	}
	_, identityType, err := data.spec()
	if err != nil {
		return false, err
	}
	database := data.Database.ValueString()
	rows, err := r.query(ctx, readDebugGrantStatement(database), nil)
	if err != nil {
		if listed, listErr := r.listed(ctx, database); listErr == nil && !listed {
			return false, nil
		}
		return false, err
	}
	for _, row := range rows {
		if debugGrantMatches(row, data.Grantee.ValueString(), identityType) {
			return true, nil
		}
	}
	return false, nil
}

// fields returns the JSON identity keys besides the binding and the database.
func (data debugGrantModel) fields() map[string]string {
	fields := map[string]string{"grantee": data.Grantee.ValueString(), "grantee_type": data.GranteeType.ValueString()}
	debugGrantCanonical(fields)
	return fields
}

// ValidateConfig reports invalid grantees during planning, once the configuration is known.
func (r *debugGrantResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data debugGrantModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !req.Config.Raw.IsFullyKnown() {
		return
	}
	if _, _, err := data.spec(); err != nil {
		resp.Diagnostics.AddError("Invalid debug grant", err.Error())
	}
}

// Create grants DEBUG and verifies that SHOW GRANTS reports it.
func (r *debugGrantResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data debugGrantModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	statement, err := createDebugGrantStatement(data)
	if err != nil {
		resp.Diagnostics.AddError("Invalid debug grant", err.Error())
		return
	}
	// The statement names the database, so it runs in the administration database, which also reaches
	// database@catalog names that cannot be connected to.
	if err := r.exec(ctx, r.database.ValueString(), statement); err != nil {
		resp.Diagnostics.AddError("Grant Redshift DEBUG", err.Error())
		return
	}
	data.ID = r.identity(data.Database.ValueString(), data.fields())
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	found, err := r.read(ctx, data)
	if err != nil {
		resp.Diagnostics.AddError("Verify Redshift DEBUG grant", err.Error())
	} else if !found {
		resp.Diagnostics.AddError("Verify Redshift DEBUG grant", "SHOW GRANTS does not report the DEBUG grant after granting it.")
	}
}

// Read refreshes the grant and removes it from state when it or its database is gone.
func (r *debugGrantResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data debugGrantModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, data)
	switch {
	case err != nil:
		resp.Diagnostics.AddError("Read Redshift DEBUG grant", err.Error())
	case !found:
		resp.State.RemoveResource(ctx)
	default:
		resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	}
}

// Update records another spelling of grantee_type; every other change replaces the grant, so the grant must exist.
func (r *debugGrantResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data debugGrantModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, data)
	switch {
	case err != nil:
		resp.Diagnostics.AddError("Read Redshift DEBUG grant", err.Error())
	case !found:
		resp.Diagnostics.AddError("Update Redshift DEBUG grant", "The DEBUG grant disappeared; refresh the plan.")
	default:
		resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	}
}

// Delete revokes only this grantee's DEBUG and verifies removal.
func (r *debugGrantResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data debugGrantModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, data)
	if err == nil && found {
		var statement string
		statement, err = dropDebugGrantStatement(data)
		if err == nil {
			err = r.exec(ctx, r.database.ValueString(), statement)
		}
		if err == nil {
			found, err = r.read(ctx, data)
			if err == nil && found {
				resp.Diagnostics.AddError("Revoke Redshift DEBUG", "SHOW GRANTS still reports the DEBUG grant after revoking it.")
			}
		}
	}
	if err != nil {
		resp.Diagnostics.AddError("Revoke Redshift DEBUG", err.Error())
	}
}

// ImportState restores the database and grantee; the next read validates them and checks the grant.
func (r *debugGrantResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importIdentity(ctx, req, resp, "database", "grantee", "grantee_type")
}

// debugGrantLookupModel converts a lookup configuration into the resource model the read shares.
func debugGrantLookupModel(data types.Object) debugGrantModel {
	attributes := data.Attributes()
	return debugGrantModel{
		ID: types.StringNull(), Database: attributes["database"].(types.String),
		Grantee: attributes["grantee"].(types.String), GranteeType: attributes["grantee_type"].(types.String),
	}
}
