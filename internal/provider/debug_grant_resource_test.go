package provider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// debugGrantSetup lets SHOW DATABASES list the test database, which the read consults when SHOW GRANTS fails, and
// grants the representative identity on the local database the standard transcripts route through too.
func debugGrantSetup(c *catalog) {
	c.databaseName = debugGrantTestDatabase
	fakeState[*debugGrantFakeFamily](c, "debug_grant").grant("warehouse", debugGrantKey{identityType: "global_user", name: "IAM:sales_auditor"}, true)
}

var _ = registerLifecycleCase(lifecycleCase{
	name: "debug grant", kind: lifecyclePermission, new: newDebugGrantResource,
	model: debugGrantFixture("warehouse", "IAM:sales_auditor", "USER"),
	setup: func(c *catalog) { debugGrantSetup(c); c.databaseName = "warehouse" },
	absent: func(c *catalog) {
		fakeState[*debugGrantFakeFamily](c, "debug_grant").clear()
	},
})

var _ = registerReplacementPolicy("redshift_debug_grant", map[string]replaceRule{
	"database":     replaceAlways,
	"grantee":      replaceAlways,
	"grantee_type": replaceAlways,
})

var _ = registerValidateConfigCase("debug_grant", validateConfigCase{
	new:     newDebugGrantResource,
	valid:   debugGrantFixture(debugGrantTestDatabase, "myidc:audit_team", "role"),
	invalid: debugGrantFixture(debugGrantTestDatabase, "auditor", "USER"),
	unknown: debugGrantModel{ID: types.StringNull(), Database: types.StringValue(debugGrantTestDatabase), Grantee: types.StringUnknown(), GranteeType: types.StringValue("USER")},
})

// debugGrantCatalog returns a full fake catalog after the debug grant setup.
func debugGrantCatalog() *catalog {
	c := fullCatalog()
	debugGrantSetup(c)
	return c
}

// TestDebugGrantRecipients grants, reads, and revokes each grantee form, which map to different identity types.
func TestDebugGrantRecipients(t *testing.T) {
	for _, data := range []debugGrantModel{
		debugGrantFixture(debugGrantTestDatabase, "IAMR:audit_role", "USER"),
		debugGrantFixture(debugGrantTestDatabase, "myidc:audit_team", "role"),
		debugGrantFixture("warehouse", "123456789012", "ACCOUNT"),
	} {
		t.Run(data.GranteeType.ValueString(), func(t *testing.T) {
			c := debugGrantCatalog()
			r := newDebugGrantResource()
			configureTestResource(t, r, c)
			state, diagnostics := applyOperation(t, r, "create", nil, data, nil)
			require.False(t, diagnostics.HasError(), "%v", diagnostics)
			var id types.String
			require.False(t, state.GetAttribute(context.Background(), path.Root("id"), &id).HasError())
			assertLookupIdentity(t, id, data.Database.ValueString(), map[string]string{"grantee": data.Grantee.ValueString(), "grantee_type": strings.ToUpper(data.GranteeType.ValueString())})
			data.ID = id
			_, diagnostics = applyOperation(t, r, "read", data, nil, nil)
			require.False(t, diagnostics.HasError(), "%v", diagnostics)
			_, diagnostics = applyOperation(t, r, "delete", data, nil, nil)
			require.False(t, diagnostics.HasError(), "%v", diagnostics)
			assert.Len(t, fakeState[*debugGrantFakeFamily](c, "debug_grant").grants[data.Database.ValueString()], 1, "only the representative grant remains")
		})
	}
}

// TestDebugGrantTypeMustMatchIdentity treats a DEBUG row of another identity type as a different grant.
func TestDebugGrantTypeMustMatchIdentity(t *testing.T) {
	c := debugGrantCatalog()
	r := &debugGrantResource{testResourceClient(c)}
	found, err := r.read(context.Background(), debugGrantFixture(debugGrantTestDatabase, "IAM:sales_auditor", "ROLE"))
	require.NoError(t, err)
	assert.False(t, found)
	found, err = r.read(context.Background(), debugGrantFixture(debugGrantTestDatabase, "iam:SALES_AUDITOR", "user"))
	require.NoError(t, err)
	assert.True(t, found)
}

// TestDebugGrantReadFailures removes the grant when SHOW GRANTS fails because the database is gone, and keeps every
// other failure an error.
func TestDebugGrantReadFailures(t *testing.T) {
	for _, test := range []struct {
		name    string
		listed  bool
		listErr bool
		removed bool
	}{
		{"database dropped", false, false, true},
		{"database still listed", true, false, false},
		{"listing unavailable", false, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var statements []string
			client := queryFunc(func(_ context.Context, target sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
				statements = append(statements, target.Database+": "+sql)
				switch {
				case strings.HasPrefix(sql, "SHOW GRANTS"):
					return nil, errors.New("database does not exist")
				case test.listErr:
					return nil, errors.New("listing unavailable")
				case test.listed:
					// LIKE matches _ as a wildcard, so only the exact name counts.
					return []sqlclient.Row{{"database_name": "salesXdb"}, {"database_name": "sales_db"}}, nil
				}
				return []sqlclient.Row{{"database_name": "salesXdb"}}, nil
			})
			r := &debugGrantResource{testResourceClient(client)}
			found, err := r.read(context.Background(), debugGrantFixture("sales_db", "IAM:sales_auditor", "USER"))
			assert.False(t, found)
			if test.removed {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, "database does not exist")
			}
			assert.Equal(t, []string{
				`admin: SHOW GRANTS ON DATABASE "sales_db"`,
				`admin: SHOW DATABASES LIKE 'sales_db'`,
			}, statements)
		})
	}
}

// TestDebugGrantRejectsInvalidGranteeBeforeSQL fails creation and reads of local identities without running SQL.
func TestDebugGrantRejectsInvalidGranteeBeforeSQL(t *testing.T) {
	c := debugGrantCatalog()
	r := newDebugGrantResource()
	configureTestResource(t, r, c)
	invalid := debugGrantFixture(debugGrantTestDatabase, "alice", "USER")
	_, diagnostics := applyOperation(t, r, "create", nil, invalid, nil)
	require.True(t, diagnostics.HasError())
	assert.Equal(t, "Invalid debug grant", diagnostics.Errors()[0].Summary())
	_, diagnostics = applyOperation(t, r, "read", invalid, nil, nil)
	require.True(t, diagnostics.HasError())
	assert.Empty(t, c.writes)
}

// TestDebugGrantUpdateRecordsKeywordCase keeps the grant when only the case of grantee_type changes.
func TestDebugGrantUpdateRecordsKeywordCase(t *testing.T) {
	c := debugGrantCatalog()
	r := newDebugGrantResource()
	configureTestResource(t, r, c)
	prior := debugGrantFixture(debugGrantTestDatabase, "IAM:sales_auditor", "USER")
	planned := debugGrantFixture(debugGrantTestDatabase, "IAM:sales_auditor", "user")
	state, diagnostics := applyOperation(t, r, "update", prior, planned, nil)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	var granteeType types.String
	require.False(t, state.GetAttribute(context.Background(), path.Root("grantee_type"), &granteeType).HasError())
	assert.Equal(t, types.StringValue("user"), granteeType)
	assert.Empty(t, c.writes)
}

// TestDebugGrantTranscripts records the Identity Center group and account forms, and drift of the grant and of the
// database, which the representative global identity case does not reach.
func TestDebugGrantTranscripts(t *testing.T) {
	group := debugGrantFixture(debugGrantTestDatabase, "myidc:audit_team", "ROLE")
	account := debugGrantFixture("warehouse", "123456789012", "ACCOUNT")
	granted := func(data debugGrantModel, identityType string) func(*catalog) {
		return func(c *catalog) {
			fakeState[*debugGrantFakeFamily](c, "debug_grant").grant(data.Database.ValueString(), debugGrantKey{identityType: identityType, name: data.Grantee.ValueString()}, true)
		}
	}
	runTranscripts(t, "lifecycle/debug_grant_recipients", newDebugGrantResource, []transcriptCase{
		{name: "create_group", operation: "create", catalog: catalogWith(debugGrantSetup), planned: group},
		{name: "delete_group", operation: "delete", catalog: catalogWith(debugGrantSetup, granted(group, "global_group")), prior: group},
		{name: "import_group", operation: "import", catalog: catalogWith(debugGrantSetup), planned: group},
		{name: "create_account", operation: "create", catalog: catalogWith(debugGrantSetup), planned: account},
		{name: "read_account", operation: "read", catalog: catalogWith(debugGrantSetup, granted(account, "admins_of_account")), prior: account},
		{name: "delete_account", operation: "delete", catalog: catalogWith(debugGrantSetup, granted(account, "admins_of_account")), prior: account},
	})
}

// TestDebugGrantDrift removes a revoked grant from state, so the next plan grants it again.
func TestDebugGrantDrift(t *testing.T) {
	c := debugGrantCatalog()
	fakeState[*debugGrantFakeFamily](c, "debug_grant").clear()
	r := newDebugGrantResource()
	configureTestResource(t, r, c)
	state, diagnostics := applyOperation(t, r, "read", debugGrantFixture(debugGrantTestDatabase, "IAM:sales_auditor", "USER"), nil, nil)
	require.True(t, diagnostics.HasError(), "applyOperation reports the removal")
	assert.True(t, state.Raw.IsNull())
}
