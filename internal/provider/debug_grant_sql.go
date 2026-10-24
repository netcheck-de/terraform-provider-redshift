package provider

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// debugGranteeTypes maps each grantee_type to the identity_type SHOW GRANTS reports for its DEBUG rows.
// https://docs.aws.amazon.com/redshift/latest/dg/r_SHOW_GRANTS.html
var debugGranteeTypes = map[string]string{"USER": "global_user", "ROLE": "global_group", "ACCOUNT": "admins_of_account"}

// debugGranteeTypeNames lists the grantee_type keywords in documentation order.
var debugGranteeTypeNames = []sqlclient.Keyword{"USER", "ROLE", "ACCOUNT"}

// debugGlobalIdentity matches <prefix>:<name>, the only form of global identities and Identity Center groups. Local
// users, groups, and PUBLIC have no prefix, and r_GRANT rejects DEBUG for them.
var debugGlobalIdentity = regexp.MustCompile(`^[^:\s]+:\S.*$`)

// debugGrantModel is the Terraform state for one DEBUG grant on one database.
type debugGrantModel struct {
	// ID records the warehouse binding, the database, and the grantee.
	ID types.String `tfsdk:"id"`
	// Database is the database with federated permissions that DEBUG applies to.
	Database types.String `tfsdk:"database"`
	// Grantee is the prefixed global identity, the prefixed Identity Center group, or the 12-digit account ID.
	Grantee types.String `tfsdk:"grantee"`
	// GranteeType selects USER, ROLE, or ACCOUNT, in any case.
	GranteeType types.String `tfsdk:"grantee_type"`
}

// debugRecipient validates the grantee and returns its SQL form and the SHOW GRANTS identity_type of its rows.
// https://docs.aws.amazon.com/redshift/latest/dg/r_GRANT.html#grant-debug-syntax
func debugRecipient(grantee, granteeType string) (sqlclient.Statement, string, error) {
	kind, err := sqlclient.OneOf(granteeType, debugGranteeTypeNames...)
	if err != nil {
		return sqlclient.Statement{}, "", fmt.Errorf("unsupported grantee_type: %w", err)
	}

	switch kind {
	case "ACCOUNT":
		if !datashareAccountPattern.MatchString(grantee) {
			return sqlclient.Statement{}, "", fmt.Errorf("an ACCOUNT grantee must be a 12-digit AWS account ID")
		}

		return sqlclient.Kw("ADMINS OF ACCOUNT").Lit(grantee), debugGranteeTypes["ACCOUNT"], nil
	case "ROLE":
		if !debugGlobalIdentity.MatchString(grantee) {
			return sqlclient.Statement{}, "", fmt.Errorf("a ROLE grantee must be an Identity Center group named <prefix>:<group>")
		}

		return sqlclient.Kw("ROLE").Ident(grantee), debugGranteeTypes["ROLE"], nil
	default:
		if !debugGlobalIdentity.MatchString(grantee) {
			return sqlclient.Statement{}, "", fmt.Errorf("a USER grantee must be a global identity named <prefix>:<name>, such as IAM:auditor or IAMR:audit_role; DEBUG cannot be granted to local users, groups, or PUBLIC")
		}

		return sqlclient.Ident(grantee), debugGranteeTypes["USER"], nil
	}
}

// spec validates the tuple and renders its grant through the shared grant shape, with DEBUG as the privilege.
func (data debugGrantModel) spec() (grantSpec, string, error) {
	if data.Database.ValueString() == "" {
		return grantSpec{}, "", fmt.Errorf("database must not be empty")
	}

	grantee, identityType, err := debugRecipient(data.Grantee.ValueString(), data.GranteeType.ValueString())
	if err != nil {
		return grantSpec{}, "", err
	}

	if err := data.validateRemote(); err != nil {
		return grantSpec{}, "", err
	}

	return grantSpec{object: sqlclient.Kw("ON DATABASE").Ident(data.Database.ValueString()), grantee: grantee}, identityType, nil
}

// validateRemote rejects remote ACCOUNT grants: remote ACCOUNT has no authoritative FOR selector, while SHOW ON
// silently omits its existing grants.
func (data debugGrantModel) validateRemote() error {
	if strings.Contains(data.Database.ValueString(), "@") && strings.EqualFold(data.GranteeType.ValueString(), "ACCOUNT") {
		return fmt.Errorf("cross-catalog ACCOUNT DEBUG grants have no reliable readback; configure a provider connected to the producer warehouse and use its local database name without @catalog")
	}

	return nil
}

// readDebugGrantRemoteStatement uses the live-verified cross-catalog shape; SHOW ON can silently return no DEBUG rows.
func readDebugGrantRemoteStatement(data debugGrantModel) (string, error) {
	spec, _, err := data.spec()
	if err != nil {
		return "", err
	}

	return sqlclient.Stmt("SHOW GRANTS FOR").Append(spec.grantee).KwIdent("FROM DATABASE", data.Database.ValueString()).String(), nil
}

// readDebugGrantAuthStatement reads the current session's authentication method. Current-user/latest filtering excludes
// incidental rdsdb authentication records for the same backend PID. The exact IAM AssumeUser result is live-verified;
// username shapes and SHOW GLOBAL IDENTITY are insufficient.
func readDebugGrantAuthStatement() string {
	return sqlclient.Stmt("SELECT").Kw("TRIM(auth_method) AS auth_method").Kw("FROM sys_connection_log").
		Kw("WHERE session_id = pg_backend_pid()").Kw("AND user_name = current_user").
		Kw("AND event =").Lit("authenticated").Kw("ORDER BY record_time DESC LIMIT").Int(1).String()
}

// debugGrantRemoteMatches normalizes only the scoped row types emitted by SHOW FOR, not arbitrary local identities.
func debugGrantRemoteMatches(row sqlclient.Row, data debugGrantModel) bool {
	kind := strings.ToUpper(data.GranteeType.ValueString())
	if kind != "USER" && kind != "ROLE" {
		return false
	}

	return row["database_name"] == data.Database.ValueString() && row["object_type"] == "DATABASE" && row["privilege_scope"] == "DATABASE" &&
		strings.EqualFold(row["identity_type"], data.GranteeType.ValueString()) && debugGrantMatches(row, data.Grantee.ValueString(), row["identity_type"])
}

// createDebugGrantStatement grants DEBUG on the database. DEBUG takes no grant option and is not part of ALL, so the
// statement names it alone.
func createDebugGrantStatement(data debugGrantModel) (string, error) {
	spec, _, err := data.spec()
	if err != nil {
		return "", err
	}

	return spec.statement(true, "DEBUG"), nil
}

// dropDebugGrantStatement revokes only this grantee's DEBUG on the database.
// https://docs.aws.amazon.com/redshift/latest/dg/r_REVOKE.html
func dropDebugGrantStatement(data debugGrantModel) (string, error) {
	spec, _, err := data.spec()
	if err != nil {
		return "", err
	}

	return spec.statement(false, "DEBUG"), nil
}

// readDebugGrantStatement lists the grants on the database, which for a warehouse with federated permissions
// include DEBUG rows; no catalog view documents them.
func readDebugGrantStatement(database string) string {
	return sqlclient.Stmt("SHOW GRANTS ON DATABASE").Ident(database).String()
}

// debugGrantMatches reports whether a SHOW GRANTS row is this tuple's DEBUG grant. Identity names compare without
// case, because global identity names keep their provider prefix in upper case while the rest may be folded.
func debugGrantMatches(row sqlclient.Row, grantee, identityType string) bool {
	return strings.EqualFold(row["privilege_type"], "DEBUG") && strings.EqualFold(row["identity_type"], identityType) &&
		strings.EqualFold(row["identity_name"], grantee)
}

// debugGrantCanonical records grantee_type in upper case, so identities agree however configuration spells it.
func debugGrantCanonical(fields map[string]string) {
	if value, ok := fields["grantee_type"]; ok {
		fields["grantee_type"] = strings.ToUpper(value)
	}
}
