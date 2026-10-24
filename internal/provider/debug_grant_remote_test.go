package provider

import (
	"context"
	"errors"
	"maps"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDebugGrantRemoteScopedLifecycle reads remote DEBUG grants through SHOW GRANTS FOR, because SHOW GRANTS ON misses
// them, so Read keeps and Delete revokes the grant.
func TestDebugGrantRemoteScopedLifecycle(t *testing.T) {
	for _, kind := range []string{"USER", "ROLE"} {
		t.Run(kind, func(t *testing.T) {
			principal := "IAM:auditor"
			if kind == "ROLE" {
				principal = "idc:audit_team"
			}

			data := debugGrantFixture("sales@catalog", principal, kind)
			r := newDebugGrantResource().(*debugGrantResource)
			held := false

			var statements []string

			r.resourceClient = testResourceClient(queryFunc(func(_ context.Context, connection sqlclient.Connection, statement string, _ map[string]string) ([]sqlclient.Row, error) {
				assert.Equal(t, "admin", connection.Database)

				statements = append(statements, statement)
				switch {
				case statement == readDebugGrantAuthStatement():
					return []sqlclient.Row{{"auth_method": "IAM AssumeUser"}}, nil
				case strings.HasPrefix(statement, "SHOW DATABASES"):
					return []sqlclient.Row{{"database_name": "sales@catalog"}}, nil
				case strings.HasPrefix(statement, "SHOW GRANTS ON"):
					return nil, nil // The old read silently missed a real remote grant.
				case strings.HasPrefix(statement, "SHOW GRANTS FOR"):
					if held {
						return []sqlclient.Row{{"database_name": "sales@catalog", "object_type": "DATABASE", "privilege_scope": "DATABASE", "privilege_type": "DEBUG", "identity_name": principal, "identity_type": strings.ToLower(kind)}}, nil
					}

					return nil, nil
				case strings.HasPrefix(statement, "GRANT DEBUG"):
					held = true
				case strings.HasPrefix(statement, "REVOKE DEBUG"):
					held = false
				}

				return nil, nil
			}))
			state, diagnostics := applyOperation(t, r, "create", nil, data, nil)
			require.False(t, diagnostics.HasError(), "%v", diagnostics)
			require.False(t, state.Get(context.Background(), &data).HasError())
			state, diagnostics = applyOperation(t, r, "read", data, nil, nil)
			require.False(t, diagnostics.HasError(), "%v", diagnostics)
			assert.False(t, state.Raw.IsNull(), "empty remote SHOW ON is not used as evidence of absence")
			_, diagnostics = applyOperation(t, r, "delete", data, nil, nil)
			require.False(t, diagnostics.HasError(), "%v", diagnostics)
			assert.False(t, held, "Delete must revoke the grant discovered through SHOW FOR")

			revoke, err := dropDebugGrantStatement(data)
			require.NoError(t, err)
			assert.Contains(t, statements, revoke)

			for _, statement := range statements {
				assert.NotContains(t, statement, "SHOW GRANTS ON")
			}
		})
	}
}

// TestDebugGrantRemoteInvisiblePreservesStateAndAvoidsMutation fails every operation on an alias missing from SHOW
// DATABASES, keeping state and issuing no GRANT, REVOKE, or grant read.
func TestDebugGrantRemoteInvisiblePreservesStateAndAvoidsMutation(t *testing.T) {
	r := newDebugGrantResource().(*debugGrantResource)

	var statements []string

	r.resourceClient = testResourceClient(queryFunc(func(_ context.Context, _ sqlclient.Connection, statement string, _ map[string]string) ([]sqlclient.Row, error) {
		statements = append(statements, statement)
		if statement == readDebugGrantAuthStatement() {
			return []sqlclient.Row{{"auth_method": "IAM AssumeUser"}}, nil
		}

		return nil, nil
	}))

	data := debugGrantFixture("sales@catalog", "IAM:auditor", "USER")
	for _, operation := range []string{"create", "read", "delete"} {
		state, diagnostics := applyOperation(t, r, operation, data, data, nil)
		require.True(t, diagnostics.HasError(), "%s", operation)
		assert.Contains(t, diagnostics.Errors()[0].Detail(), "not visible")

		if operation == "read" {
			assert.False(t, state.Raw.IsNull())
		}
	}

	for _, statement := range statements {
		assert.True(t, statement == readDebugGrantAuthStatement() || strings.HasPrefix(statement, "SHOW DATABASES"), "invisible aliases must not lead to GRANT, REVOKE, or an untrustworthy empty grant read: %s", statement)
	}
}

// TestDebugGrantRemoteAccountRefused rejects remote ACCOUNT grantees, which belong on the producer warehouse, before
// any SQL while keeping existing state.
func TestDebugGrantRemoteAccountRefused(t *testing.T) {
	r := newDebugGrantResource().(*debugGrantResource)
	calls := 0
	r.resourceClient = testResourceClient(queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
		calls++
		return nil, nil
	}))

	data := debugGrantFixture("sales@catalog", "123456789012", "ACCOUNT")
	for _, operation := range []string{"create", "read", "delete"} {
		state, diagnostics := applyOperation(t, r, operation, data, data, nil)
		require.True(t, diagnostics.HasError(), "%s", operation)
		assert.Contains(t, diagnostics.Errors()[0].Detail(), "producer warehouse")

		if operation == "read" {
			assert.False(t, state.Raw.IsNull(), "legacy remote ACCOUNT ownership must not be silently forgotten")
		}
	}

	state := testState(t, r, data)

	var response resource.ValidateConfigResponse
	r.ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config(state)}, &response)
	assert.True(t, response.Diagnostics.HasError())
	assert.Zero(t, calls, "unsupported ACCOUNT tuples are rejected before any SQL")
}

// TestDebugGrantRemoteScopedMatches accepts a SHOW GRANTS FOR row only when database, object, scope, privilege, and
// grantee all match.
func TestDebugGrantRemoteScopedMatches(t *testing.T) {
	data := debugGrantFixture("sales@catalog", "IAM:auditor", "USER")
	base := sqlclient.Row{"database_name": "sales@catalog", "object_type": "DATABASE", "privilege_scope": "DATABASE", "privilege_type": "DEBUG", "identity_name": "IAM:auditor", "identity_type": "user"}
	assert.True(t, debugGrantRemoteMatches(base, data))

	for key, value := range map[string]string{"database_name": "other@catalog", "object_type": "TABLE", "privilege_scope": "TABLES", "privilege_type": "SELECT", "identity_name": "IAM:someone_else", "identity_type": "global_user"} {
		row := maps.Clone(base)
		row[key] = value
		assert.False(t, debugGrantRemoteMatches(row, data), "%s", key)
	}

	data.GranteeType = types.StringValue("ROLE")
	assert.False(t, debugGrantRemoteMatches(base, data))
}

// TestDebugGrantRemoteScopedReadFailurePreservesOwnership fails Create, Read, and Delete before any mutation when the
// scoped grant read fails, keeping state.
func TestDebugGrantRemoteScopedReadFailurePreservesOwnership(t *testing.T) {
	r := newDebugGrantResource().(*debugGrantResource)
	mutations := 0
	r.resourceClient = testResourceClient(queryFunc(func(_ context.Context, _ sqlclient.Connection, statement string, _ map[string]string) ([]sqlclient.Row, error) {
		if statement == readDebugGrantAuthStatement() {
			return []sqlclient.Row{{"auth_method": "IAM AssumeUser"}}, nil
		}

		if strings.HasPrefix(statement, "SHOW DATABASES") {
			return []sqlclient.Row{{"database_name": "sales@catalog"}}, nil
		}

		if strings.HasPrefix(statement, "SHOW GRANTS FOR") {
			return nil, errors.New("scoped catalog unavailable")
		}

		mutations++

		return nil, nil
	}))

	data := debugGrantFixture("sales@catalog", "IAM:auditor", "USER")
	for _, operation := range []string{"create", "read", "delete"} {
		state, diagnostics := applyOperation(t, r, operation, data, data, nil)
		require.True(t, diagnostics.HasError())
		assert.Contains(t, diagnostics.Errors()[0].Detail(), "scoped catalog unavailable")

		if operation == "read" {
			assert.False(t, state.Raw.IsNull())
		}
	}

	assert.Zero(t, mutations, "Create preflight and Delete must fail closed before SQL mutation")
}

// TestDebugGrantRemoteAuthenticationMustBeVerified fails closed unless the session authenticated through IAM
// AssumeUser, because other sessions can see a remote alias yet get empty scoped grant reads.
func TestDebugGrantRemoteAuthenticationMustBeVerified(t *testing.T) {
	for _, test := range []struct {
		name string
		rows []sqlclient.Row
		err  error
	}{
		{"visible_alias_password", []sqlclient.Row{{"auth_method": "password"}}, nil},
		{"unverified_iam_spelling", []sqlclient.Row{{"auth_method": "IAM"}}, nil},
		{"unknown", nil, nil},
		{"ambiguous", []sqlclient.Row{{"auth_method": "IAM AssumeUser"}, {"auth_method": "Ident"}}, nil},
		{"unavailable", nil, errors.New("permission denied for sys_connection_log")},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := newDebugGrantResource().(*debugGrantResource)
			mutations, scopedReads := 0, 0
			r.resourceClient = testResourceClient(queryFunc(func(_ context.Context, _ sqlclient.Connection, statement string, _ map[string]string) ([]sqlclient.Row, error) {
				switch {
				case statement == readDebugGrantAuthStatement():
					return test.rows, test.err
				case strings.HasPrefix(statement, "SHOW DATABASES"):
					return []sqlclient.Row{{"database_name": "sales@catalog"}}, nil
				case strings.HasPrefix(statement, "SHOW GRANTS FOR"):
					scopedReads++
					return nil, nil // Actual secret-session counterexample: visible alias, silently empty scoped rows.
				default:
					mutations++
					return nil, nil
				}
			}))
			data := debugGrantFixture("sales@catalog", "IAM:auditor", "USER")

			data.ID = r.identity(data.Database.ValueString(), data.fields())
			for _, operation := range []string{"create", "read", "delete"} {
				state, diagnostics := applyOperation(t, r, operation, data, data, nil)
				require.True(t, diagnostics.HasError(), "%s must fail closed", operation)
				assert.Contains(t, diagnostics.Errors()[0].Detail(), "authentication context")

				if operation != "create" {
					assert.False(t, state.Raw.IsNull(), "existing grant ownership must survive unsupported authentication")
				}
			}

			diagnostics := importAndRead(t, r, data.ID.ValueString())
			require.True(t, diagnostics.HasError(), "import must not interpret an unverified empty read as no grant")
			assert.Zero(t, mutations)
			assert.Zero(t, scopedReads, "unverified context must be rejected before trusting the empty scoped read")
		})
	}
}

// TestDebugGrantAuthSQL pins the query that reads the session's authentication method.
func TestDebugGrantAuthSQL(t *testing.T) {
	checkSQL(t, "debug_grant_context", []sqlCase{{name: "current_authentication", render: readDebugGrantAuthStatement}})
}
