package provider

import (
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
)

// debugGrantFixture builds a DEBUG grant on database for one grantee.
func debugGrantFixture(database, grantee, granteeType string) debugGrantModel {
	return debugGrantModel{ID: types.StringNull(), Database: types.StringValue(database), Grantee: types.StringValue(grantee), GranteeType: types.StringValue(granteeType)}
}

// TestDebugGrantSQL pins SHOW GRANTS ON DATABASE, GRANT DEBUG, and REVOKE DEBUG for each grantee form of r_GRANT's
// DEBUG syntax, and the grantees Redshift rejects.
func TestDebugGrantSQL(t *testing.T) {
	render := func(data debugGrantModel) func() ([]string, error) {
		return func() ([]string, error) {
			create, err := createDebugGrantStatement(data)
			if err != nil {
				return nil, err
			}
			drop, err := dropDebugGrantStatement(data)
			if err != nil {
				return nil, err
			}
			read := readDebugGrantStatement(data.Database.ValueString())
			if strings.Contains(data.Database.ValueString(), "@") {
				read, err = readDebugGrantRemoteStatement(data)
				if err != nil {
					return nil, err
				}
			}
			return []string{read, create, drop}, nil
		}
	}
	checkSQL(t, "debug_grant", []sqlCase{
		{"iam_user", render(debugGrantFixture(debugGrantTestDatabase, "IAM:sales_auditor", "USER"))},
		{"iam_role", render(debugGrantFixture(debugGrantTestDatabase, "IAMR:audit_role", "USER"))},
		{"idc_group", render(debugGrantFixture(debugGrantTestDatabase, "myidc:audit_team", "ROLE"))},
		{"account_admins", render(debugGrantFixture("sales_db", "123456789012", "ACCOUNT"))},
		{"remote_account_rejected", render(debugGrantFixture(debugGrantTestDatabase, "123456789012", "ACCOUNT"))},
		{"local_database_lowercase_type", render(debugGrantFixture("sales_db", "IAM:sales_auditor", "user"))},
		{"quoted_identifiers", render(debugGrantFixture(`Odd"Db@catalog`, `myidc:Audit "Team"`, "ROLE"))},
		{"local_user_rejected", render(debugGrantFixture(debugGrantTestDatabase, "alice", "USER"))},
		{"public_rejected", render(debugGrantFixture(debugGrantTestDatabase, "PUBLIC", "USER"))},
		{"local_role_rejected", render(debugGrantFixture(debugGrantTestDatabase, "auditors", "ROLE"))},
		{"short_account_rejected", render(debugGrantFixture(debugGrantTestDatabase, "12345", "ACCOUNT"))},
		{"group_type_rejected", render(debugGrantFixture(debugGrantTestDatabase, "IAM:sales_auditor", "GROUP"))},
		{"empty_database_rejected", render(debugGrantFixture("", "IAM:sales_auditor", "USER"))},
	})
}

// TestDebugGrantMatches keeps only the tuple's DEBUG row, comparing identity names without case.
func TestDebugGrantMatches(t *testing.T) {
	row := sqlclient.Row{"privilege_type": "DEBUG", "identity_name": "IAM:sales_auditor", "identity_type": "global_user"}
	assert.True(t, debugGrantMatches(row, "IAM:sales_auditor", "global_user"))
	assert.True(t, debugGrantMatches(row, "iam:Sales_Auditor", "global_user"))
	assert.False(t, debugGrantMatches(row, "IAM:sales_auditor", "global_group"))
	assert.False(t, debugGrantMatches(row, "IAM:other", "global_user"))
	row["privilege_type"] = "TEMP"
	assert.False(t, debugGrantMatches(row, "IAM:sales_auditor", "global_user"))
}
