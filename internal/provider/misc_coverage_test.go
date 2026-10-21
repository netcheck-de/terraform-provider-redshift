package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCoverageMiscCommentKnownTarget(t *testing.T) {
	for _, test := range []struct {
		data    commentModel
		message string
	}{
		{commentModel{ObjectType: types.StringValue("FUNCTION")}, "unsupported"},
		{commentModel{ObjectType: types.StringValue("SCHEMA"), SchemaName: types.StringValue("public")}, "does not accept"},
		{commentModel{ObjectType: types.StringValue("SCHEMA"), ConstraintName: types.StringValue("pk")}, "constraint_name"},
		{commentModel{ObjectType: types.StringValue("TABLE"), SchemaName: types.StringValue("public")}, "object_name"},
	} {
		require.ErrorContains(t, commentKnownTarget(test.data), test.message)
	}
	modifier := identityProviderInstanceModifier{}
	assert.Equal(t, modifier.Description(context.Background()), modifier.MarkdownDescription(context.Background()))
}

func TestCoverageMiscSchemaQuotaMalformedSession(t *testing.T) {
	for _, test := range []struct {
		rows    []sqlclient.Row
		message string
	}{
		{nil, "no unique row"},
		{[]sqlclient.Row{{"superuser": "true"}, {"superuser": "false"}}, "no unique row"},
		{[]sqlclient.Row{{"superuser": "unexpected"}}, "unrecognized usesuper"},
	} {
		calls := 0
		r := &schemaResource{testResourceClient(queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
			calls++
			if calls == 1 {
				return nil, nil
			}
			return test.rows, nil
		}))}
		_, visible, err := r.schemaQuota(context.Background(), schemaModel{Database: types.StringValue("admin"), Name: types.StringValue("serving")}, "owner")
		require.ErrorContains(t, err, test.message)
		assert.False(t, visible)
		assert.Equal(t, 2, calls)
	}
}

func TestCoverageMiscSchemaDisappearsAfterAlter(t *testing.T) {
	for _, external := range []bool{false, true} {
		c := fullCatalog()
		client := queryFunc(func(ctx context.Context, connection sqlclient.Connection, query string, parameters map[string]string) ([]sqlclient.Row, error) {
			rows, err := c.Query(ctx, connection, query, parameters)
			if strings.HasPrefix(query, "ALTER SCHEMA ") || strings.HasPrefix(query, "ALTER EXTERNAL SCHEMA ") {
				c.schema, c.external = false, false
			}
			return rows, err
		})
		var r resource.Resource
		var before, after any
		if external {
			r = newExternalSchemaResource()
			model := externalSchemaModel{Database: types.StringValue("admin"), Name: types.StringValue("example_external"), GlueDatabase: types.StringValue("example_glue"), IAMRoleARN: types.StringValue("arn:aws:iam::123456789012:role/spectrum"), RefreshRevision: types.StringNull(), Owner: types.StringValue("admin")}
			before = model
			model.Owner = types.StringValue("replacement_owner")
			after = model
		} else {
			r = newSchemaResource()
			model := schemaModel{Database: types.StringValue("admin"), Name: types.StringValue("serving"), Owner: types.StringValue("admin")}
			before = model
			model.Owner = types.StringValue("replacement_owner")
			after = model
		}
		configureTestResource(t, r, client)
		_, diagnostics := applyOperation(t, r, "update", before, after, nil)
		require.True(t, diagnostics.HasError(), "%v", diagnostics)
		assert.Contains(t, diagnostics[0].Detail(), "disappeared during the update")
	}
}

func TestCoverageMiscUserUnresolvedAndRemovedSettings(t *testing.T) {
	before := grafanaUser()
	after := before
	after.SearchPath = types.ListUnknown(types.StringType)
	assert.Empty(t, userSearchPathStatements(before, after))
	after.SessionDefaults = types.MapUnknown(types.StringType)
	statements, err := userSessionDefaultStatements(before, after)
	require.NoError(t, err)
	assert.Empty(t, statements)
	before.SyslogAccess, before.ValidUntil = types.StringValue("UNRESTRICTED"), types.StringValue("2030-01-01")
	before.ConnectionLimit, before.SessionTimeout = types.Int64Value(10), types.Int64Value(60)
	after = before
	after.SyslogAccess, after.ValidUntil = types.StringNull(), types.StringNull()
	after.ConnectionLimit, after.SessionTimeout = types.Int64Null(), types.Int64Null()
	statements, err = alterUserStatements(before, after)
	require.NoError(t, err)
	require.Len(t, statements, 1)
	assert.Contains(t, statements[0], "VALID UNTIL 'infinity'")
	after.SyslogAccess = types.StringValue("INVALID")
	_, err = alterUserBatches(before, after)
	require.ErrorContains(t, err, "syslog_access")
}
