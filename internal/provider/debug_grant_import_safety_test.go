package provider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDebugGrantImportUnreliableRemoteReadPreservesState keeps an imported remote grant's identity when authentication,
// visibility, or the scoped read cannot be trusted, and stops reading at the first such failure.
func TestDebugGrantImportUnreliableRemoteReadPreservesState(t *testing.T) {
	for _, kind := range []string{"USER", "ROLE"} {
		for _, failure := range []struct {
			name       string
			visible    bool
			authMethod string
			authErr    error
			detail     string
			queries    int
		}{
			{name: "invisible alias", authMethod: "IAM AssumeUser", detail: "not visible", queries: 2},
			{name: "scoped catalog error", visible: true, authMethod: "IAM AssumeUser", detail: "scoped catalog unavailable", queries: 3},
			{name: "visible alias password", visible: true, authMethod: "password", detail: "authentication context", queries: 1},
			{name: "unknown authentication", visible: true, detail: "authentication context", queries: 1},
			{name: "authentication unavailable", visible: true, authErr: errors.New("permission denied for sys_connection_log"), detail: "authentication context", queries: 1},
		} {
			t.Run(kind+"/"+failure.name, func(t *testing.T) {
				ctx := context.Background()
				r := newDebugGrantResource().(*debugGrantResource)
				var statements []string
				mutations := 0
				r.resourceClient = testResourceClient(queryFunc(func(_ context.Context, _ sqlclient.Connection, statement string, _ map[string]string) ([]sqlclient.Row, error) {
					statements = append(statements, statement)
					switch {
					case statement == readDebugGrantAuthStatement():
						if failure.authErr != nil || failure.authMethod == "" {
							return nil, failure.authErr
						}
						return []sqlclient.Row{{"auth_method": failure.authMethod}}, nil
					case strings.HasPrefix(statement, "SHOW DATABASES"):
						if failure.visible {
							return []sqlclient.Row{{"database_name": "sales@catalog"}}, nil
						}
						return nil, nil
					case strings.HasPrefix(statement, "SHOW GRANTS FOR"):
						return nil, errors.New("scoped catalog unavailable")
					default:
						mutations++
						return nil, errors.New("unexpected SQL: " + statement)
					}
				}))
				principal := "IAM:auditor"
				if kind == "ROLE" {
					principal = "idc:audit_team"
				}
				data := debugGrantFixture("sales@catalog", principal, kind)
				data.ID = r.identity(data.Database.ValueString(), data.fields())
				imported := resource.ImportStateResponse{State: emptyState(t, r)}
				r.ImportState(ctx, resource.ImportStateRequest{ID: data.ID.ValueString()}, &imported)
				require.False(t, imported.Diagnostics.HasError(), "%v", imported.Diagnostics)
				assert.Empty(t, statements, "import only restores identity")
				var restored debugGrantModel
				require.False(t, imported.State.Get(ctx, &restored).HasError())
				require.Equal(t, data, restored)

				response := resource.ReadResponse{State: imported.State}
				r.Read(ctx, resource.ReadRequest{State: imported.State}, &response)
				require.True(t, response.Diagnostics.HasError())
				assert.Contains(t, response.Diagnostics.Errors()[0].Detail(), failure.detail)
				assert.False(t, response.State.Raw.IsNull())
				assert.True(t, response.State.Raw.Equal(imported.State.Raw), "unreliable refresh must retain every imported identity field")
				assert.Zero(t, mutations)
				assert.Len(t, statements, failure.queries, "failed authentication or visibility must stop subsequent catalog reads")
			})
		}
	}
}
