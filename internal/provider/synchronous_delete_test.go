package provider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeleteSkipsObservationQueries drops a schema without its quota lookup and a procedure without its parameter
// listing, so metadata only a refresh decodes cannot block deletion, and nothing is read after the DROP.
func TestDeleteSkipsObservationQueries(t *testing.T) {
	for _, test := range []struct {
		name  string
		new   func() resource.Resource
		model any
		row   sqlclient.Row
	}{
		{
			name: "schema", new: newSchemaResource,
			model: schemaModel{Database: types.StringValue("admin"), Name: types.StringValue("serving")},
			row:   sqlclient.Row{"schema_name": "serving"},
		},
		{
			name: "procedure", new: newProcedureResource, model: procedureLifecycleModel(),
			row: sqlclient.Row{"owner": "admin", "security_definer": "invalid"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, denied := range []bool{false, true} {
				t.Run(map[bool]string{false: "success", true: "drop denied"}[denied], func(t *testing.T) {
					var calls []string
					client := queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
						calls = append(calls, sql)
						switch len(calls) {
						case 1:
							assert.True(t, strings.HasPrefix(sql, "SELECT "), sql)
							return []sqlclient.Row{test.row}, nil
						case 2:
							assert.True(t, strings.HasPrefix(sql, "DROP "), sql)
							if denied {
								return nil, errors.New("DROP denied")
							}
							return nil, nil
						default:
							t.Errorf("unexpected metadata or post-DROP query: %s", sql)
							return nil, errors.New("unexpected query")
						}
					})
					r := test.new()
					configureTestResource(t, r, client)
					_, diagnostics := applyOperation(t, r, "delete", test.model, nil, nil)
					require.Equal(t, denied, diagnostics.HasError(), "%v", diagnostics)
					if denied {
						assert.Contains(t, diagnostics.Errors()[0].Detail(), "DROP denied")
					}
					assert.Len(t, calls, 2)
				})
			}
		})
	}
}
