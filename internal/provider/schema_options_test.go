package provider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// schemaOptionsModel returns the "serving" schema the fake catalog knows, with owner and quota set.
func schemaOptionsModel(owner string, quota int64) schemaModel {
	return schemaModel{Database: types.StringValue("admin"), Name: types.StringValue("serving"), Owner: types.StringValue(owner), Quota: types.Int64Value(quota)}
}

// TestSchemaAlterCoverage checks that every in-place schema option has an ALTER SCHEMA step.
func TestSchemaAlterCoverage(t *testing.T) {
	assertAlterCoverage(t, newSchemaResource(), schemaAlterSteps)
}

// TestSchemaOptionTranscripts records schema creation with owner and quota, in-place changes, refresh, and import.
func TestSchemaOptionTranscripts(t *testing.T) {
	defaults := schemaOptionsModel("admin", -1)
	configured := schemaOptionsModel(`Etl"Owner`, 2048)
	unconfigured := schemaModel{Database: types.StringValue("admin"), Name: types.StringValue("serving"), Owner: types.StringUnknown(), Quota: types.Int64Unknown()}
	absent := catalogWith(func(c *catalog) { c.schema = false })
	limited := catalogWith(func(c *catalog) {
		f := c.family("databases").(*databasesFamily)
		f.schemaOwner, f.schemaQuota = `Etl"Owner`, "2048"
	})
	runTranscripts(t, "schema_options", newSchemaResource, []transcriptCase{
		{name: "create_options", operation: "create", catalog: absent, planned: configured},
		{name: "create_defaults", operation: "create", catalog: absent, planned: unconfigured},
		{name: "read_options", operation: "read", catalog: limited, prior: configured},
		{name: "update_options", operation: "update", catalog: catalogWith(), prior: defaults, planned: configured},
		{name: "update_unlimited", operation: "update", catalog: limited, prior: configured, planned: defaults},
		{name: "import_options", operation: "import", catalog: absent, planned: configured},
	})
}

// TestSchemaOptionsMustConverge reports an acknowledged CREATE or ALTER whose option the catalog does not hold,
// while a created schema keeps fully known state.
func TestSchemaOptionsMustConverge(t *testing.T) {
	for _, operation := range []string{"create", "update"} {
		t.Run(operation, func(t *testing.T) {
			c := fullCatalog()
			c.schema = operation == "update"
			client := queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
				switch {
				case strings.HasPrefix(sql, "ALTER SCHEMA"):
					return nil, nil
				case strings.HasPrefix(sql, "CREATE SCHEMA"):
					sql = `CREATE SCHEMA "serving"`
				}
				return c.Query(ctx, target, sql, parameters)
			})
			r := &schemaResource{testResourceClient(client)}
			state, diagnostics := applyOperation(t, r, operation, schemaOptionsModel("admin", -1), schemaOptionsModel("etl", 300), nil)
			require.True(t, diagnostics.HasError())
			assert.Contains(t, diagnostics.Errors()[0].Detail(), "owner is")
			assert.True(t, state.Raw.IsFullyKnown())
		})
	}
}

// TestSchemaOptionsDetectDrift refreshes a quota changed outside Terraform and lets Update restore it.
func TestSchemaOptionsDetectDrift(t *testing.T) {
	c := fullCatalog()
	r := &schemaResource{testResourceClient(c)}
	data := schemaOptionsModel("admin", 2048)
	fakeState[*databasesFamily](c, "databases").schemaQuota = "100"
	found, err := r.read(context.Background(), &data)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, int64(100), data.Quota.ValueInt64())
	desired := schemaOptionsModel("admin", 2048)
	_, diagnostics := applyOperation(t, r, "update", data, desired, nil)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.Equal(t, []string{`ALTER SCHEMA "serving" QUOTA 2048 MB`}, c.writes)
}

// TestSchemaQuotaValue decodes the quota view, where a schema without a quota may have no row or no value.
func TestSchemaQuotaValue(t *testing.T) {
	for _, test := range []struct {
		rows     []sqlclient.Row
		expected int64
	}{
		{nil, -1},
		{[]sqlclient.Row{{"quota": ""}}, -1},
		{[]sqlclient.Row{{"quota": "0"}}, -1},
		{[]sqlclient.Row{{"quota": " 51200 "}}, 51200},
	} {
		quota, err := schemaQuotaValue(test.rows)
		require.NoError(t, err)
		assert.Equal(t, test.expected, quota)
	}
	for _, rows := range [][]sqlclient.Row{{{"quota": "lots"}}, {{"quota": "1"}, {"quota": "2"}}} {
		_, err := schemaQuotaValue(rows)
		require.Error(t, err)
	}
}

// TestSchemaQuotaReadFailures reports quota read errors and ambiguous quota rows.
func TestSchemaQuotaReadFailures(t *testing.T) {
	for name, quota := range map[string]func() ([]sqlclient.Row, error){
		"error":     func() ([]sqlclient.Row, error) { return nil, errors.New("view unavailable") },
		"ambiguous": func() ([]sqlclient.Row, error) { return []sqlclient.Row{{"quota": "1"}, {"quota": "2"}}, nil },
	} {
		t.Run(name, func(t *testing.T) {
			r := &schemaResource{testResourceClient(queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
				if strings.HasPrefix(sql, "SELECT quota") {
					return quota()
				}
				return []sqlclient.Row{{"schema_name": "serving", "owner": "admin"}}, nil
			}))}
			_, err := r.read(context.Background(), &schemaModel{Database: types.StringValue("admin"), Name: types.StringValue("serving")})
			require.Error(t, err)
		})
	}
}

// TestSchemaUpdateRejectsInvalidQuota refuses a quota the validator would reject before reaching SQL.
func TestSchemaUpdateRejectsInvalidQuota(t *testing.T) {
	c := fullCatalog()
	r := &schemaResource{testResourceClient(c)}
	_, diagnostics := applyOperation(t, r, "update", schemaOptionsModel("admin", -1), schemaOptionsModel("admin", 0), nil)
	require.True(t, diagnostics.HasError())
	_, diagnostics = applyOperation(t, r, "create", nil, schemaOptionsModel("admin", -7), nil)
	require.True(t, diagnostics.HasError())
	assert.Empty(t, c.writes)
}

// TestSchemaQuotaHiddenFromRegularUser keeps the known quota of a schema owned by another user, which
// SVV_REDSHIFT_SCHEMA_QUOTA hides from a regular user, instead of reading it as UNLIMITED.
func TestSchemaQuotaHiddenFromRegularUser(t *testing.T) {
	regular := func(owner, quota string) *catalog {
		c := fullCatalog()
		f := fakeState[*databasesFamily](c, "databases")
		f.sessionRegular, f.schemaOwner, f.schemaQuota = true, owner, quota
		return c
	}
	r := &schemaResource{testResourceClient(regular("etl", "2048"))}
	for name, prior := range map[string]types.Int64{"known": types.Int64Value(100), "unknown": types.Int64Unknown(), "null": types.Int64Null()} {
		t.Run(name, func(t *testing.T) {
			data := schemaModel{Database: types.StringValue("admin"), Name: types.StringValue("serving"), Quota: prior}
			found, err := r.read(context.Background(), &data)
			require.NoError(t, err)
			require.True(t, found)
			assert.Equal(t, "etl", data.Owner.ValueString())
			if prior.IsUnknown() {
				assert.True(t, data.Quota.IsNull(), "an unreadable quota is not reported as UNLIMITED")
			} else {
				assert.Equal(t, prior, data.Quota, "the prior quota is kept rather than shown as drift")
			}
		})
	}

	own := schemaModel{Database: types.StringValue("admin"), Name: types.StringValue("serving"), Quota: types.Int64Value(2048)}
	found, err := (&schemaResource{testResourceClient(regular("admin", ""))}).read(context.Background(), &own)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, int64(schemaUnlimited), own.Quota.ValueInt64(), "the owner's missing row means UNLIMITED")

	c := regular("admin", "")
	c.schema = false
	state, diagnostics := applyOperation(t, &schemaResource{testResourceClient(c)}, "create", nil, schemaOptionsModel("etl", 2048), nil)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	var created schemaModel
	require.False(t, state.Get(context.Background(), &created).HasError())
	assert.Equal(t, int64(2048), created.Quota.ValueInt64())
}
