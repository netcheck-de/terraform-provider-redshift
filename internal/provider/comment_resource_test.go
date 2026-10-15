package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerValidateConfigCase("comment", validateConfigCase{
	new:     newCommentResource,
	valid:   commentModel{DatabaseName: types.StringValue("analytics"), ObjectType: types.StringValue("SCHEMA"), ObjectName: types.StringValue("serving"), Text: types.StringValue("note")},
	invalid: commentModel{DatabaseName: types.StringValue("analytics"), ObjectType: types.StringValue("TABLE"), ObjectName: types.StringValue("t"), Text: types.StringValue("note")},
	unknown: commentModel{DatabaseName: types.StringValue("analytics"), ObjectType: types.StringValue("TABLE"), ObjectName: types.StringValue("t"), SchemaName: types.StringUnknown(), Text: types.StringValue("note")},
})

var _ = registerReplacementPolicy("redshift_comment", map[string]replaceRule{
	"database_name":   replaceAlways,
	"schema_name":     replaceAlways,
	"object_type":     replaceAlways,
	"object_name":     replaceAlways,
	"column_name":     replaceAlways,
	"constraint_name": replaceAlways,
	"text":            replaceNever,
})

// schemaComment supplies an annotation on a local schema for lifecycle tests.
func schemaComment() commentModel {
	return commentModel{DatabaseName: types.StringValue("analytics"), ObjectType: types.StringValue("SCHEMA"), ObjectName: types.StringValue("serving"), SchemaName: types.StringNull(), ColumnName: types.StringNull(), Text: types.StringValue("after")}
}

// TestCommentLifecycle checks annotation mutation, cleanup, and errors at every SQL boundary.
func TestCommentLifecycle(t *testing.T) {
	for _, operation := range []string{"create", "read", "update", "delete"} {
		t.Run(operation, func(t *testing.T) {
			require.True(t, invoke(t, newCommentResource(), operation, schemaComment(), true).HasError())
			queries := 0
			for failAt := 0; failAt <= queries; failAt++ {
				calls, text := 0, "before"
				client := queryFunc(func(_ context.Context, connection sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
					calls++
					if calls == failAt {
						return nil, errors.New("injected query failure")
					}
					if strings.HasPrefix(sql, "SELECT database_name") {
						return []sqlclient.Row{{"database_name": "analytics"}}, nil
					}
					assert.Equal(t, "analytics", connection.Database)
					if strings.HasPrefix(sql, "COMMENT ON") {
						text = ""
						if !strings.HasSuffix(sql, " IS NULL") {
							text = literals.FindStringSubmatch(sql)[1]
						}
						return nil, nil
					}
					return []sqlclient.Row{{"text": text}}, nil
				})
				r := &commentResource{testResourceClient(client)}
				diagnostics := invoke(t, r, operation, schemaComment(), false)
				if failAt == 0 {
					require.False(t, diagnostics.HasError(), "%v", diagnostics)
					queries = calls
				} else {
					require.True(t, diagnostics.HasError(), "query %d", failAt)
				}
			}
		})
	}
}

// TestCommentTargets validates catalog routing and safely quoted identifiers for all supported object kinds.
func TestCommentTargets(t *testing.T) {
	for _, test := range []struct {
		kind, schema, column, name string
		valid                      bool
	}{
		{"DATABASE", "", "", "analytics", true},
		{"DATABASE", "", "", "other", false},
		{"SCHEMA", "", "", "serving", true},
		{"SCHEMA", "invalid", "", "serving", false},
		{"TABLE", "serving", "", "table", true},
		{"VIEW", "serving", "", "view", true},
		{"COLUMN", "serving", "column", "table", true},
		{"COLUMN", "serving", "", "table", false},
		{"TABLE", "", "", "table", false},
		{"TABLE", "serving", "column", "table", false},
		{"CONSTRAINT", "serving", "", "table", false},
		{"unknown", "", "", "table", false},
	} {
		data := schemaComment()
		data.ObjectType, data.SchemaName, data.ColumnName, data.ObjectName = types.StringValue(test.kind), types.StringValue(test.schema), types.StringValue(test.column), types.StringValue(test.name)
		statement, query, err := commentTarget(data)
		assert.Equal(t, test.valid, err == nil)
		if test.valid {
			assert.Contains(t, statement.String(), `"`+test.name+`"`)
			_, parameters, err := query.Build()
			require.NoError(t, err)
			assert.Equal(t, test.name, parameters["name"])
		}
	}
}

// TestCommentMissingTargets checks database/object disappearance and ambiguous catalog identities.
func TestCommentMissingTargets(t *testing.T) {
	for _, mode := range []string{"database", "object", "ambiguous", "binding", "type"} {
		data := schemaComment()
		r := &commentResource{testResourceClient(queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
			if mode == "database" || (mode == "object" && !strings.HasPrefix(sql, "SELECT database_name")) {
				return nil, nil
			}
			rows := []sqlclient.Row{{"text": "after"}}
			if mode == "ambiguous" && !strings.HasPrefix(sql, "SELECT database_name") {
				rows = append(rows, rows[0])
			}
			return rows, nil
		}))}
		if mode == "binding" {
			data.ID = types.StringValue(`{"workgroup_name":"other","database":"admin"}`)
		}
		if mode == "type" {
			data.ObjectType = types.StringValue("unknown")
		}
		found, err := r.read(context.Background(), &data)
		assert.False(t, found)
		if mode == "database" || mode == "object" {
			require.NoError(t, err)
			for _, operation := range []string{"create", "read", "update", "delete"} {
				assert.Equal(t, operation == "create" || operation == "update", invoke(t, r, operation, data, false).HasError())
			}
		} else {
			require.Error(t, err)
		}
	}
}

// TestCommentConvergence checks no-op updates, empty annotations, and ineffective writes.
func TestCommentConvergence(t *testing.T) {
	for _, text := range []string{"after", "before", ""} {
		r := &commentResource{testResourceClient(queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
			if strings.HasPrefix(sql, "COMMENT") {
				return nil, nil
			}
			return []sqlclient.Row{{"text": text}}, nil
		}))}
		data := schemaComment()
		assert.Equal(t, text != "after", r.reconcile(context.Background(), data, false) != nil)
		data.Text = types.StringValue("")
		assert.Equal(t, text != "", r.reconcile(context.Background(), data, false) != nil)
	}
}

// TestCommentImport restores optional column/schema identity and rejects invalid IDs.
func TestCommentImport(t *testing.T) {
	r := &commentResource{testResourceClient(nil)}
	var metadata resource.MetadataResponse
	r.Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "redshift"}, &metadata)
	assert.Equal(t, "redshift_comment", metadata.TypeName)
	for _, id := range []string{"invalid", `{"workgroup_name":"warehouse","database":"admin","database_name":"analytics","object_type":"COLUMN","object_name":"table","schema_name":"serving","column_name":"column"}`} {
		resp := resource.ImportStateResponse{State: testState(t, r, schemaComment())}
		r.ImportState(context.Background(), resource.ImportStateRequest{ID: id}, &resp)
		assert.Equal(t, id == "invalid", resp.Diagnostics.HasError())
	}
	data := schemaComment()
	data.ObjectType, data.ObjectName, data.SchemaName, data.ColumnName = types.StringValue("COLUMN"), types.StringValue("table"), types.StringValue("serving"), types.StringValue("column")
	r.resourceClient = testResourceClient(queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
		return []sqlclient.Row{{"text": "after"}}, nil
	}))
	require.False(t, invoke(t, r, "create", data, false).HasError())
}

// TestCommentCreateRejectsInvalidTargetBeforeState keeps invalid targets out of state so they cannot block refresh.
func TestCommentCreateRejectsInvalidTargetBeforeState(t *testing.T) {
	r := &commentResource{testResourceClient(queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
		return nil, fmt.Errorf("unexpected SQL %q", sql)
	}))}
	data := commentModel{ID: types.StringNull(), DatabaseName: types.StringValue("analytics"), SchemaName: types.StringValue("serving"), ObjectType: types.StringValue("DATABASE"), ObjectName: types.StringValue("analytics"), ColumnName: types.StringNull(), Text: types.StringValue("note")}
	plan := testState(t, r, data)
	resp := resource.CreateResponse{State: tfsdk.State{Schema: plan.Schema, Raw: tftypes.NewValue(plan.Raw.Type(), nil)}}
	r.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan(plan)}, &resp)
	require.True(t, resp.Diagnostics.HasError())
	assert.True(t, resp.State.Raw.IsNull(), "invalid target must not be recorded in state")
}
