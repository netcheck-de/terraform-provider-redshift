package provider

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerLifecycleCase(lifecycleCase{name: "schema", new: newSchemaResource, model: schemaModel{Database: types.StringValue("admin"), Name: types.StringValue("serving"), Owner: types.StringValue("admin")}, absent: func(c *catalog) { c.schema = false }})

var _ = registerReplacementPolicy("redshift_schema", map[string]replaceRule{
	"database": replaceAlways,
	"name":     replaceAlways,
	"owner":    replaceNever,
	"quota":    replaceNever,
})

// TestSchemaCatalogOwnership checks observed schema ownership.
func TestSchemaCatalogOwnership(t *testing.T) {
	r := &schemaResource{testResourceClient(queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
		return []sqlclient.Row{{"schema_name": "serving", "owner": "warehouse_admin"}}, nil
	}))}
	data := schemaModel{Database: types.StringValue("admin"), Name: types.StringValue("serving")}
	found, err := r.read(context.Background(), &data)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "warehouse_admin", data.Owner.ValueString())
}

// TestSchemaRejectsIncompleteMetadata rejects missing or ambiguous catalog fields.
func TestSchemaRejectsIncompleteMetadata(t *testing.T) {
	for _, rows := range [][]sqlclient.Row{
		{{"schema_name": "serving"}},
		{{"schema_name": "serving", "owner": "admin"}, {"schema_name": "serving", "owner": "admin"}},
	} {
		r := &schemaResource{testResourceClient(queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
			return rows, nil
		}))}
		_, err := r.read(context.Background(), &schemaModel{Database: types.StringValue("admin"), Name: types.StringValue("serving")})
		require.Error(t, err)
	}
}

// TestDroppedParentDatabaseIsNotFound removes database-local objects whose database was dropped outside Terraform.
func TestDroppedParentDatabaseIsNotFound(t *testing.T) {
	client := testResourceClient(queryFunc(func(_ context.Context, target sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
		if target.Database == "admin" && strings.HasPrefix(sql, "SELECT database_name FROM svv_redshift_databases") {
			return nil, nil
		}
		return nil, fmt.Errorf("database %q does not exist", target.Database)
	}))
	database := types.StringValue("analytics")
	reads := map[string]func() (bool, error){
		"schema": func() (bool, error) {
			return (&schemaResource{client}).read(context.Background(), &schemaModel{Database: database, Name: types.StringValue("serving")})
		},
		"external_schema": func() (bool, error) {
			return (&externalSchemaResource{client}).read(context.Background(), &externalSchemaModel{Database: database, Name: types.StringValue("lake")})
		},
		"datashare": func() (bool, error) {
			return (&datashareResource{client}).read(context.Background(), &datashareModel{Database: database, Name: types.StringValue("share")})
		},
		"datashare_schema": func() (bool, error) {
			return (&datashareSchemaResource{client}).read(context.Background(), &datashareSchemaModel{Database: database, Datashare: types.StringValue("share"), Schema: types.StringValue("serving")})
		},
		"datashare_table": func() (bool, error) {
			return (&datashareTableResource{client}).read(context.Background(), datashareTableModel{Database: database, Datashare: types.StringValue("share"), Schema: types.StringValue("serving"), Table: types.StringValue("t")})
		},
		"datashare_grant": func() (bool, error) {
			return (&datashareGrantResource{client}).read(context.Background(), datashareGrantModel{Database: database, Datashare: types.StringValue("share"), AccountID: types.StringValue("123456789012")})
		},
	}
	for name, read := range reads {
		t.Run(name, func(t *testing.T) {
			found, err := read()
			require.NoError(t, err)
			assert.False(t, found)
		})
	}
}

// creationOnlyClient accepts CREATE statements and then fails or misses every catalog readback.
func creationOnlyClient(unavailable bool) resourceClient {
	return testResourceClient(queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
		if unavailable && !strings.HasPrefix(sql, "CREATE ") {
			return nil, fmt.Errorf("catalog unavailable")
		}
		return nil, nil
	}))
}

// TestSchemaCreationErrorsRetainKnownState keeps a known identity when readback fails after a successful CREATE.
func TestSchemaCreationErrorsRetainKnownState(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		t.Run(fmt.Sprintf("unavailable=%t", unavailable), func(t *testing.T) {
			r := &schemaResource{creationOnlyClient(unavailable)}
			state := testState(t, r, schemaModel{Database: types.StringValue("admin"), Name: types.StringValue("serving"), Owner: types.StringUnknown()})
			resp := resource.CreateResponse{State: tfsdk.State{Schema: state.Schema}}
			r.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan(state)}, &resp)
			require.True(t, resp.Diagnostics.HasError())
			var observed schemaModel
			require.False(t, resp.State.Get(context.Background(), &observed).HasError())
			assert.False(t, observed.ID.IsNull())
			assert.True(t, observed.Owner.IsNull())
			assert.True(t, resp.State.Raw.IsFullyKnown())
		})
	}
}

// TestSchemaQuotaRejectsMalformedSession fails instead of guessing quota visibility from an ambiguous or unrecognized session row.
func TestSchemaQuotaRejectsMalformedSession(t *testing.T) {
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

// TestSchemaUpdateReportsDisappearance reports a local or external schema that vanishes between ALTER and verification.
func TestSchemaUpdateReportsDisappearance(t *testing.T) {
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
