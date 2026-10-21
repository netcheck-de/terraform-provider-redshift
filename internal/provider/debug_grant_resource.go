package provider

import (
	"context"
	"fmt"
	"strings"

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
			"id":       idAttribute(),
			"database": privilegeString("Database with federated permissions that `DEBUG` applies to. `ACCOUNT` requires a producer-local name such as `sales_db` and a provider connected to that producer. Remote `USER`/`ROLE` aliases such as `sales_db@finance-catalog` require a SQL superuser, a verified `IAM AssumeUser` current-session authentication event in `SYS_CONNECTION_LOG`, and alias visibility in `SHOW DATABASES`. Password, unavailable, or unverified remote contexts are errors even if the alias is listed. Changing it replaces the grant.", false),
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

// remoteVisible proves both session authentication and alias visibility before any absence inference. Password
// sessions can list the alias but silently omit existing remote grants, so listing alone is not evidence of access.
func (r *debugGrantResource) remoteVisible(ctx context.Context, database string) error {
	rows, err := r.query(ctx, readDebugGrantAuthStatement(), nil)
	if err != nil {
		return fmt.Errorf("cannot verify remote DEBUG authentication context through SYS_CONNECTION_LOG (requires a SQL superuser); use verified IAM authentication or a producer-local provider: %w", err)
	}
	method := "unknown"
	if len(rows) == 1 && rows[0]["auth_method"] != "" {
		method = rows[0]["auth_method"]
	}
	if method != "IAM AssumeUser" {
		return fmt.Errorf("remote DEBUG requires a verified IAM AssumeUser authentication context, got %q; password, unavailable, or unverified sessions cannot safely read remote grants. Use supported IAM authentication or a producer-local provider", method)
	}
	listed, err := r.listed(ctx, database)
	if err != nil {
		return fmt.Errorf("cannot verify remote DEBUG database visibility: %w", err)
	}
	if !listed {
		return fmt.Errorf("remote DEBUG database %q is not visible to the provider's SQL identity in SHOW DATABASES; use an identity with catalog visibility or a producer-local provider. Refusing to infer grant absence", database)
	}
	return nil
}

// read uses local global-identity rows or remote scoped FOR rows, never empty remote ON output as absence evidence.
func (r *debugGrantResource) read(ctx context.Context, data debugGrantModel) (bool, error) {
	if err := r.bound(data.ID, data.Database.ValueString()); err != nil {
		return false, err
	}
	_, identityType, err := data.spec()
	if err != nil {
		return false, err
	}
	database := data.Database.ValueString()
	if strings.Contains(database, "@") {
		if err := r.remoteVisible(ctx, database); err != nil {
			return false, err
		}
		statement, err := readDebugGrantRemoteStatement(data)
		if err != nil {
			return false, err
		}
		rows, err := r.query(ctx, statement, nil)
		if err != nil {
			return false, err
		}
		for _, row := range rows {
			if debugGrantRemoteMatches(row, data) {
				return true, nil
			}
		}
		return false, nil
	}
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

// ValidateConfig checks known grantees independently of generated database names.
func (r *debugGrantResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data debugGrantModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !data.Database.IsUnknown() && data.Database.ValueString() == "" {
		resp.Diagnostics.AddError("Invalid debug grant", "database must not be empty")
	}
	if err := data.validateRemote(); err != nil {
		resp.Diagnostics.AddError("Invalid debug grant", err.Error())
	}
	if data.Grantee.IsUnknown() || data.GranteeType.IsUnknown() {
		return
	}
	if _, _, err := debugRecipient(data.Grantee.ValueString(), data.GranteeType.ValueString()); err != nil {
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
	if strings.Contains(data.Database.ValueString(), "@") {
		// Prove both alias visibility and the scoped read's permission before granting access we must verify.
		preflight := data
		preflight.ID = types.StringNull()
		if _, err := r.read(ctx, preflight); err != nil {
			resp.Diagnostics.AddError("Invalid debug grant", err.Error())
			return
		}
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
