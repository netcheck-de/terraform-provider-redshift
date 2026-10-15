package provider

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// grantUserModel is a TABLES grant in the shared database to the fake family's user.
func grantUserModel(privileges, options []string) grantModel {
	return grantModel{
		DatabaseName: types.StringValue("analytics"), User: types.StringValue(grantFakeUser), Scope: types.StringValue("TABLES"),
		Privileges:            types.SetValueMust(types.StringType, grantStringValues(privileges)),
		GrantOptionPrivileges: types.SetValueMust(types.StringType, grantStringValues(options)),
	}
}

// TestGrantUserFailures injects a failure at every SQL call of each operation on a user tuple with grant options,
// as the shared lifecycle harness does for the role tuple, and checks missing parents.
func TestGrantUserFailures(t *testing.T) {
	model := grantUserModel([]string{"SELECT"}, []string{"SELECT"})
	for _, operation := range []string{"create", "read", "update", "delete"} {
		t.Run(operation, func(t *testing.T) {
			queries := 0
			for failAt := 0; failAt <= queries; failAt++ {
				c := fullCatalog()
				if operation == "update" {
					// A plain INSERT must be revoked before SELECT is granted with its option.
					family := c.family("grant_user").(*grantUserFamily)
					family.privileges, family.options = map[string]bool{"INSERT": true}, map[string]bool{}
				}
				calls := 0
				r := newGrantResource()
				configureTestResource(t, r, queryFunc(func(ctx context.Context, connection dataapi.Connection, sql string, parameters map[string]string) ([]dataapi.Row, error) {
					calls++
					if calls == failAt {
						return nil, fmt.Errorf("injected API failure")
					}
					return c.Query(ctx, connection, sql, parameters)
				}))
				diagnostics := invoke(t, r, operation, model, false)
				if failAt == 0 {
					require.False(t, diagnostics.HasError(), "%v", diagnostics)
					queries = calls
				} else {
					require.True(t, diagnostics.HasError(), "query %d failure must be reported", failAt)
				}
			}
			r := newGrantResource()
			configureTestResource(t, r, &catalog{database: true})
			diagnostics := invoke(t, r, operation, model, false)
			assert.Equal(t, operation == "create" || operation == "update", diagnostics.HasError(), "%v", diagnostics)
		})
	}
}

// TestGrantUserTranscripts records grant option upgrades and downgrades for a user recipient.
func TestGrantUserTranscripts(t *testing.T) {
	selectOnly := []string{"SELECT"}
	catalogWithUser := func(privileges, options []string) func() dataapi.Client {
		return catalogWith(func(c *catalog) {
			family := c.family("grant_user").(*grantUserFamily)
			family.privileges, family.options = map[string]bool{}, map[string]bool{}
			for _, privilege := range privileges {
				family.privileges[privilege] = true
			}
			for _, option := range options {
				family.options[option] = true
			}
		})
	}
	runTranscripts(t, "grant/user", newGrantResource, []transcriptCase{
		{name: "create_with_option", operation: "create", catalog: catalogWithUser(nil, nil), planned: grantUserModel([]string{"INSERT", "SELECT"}, selectOnly)},
		{name: "read_with_option", operation: "read", catalog: catalogWithUser(selectOnly, selectOnly), prior: grantUserModel(selectOnly, selectOnly)},
		{name: "upgrade", operation: "update", catalog: catalogWithUser(selectOnly, nil), prior: grantUserModel(selectOnly, nil), planned: grantUserModel(selectOnly, selectOnly)},
		{name: "downgrade", operation: "update", catalog: catalogWithUser(selectOnly, selectOnly), prior: grantUserModel(selectOnly, selectOnly), planned: grantUserModel(selectOnly, nil)},
		{name: "delete_with_option", operation: "delete", catalog: catalogWithUser([]string{"INSERT", "SELECT"}, selectOnly), prior: grantUserModel([]string{"INSERT", "SELECT"}, selectOnly)},
		{name: "import", operation: "import", catalog: catalogWithUser(nil, nil), planned: grantUserModel(selectOnly, selectOnly)},
	})
}

// TestGrantUserReadsOptionsPerTuple keeps other identities, scopes, and schema-level rows out of a user tuple and
// reports a privilege with the option when any of its rows carries it.
func TestGrantUserReadsOptionsPerTuple(t *testing.T) {
	r := &grantResource{testResourceClient(queryFunc(func(_ context.Context, _ dataapi.Connection, sql string, _ map[string]string) ([]dataapi.Row, error) {
		switch {
		case strings.HasPrefix(sql, "SELECT database_type"):
			return []dataapi.Row{{"database_type": "local"}}, nil
		case strings.HasPrefix(sql, "SELECT usename"):
			return []dataapi.Row{{"usename": "analyst"}}, nil
		case sql == `SHOW GRANTS ON DATABASE "analytics" FOR "analyst"`:
			row := func(identity, kind, scope, privilege, option string) dataapi.Row {
				return dataapi.Row{"database_name": "analytics", "identity_name": identity, "identity_type": kind, "privilege_scope": scope, "privilege_type": privilege, "admin_option": option}
			}
			schemaRow := row("analyst", "user", "TABLES", "TRUNCATE", "f")
			schemaRow["schema_name"] = "serving"
			return []dataapi.Row{
				row("analyst", "user", "TABLES", "SELECT", "false"),
				row("analyst", "user", "TABLES", "SELECT", "t"),
				row("analyst", "user", "TABLES", "INSERT", "f"),
				row("analyst", "role", "TABLES", "DELETE", "f"),
				row("other", "user", "TABLES", "UPDATE", "f"),
				row("analyst", "user", "SCHEMAS", "USAGE", "f"),
				schemaRow,
			}, nil
		default:
			return nil, fmt.Errorf("unexpected SQL %q", sql)
		}
	}))}
	data := grantTestModel(grantModel{DatabaseName: types.StringValue("analytics"), User: types.StringValue("analyst"), Scope: types.StringValue("TABLES")})
	_, found, err := r.read(context.Background(), &data)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, []string{"INSERT", "SELECT"}, knownStrings(data.Privileges))
	assert.Equal(t, []string{"SELECT"}, knownStrings(data.GrantOptionPrivileges))
}

// TestGrantValidation covers the scope, recipient, privilege, and grant option rules shared by Create and
// ValidateConfig.
func TestGrantValidation(t *testing.T) {
	for _, test := range []struct {
		name                string
		data                grantModel
		privileges, options []string
		err                 string
	}{
		{name: "user options", data: grantModel{User: types.StringValue("analyst"), Scope: types.StringValue("TABLES")}, privileges: []string{"INSERT", "SELECT"}, options: []string{"SELECT"}},
		{name: "languages", data: grantModel{Role: types.StringValue("readers"), Scope: types.StringValue("LANGUAGES")}, privileges: []string{"USAGE"}},
		{name: "copy jobs", data: grantModel{User: types.StringValue("analyst"), Scope: types.StringValue("COPY JOBS")}, privileges: []string{"CREATE", "ALTER", "DROP"}, options: []string{"DROP"}},
		{name: "templates in schema", data: grantModel{Role: types.StringValue("readers"), Scope: types.StringValue("TEMPLATES"), SchemaName: types.StringValue("serving")}, privileges: []string{"ALTER", "DROP", "USAGE"}},
		{name: "role options", data: grantModel{Role: types.StringValue("readers"), Scope: types.StringValue("TABLES")}, privileges: []string{"SELECT"}, options: []string{"SELECT"}, err: "requires a user recipient"},
		{name: "datashare options", data: grantModel{Datashare: types.StringValue("share"), Scope: types.StringValue("SCHEMA"), SchemaName: types.StringValue("serving")}, privileges: []string{"USAGE"}, options: []string{"USAGE"}, err: "requires a user recipient"},
		{name: "option outside privileges", data: grantModel{User: types.StringValue("analyst"), Scope: types.StringValue("TABLES")}, privileges: []string{"SELECT"}, options: []string{"INSERT"}, err: `"INSERT" is not in privileges`},
		{name: "languages in schema", data: grantModel{Role: types.StringValue("readers"), Scope: types.StringValue("LANGUAGES"), SchemaName: types.StringValue("serving")}, privileges: []string{"USAGE"}, err: "schema_name"},
		{name: "copy jobs privilege", data: grantModel{Role: types.StringValue("readers"), Scope: types.StringValue("COPY JOBS")}, privileges: []string{"USAGE"}, err: `COPY JOBS grants support only CREATE, ALTER, DROP, not "USAGE"`},
		{name: "two recipients", data: grantModel{Role: types.StringValue("readers"), User: types.StringValue("analyst"), Scope: types.StringValue("TABLES")}, privileges: []string{"SELECT"}, err: "exactly one"},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := test.data
			data.DatabaseName = types.StringValue("analytics")
			data.Privileges = types.SetValueMust(types.StringType, grantStringValues(test.privileges))
			data.GrantOptionPrivileges = types.SetValueMust(types.StringType, grantStringValues(test.options))
			r := &grantResource{testResourceClient(queryFunc(func(_ context.Context, _ dataapi.Connection, sql string, _ map[string]string) ([]dataapi.Row, error) {
				return nil, fmt.Errorf("unexpected SQL %q", sql)
			}))}
			var validated resource.ValidateConfigResponse
			r.ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config(testState(t, r, data))}, &validated)
			if test.err == "" {
				require.NoError(t, data.validate())
				assert.False(t, validated.Diagnostics.HasError(), "%v", validated.Diagnostics)
				return
			}
			require.ErrorContains(t, data.validate(), test.err)
			assert.True(t, validated.Diagnostics.HasError(), "invalid tuples must be reported during planning")
			state, diagnostics := applyOperation(t, r, "create", nil, data, nil)
			require.True(t, diagnostics.HasError())
			assert.True(t, state.Raw.IsNull(), "invalid tuples must not be recorded in state")
		})
	}
}

// TestGrantUserImport restores a user tuple, including its schema binding, from the identity Create records, and
// rejects an identity without a recipient.
func TestGrantUserImport(t *testing.T) {
	r := &grantResource{testResourceClient(&catalog{})}
	for id, user := range map[string]string{
		`{"workgroup_name":"warehouse","database":"admin","database_name":"analytics","user":"analyst","scope":"TEMPLATES","schema_name":"serving"}`: "analyst",
		`{"workgroup_name":"warehouse","database":"admin","database_name":"analytics","scope":"TABLES"}`:                                             "",
	} {
		resp := resource.ImportStateResponse{State: emptyState(t, r)}
		r.ImportState(context.Background(), resource.ImportStateRequest{ID: id}, &resp)
		if user == "" {
			assert.True(t, resp.Diagnostics.HasError(), "an identity without a recipient must be rejected")
			continue
		}
		require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
		var imported grantModel
		require.False(t, resp.State.Get(context.Background(), &imported).HasError())
		assert.Equal(t, user, imported.User.ValueString())
		assert.Equal(t, "serving", imported.SchemaName.ValueString())
		assert.True(t, imported.Role.IsNull())
	}
}

// TestGrantUserRejectsUnconvergedOptions fails when the catalog keeps a grant option the plan removes.
func TestGrantUserRejectsUnconvergedOptions(t *testing.T) {
	c := fullCatalog()
	r := &grantResource{testResourceClient(queryFunc(func(ctx context.Context, connection dataapi.Connection, sql string, parameters map[string]string) ([]dataapi.Row, error) {
		if strings.HasPrefix(sql, "REVOKE GRANT OPTION") {
			return nil, nil
		}
		return c.Query(ctx, connection, sql, parameters)
	}))}
	require.ErrorContains(t, r.reconcile(context.Background(), grantUserModel([]string{"SELECT"}, nil)), "did not converge")
}
