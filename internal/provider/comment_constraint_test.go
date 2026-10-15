package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// commentConstraintModel supplies an annotation on a table's primary key.
func commentConstraintModel() commentModel {
	return commentModel{DatabaseName: types.StringValue("analytics"), ObjectType: types.StringValue("CONSTRAINT"), SchemaName: types.StringValue("serving"), ObjectName: types.StringValue("orders"), ConstraintName: types.StringValue("orders_pkey"), ColumnName: types.StringNull(), Text: types.StringValue("note")}
}

// TestCommentConstraintTranscripts records the full SQL conversation for constraint annotations. It owns its own
// group because the shared comment transcripts belong to TestCommentTranscripts.
func TestCommentConstraintTranscripts(t *testing.T) {
	text := func(value string) func() sqlclient.Client {
		return func() sqlclient.Client { return &commentCatalog{text: value} }
	}
	model := commentConstraintModel()
	quoted := model
	quoted.SchemaName, quoted.ObjectName, quoted.ConstraintName = types.StringValue(`Odd"Schema`), types.StringValue(`Odd"Table`), types.StringValue(`Odd"Key`)
	revised := quoted
	revised.Text = types.StringValue(`Owner's \notes`)
	runTranscripts(t, "lifecycle/comment_constraint", newCommentResource, []transcriptCase{
		{name: "create", operation: "create", catalog: text(""), planned: model},
		{name: "read", operation: "read", catalog: text("note"), prior: model},
		{name: "update_quoted", operation: "update", catalog: text("note"), prior: quoted, planned: revised},
		{name: "delete_quoted", operation: "delete", catalog: text(`Owner's \notes`), prior: revised},
		{name: "import", operation: "import", catalog: text(""), planned: model},
	})
}

// TestCommentConstraintLifecycle checks that the constraint target reaches the constraint catalog, survives import,
// and leaves state once the constraint or its table disappears.
func TestCommentConstraintLifecycle(t *testing.T) {
	var reads []string
	exists := true
	r := &commentResource{testResourceClient(queryFunc(func(_ context.Context, connection sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		if strings.HasPrefix(sql, "SELECT database_name") {
			return []sqlclient.Row{{"database_name": "analytics"}}, nil
		}
		assert.Equal(t, "analytics", connection.Database)
		if strings.HasPrefix(sql, "COMMENT ON CONSTRAINT") {
			return nil, nil
		}
		reads = append(reads, sql)
		assert.Equal(t, map[string]string{"constraint": "orders_pkey", "name": "orders", "schema": "serving"}, parameters)
		if !exists {
			return nil, nil
		}
		return []sqlclient.Row{{"text": "note"}}, nil
	}))}
	require.False(t, invoke(t, r, "read", commentConstraintModel(), false).HasError())
	require.NotEmpty(t, reads)
	assert.Contains(t, reads[0], "FROM pg_constraint k")

	exists = false
	state := testState(t, r, commentConstraintModel())
	resp := resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)
	require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
	assert.True(t, resp.State.Raw.IsNull(), "a dropped constraint removes the comment from state")
	assert.True(t, invoke(t, r, "create", commentConstraintModel(), false).HasError(), "a missing constraint cannot be annotated")
	assert.False(t, invoke(t, r, "delete", commentConstraintModel(), false).HasError(), "clearing a vanished constraint's comment is a no-op")

	imported := resource.ImportStateResponse{State: emptyState(t, r)}
	r.ImportState(context.Background(), resource.ImportStateRequest{ID: `{"workgroup_name":"warehouse","database":"admin","database_name":"analytics","object_type":"CONSTRAINT","object_name":"orders","schema_name":"serving","constraint_name":"orders_pkey"}`}, &imported)
	require.False(t, imported.Diagnostics.HasError(), "%v", imported.Diagnostics)
	var restored commentModel
	require.False(t, imported.State.Get(context.Background(), &restored).HasError())
	assert.Equal(t, types.StringValue("orders_pkey"), restored.ConstraintName)
	assert.True(t, restored.ColumnName.IsNull())
}

// TestCommentConstraintValidateConfig mirrors the Create-time target check for constraint annotations at plan time.
func TestCommentConstraintValidateConfig(t *testing.T) {
	r := newCommentResource()
	for name, test := range map[string]struct {
		mutate  func(*commentModel)
		invalid bool
	}{
		"valid":              {func(*commentModel) {}, false},
		"missing constraint": {func(m *commentModel) { m.ConstraintName = types.StringNull() }, true},
		"with column":        {func(m *commentModel) { m.ColumnName = types.StringValue("id") }, true},
		"on table kind":      {func(m *commentModel) { m.ObjectType = types.StringValue("TABLE") }, true},
		"missing schema":     {func(m *commentModel) { m.SchemaName = types.StringNull() }, true},
		"unknown constraint": {func(m *commentModel) { m.ConstraintName = types.StringUnknown() }, false},
	} {
		t.Run(name, func(t *testing.T) {
			data := commentConstraintModel()
			test.mutate(&data)
			var resp resource.ValidateConfigResponse
			r.(resource.ResourceWithValidateConfig).ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config(testState(t, r, data))}, &resp)
			assert.Equal(t, test.invalid, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
		})
	}
}
