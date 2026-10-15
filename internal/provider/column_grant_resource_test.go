package provider

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerReplacementPolicy("redshift_column_grant", map[string]replaceRule{
	"database_name": replaceAlways,
	"schema_name":   replaceAlways,
	"object_name":   replaceAlways,
	"grantee":       replaceAlways,
	"grantee_type":  replaceAlways,
	"privileges":    replaceNever,
})

// columnGrantLifecycleColumns is the lifecycle model's privilege map, which the fake populates.
var columnGrantLifecycleColumns = map[string][]string{"SELECT": {"id", "label"}, "UPDATE": {"label"}}

var _ = registerLifecycleCase(lifecycleCase{
	name: "column grant", kind: lifecyclePermission, new: newColumnGrantResource,
	model: columnGrantTestModel(columnGrantBaseFields, columnGrantLifecycleColumns),
	// The relation lives in another local database than admin, so the database check must find it.
	setup: func(c *catalog) { c.localDB = true },
	absent: func(c *catalog) {
		clear(fakeState[*columnGrantFake](c, "column_grant").grants)
	},
})

// columnGrantClient answers the user existence check that the legacy fake leaves empty, and delegates the rest.
func columnGrantClient(c *catalog) sqlclient.Client {
	return queryFunc(func(ctx context.Context, connection sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		if strings.HasPrefix(sql, "SELECT usename FROM pg_user") {
			return []sqlclient.Row{{"usename": parameters["name"]}}, nil
		}
		return c.Query(ctx, connection, sql, parameters)
	})
}

// TestColumnGrantTranscripts records grantee forms, quoted names and literal bindings, and convergence repair.
func TestColumnGrantTranscripts(t *testing.T) {
	user := maps.Clone(columnGrantBaseFields)
	user["grantee"], user["grantee_type"] = `Odd"O'Reilly\User`, "USER"
	group := maps.Clone(columnGrantBaseFields)
	group["grantee"], group["grantee_type"] = "readers", "GROUP"
	public := maps.Clone(columnGrantBaseFields)
	public["grantee"], public["grantee_type"] = "public", "PUBLIC"
	odd := map[string][]string{"SELECT": {`Odd"Column`, "UPPER"}, "UPDATE": {"UPPER"}}
	fresh := func(setup func(*catalog)) func() sqlclient.Client {
		return func() sqlclient.Client {
			c := fullCatalog()
			c.localDB = true
			clear(fakeState[*columnGrantFake](c, "column_grant").grants)
			if setup != nil {
				setup(c)
			}
			return columnGrantClient(c)
		}
	}
	granted := func(grantee string, columns map[string][]string) func(*catalog) {
		return func(c *catalog) { fakeState[*columnGrantFake](c, "column_grant").set(grantee, columns) }
	}
	userGrantee := sqlclient.Identifier(user["grantee"])
	runTranscripts(t, "column_grant/transcript", newColumnGrantResource, []transcriptCase{
		{name: "create_quoted_user", operation: "create", catalog: fresh(nil), planned: columnGrantTestModel(user, odd)},
		{name: "update_quoted_user", operation: "update", catalog: fresh(granted(userGrantee, map[string][]string{"SELECT": {"id"}})), prior: columnGrantTestModel(user, odd), planned: columnGrantTestModel(user, odd)},
		{name: "delete_quoted_user", operation: "delete", catalog: fresh(granted(userGrantee, odd)), prior: columnGrantTestModel(user, odd)},
		{name: "import_quoted_user", operation: "import", catalog: fresh(nil), planned: columnGrantTestModel(user, odd)},
		{name: "create_group", operation: "create", catalog: fresh(nil), planned: columnGrantTestModel(group, map[string][]string{"SELECT": {"id"}})},
		{name: "create_public", operation: "create", catalog: fresh(nil), planned: columnGrantTestModel(public, map[string][]string{"SELECT": {"label"}})},
		{name: "update_revoke_all", operation: "update", catalog: fresh(granted(`ROLE "example:readers"`, columnGrantLifecycleColumns)), prior: columnGrantTestModel(columnGrantBaseFields, columnGrantLifecycleColumns), planned: columnGrantTestModel(columnGrantBaseFields, nil)},
	})
}

// columnGrantPlan builds a plan for the resource from a model.
func columnGrantPlan(t *testing.T, r resource.Resource, model columnGrantModel) tfsdk.Plan {
	t.Helper()
	return tfsdk.Plan(testState(t, r, model))
}

// TestColumnGrantCreateRejectsInvalidTupleBeforeState reports invalid tuples during planning and keeps them out of state.
func TestColumnGrantCreateRejectsInvalidTupleBeforeState(t *testing.T) {
	for name, model := range map[string]columnGrantModel{
		"grantee":   columnGrantTestModel(map[string]string{"database_name": "warehouse", "schema_name": "serving", "object_name": "events", "grantee": "readers", "grantee_type": "PUBLIC"}, nil),
		"privilege": columnGrantTestModel(columnGrantBaseFields, map[string][]string{"INSERT": {"id"}}),
		"columns":   columnGrantTestModel(columnGrantBaseFields, map[string][]string{"SELECT": {}}),
	} {
		t.Run(name, func(t *testing.T) {
			r := newColumnGrantResource().(*columnGrantResource)
			r.resourceClient = testResourceClient(queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
				return nil, errors.New("unexpected SQL " + sql)
			}))
			plan := columnGrantPlan(t, r, model)
			resp := resource.CreateResponse{State: tfsdk.State{Schema: plan.Schema, Raw: tftypes.NewValue(plan.Raw.Type(), nil)}}
			r.Create(context.Background(), resource.CreateRequest{Plan: plan}, &resp)
			require.True(t, resp.Diagnostics.HasError())
			assert.True(t, resp.State.Raw.IsNull(), "invalid tuple must not be recorded in state")
			var validated resource.ValidateConfigResponse
			r.ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config(plan)}, &validated)
			assert.True(t, validated.Diagnostics.HasError(), "invalid tuple must be reported during planning")
		})
	}
	r := newColumnGrantResource()
	unknown := columnGrantTestModel(columnGrantBaseFields, nil)
	unknown.Grantee = types.StringUnknown()
	var validated resource.ValidateConfigResponse
	r.(resource.ResourceWithValidateConfig).ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config(testState(t, r, unknown))}, &validated)
	assert.False(t, validated.Diagnostics.HasError(), "unknown values are validated once known")
	unknownColumns := columnGrantTestModel(columnGrantBaseFields, nil)
	unknownColumns.Privileges = types.MapValueMust(types.SetType{ElemType: types.StringType}, map[string]attr.Value{"SELECT": types.SetUnknown(types.StringType)})
	require.NoError(t, validateColumnGrant(unknownColumns))
}

// TestColumnGrantFailurePaths covers each non-convergence cause, unmanaged catalog privileges, and refresh of a dropped
// relation.
func TestColumnGrantFailurePaths(t *testing.T) {
	model := columnGrantTestModel(columnGrantBaseFields, columnGrantLifecycleColumns)
	setup := func(mutate func(*catalog)) (*columnGrantResource, *catalog) {
		c := fullCatalog()
		c.localDB = true
		clear(fakeState[*columnGrantFake](c, "column_grant").grants)
		if mutate != nil {
			mutate(c)
		}
		r := newColumnGrantResource().(*columnGrantResource)
		r.resourceClient = testResourceClient(c)
		return r, c
	}
	t.Run("superseded", func(t *testing.T) {
		r, c := setup(nil)
		r.resourceClient = testResourceClient(queryFunc(func(ctx context.Context, connection sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
			if strings.HasPrefix(sql, "GRANT ") {
				return nil, nil // A table-level grant makes Redshift accept and ignore the column grant.
			}
			return c.Query(ctx, connection, sql, parameters)
		}))
		err := r.reconcile(context.Background(), model)
		require.ErrorContains(t, err, "did not converge")
		assert.Contains(t, err.Error(), "observed map[]", "the error shows what the catalog holds")
		assert.Contains(t, err.Error(), "on the whole table")
	})
	t.Run("case folded", func(t *testing.T) {
		r, c := setup(nil)
		// With enable_case_sensitive_identifier off, Redshift stores a delimited "Label" as label.
		r.resourceClient = testResourceClient(queryFunc(func(ctx context.Context, connection sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
			return c.Query(ctx, connection, strings.ReplaceAll(sql, `"Label"`, `"label"`), parameters)
		}))
		folded := columnGrantTestModel(columnGrantBaseFields, map[string][]string{"SELECT": {"Label"}})
		err := r.reconcile(context.Background(), folded)
		require.ErrorContains(t, err, "did not converge")
		assert.Contains(t, err.Error(), "desired map[SELECT:[Label]], observed map[SELECT:[label]]")
		assert.Contains(t, err.Error(), "enable_case_sensitive_identifier")
	})
	t.Run("relation vanished during apply", func(t *testing.T) {
		r, c := setup(nil)
		r.resourceClient = testResourceClient(queryFunc(func(ctx context.Context, connection sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
			rows, err := c.Query(ctx, connection, sql, parameters)
			if strings.HasPrefix(sql, "GRANT ") {
				fakeState[*columnGrantFake](c, "column_grant").relation = false
			}
			return rows, err
		}))
		err := r.reconcile(context.Background(), model)
		require.ErrorContains(t, err, "disappeared")
		assert.NotContains(t, err.Error(), "whole table", "a vanished relation is not blamed on a table-level grant")
	})
	t.Run("unmanaged privilege", func(t *testing.T) {
		r, c := setup(func(c *catalog) {
			fakeState[*columnGrantFake](c, "column_grant").set(`ROLE "example:readers"`, map[string][]string{"REFERENCES": {"id"}})
		})
		require.ErrorContains(t, r.reconcile(context.Background(), model), "unsupported catalog column privilege")
		assert.Empty(t, c.writes)
		observed := model
		_, found, err := r.read(context.Background(), &observed, false)
		require.NoError(t, err)
		require.True(t, found)
		assert.Equal(t, columnGrantColumns{"REFERENCES": {"id"}}, columnGrantDesired(observed.Privileges), "lookups observe unmanaged privileges")
	})
	t.Run("relation dropped", func(t *testing.T) {
		r, _ := setup(func(c *catalog) { fakeState[*columnGrantFake](c, "column_grant").relation = false })
		state := testState(t, r, withID(model, r.identity("admin", map[string]string{"x": "y"})))
		resp := resource.ReadResponse{State: state}
		r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)
		require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
		assert.True(t, resp.State.Raw.IsNull(), "a dropped relation removes the grant from state")
		require.ErrorContains(t, r.reconcile(context.Background(), model), "does not exist")
	})
	t.Run("invalid prior state", func(t *testing.T) {
		r, _ := setup(nil)
		broken := model
		broken.GranteeType = types.StringValue("DATASHARE")
		_, _, err := r.read(context.Background(), &broken, true)
		require.Error(t, err)
	})
}

// TestColumnGrantImport accepts the JSON identity Create writes and rejects incomplete ones.
func TestColumnGrantImport(t *testing.T) {
	r := newColumnGrantResource().(*columnGrantResource)
	r.resourceClient = testResourceClient(nil)
	fields := maps.Clone(columnGrantBaseFields)
	valid := r.identity("admin", fields).ValueString()
	incomplete := maps.Clone(fields)
	delete(incomplete, "object_name")
	encoded, err := json.Marshal(incomplete)
	require.NoError(t, err)
	for id, failure := range map[string]bool{valid: false, "invalid JSON": true, string(encoded): true} {
		resp := resource.ImportStateResponse{State: emptyState(t, r)}
		r.ImportState(context.Background(), resource.ImportStateRequest{ID: id}, &resp)
		assert.Equal(t, failure, resp.Diagnostics.HasError(), "%s: %v", id, resp.Diagnostics)
		if !failure {
			var imported columnGrantModel
			require.False(t, resp.State.Get(context.Background(), &imported).HasError())
			assert.Equal(t, "events", imported.ObjectName.ValueString())
			assert.Equal(t, "ROLE", imported.GranteeType.ValueString())
		}
	}
}
