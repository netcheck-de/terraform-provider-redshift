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

// tableSecurityLifecycleModel protects the fake's relation with the documented defaults.
func tableSecurityLifecycleModel() tableSecurityModel {
	data := tableSecuritySample()
	data.Database = types.StringValue("admin")
	return data
}

// tableSecurityDeletion recognizes the statement that turns row-level security off.
func tableSecurityDeletion(sql string) bool {
	return strings.HasPrefix(sql, "ALTER TABLE ") && strings.HasSuffix(sql, " ROW LEVEL SECURITY OFF")
}

var (
	_ = registerLifecycleCase(lifecycleCase{
		name: "table_security", new: newTableSecurityResource, model: tableSecurityLifecycleModel(),
		absent: func(c *catalog) { rlsFakeOf(c).rlsOn = false }, isDeletion: tableSecurityDeletion,
	})
	_ = registerReplacementPolicy("redshift_table_security", map[string]replaceRule{
		"database":                     replaceAlways,
		"schema":                       replaceAlways,
		"relation":                     replaceAlways,
		"row_level_security":           replaceNever,
		"conjunction_type":             replaceNever,
		"datashare_row_level_security": replaceNever,
	})
	_ = registerValidateConfigCase("table_security", validateConfigCase{
		new:   newTableSecurityResource,
		valid: tableSecurityLifecycleModel(),
		invalid: func() tableSecurityModel {
			data := tableSecurityLifecycleModel()
			data.Schema = types.StringValue("")
			return data
		}(),
		unknown: func() tableSecurityModel {
			data := tableSecurityLifecycleModel()
			data.Schema = types.StringUnknown()
			return data
		}(),
	})
)

// TestTableSecurityAlterCoverage keeps an alter step for every in-place attribute.
func TestTableSecurityAlterCoverage(t *testing.T) {
	assertAlterCoverage(t, newTableSecurityResource(), tableSecurityAlterSteps)
}

// TestTableSecurityDeletion pins which statements count as removal.
func TestTableSecurityDeletion(t *testing.T) {
	assert.True(t, tableSecurityDeletion(`ALTER TABLE "public"."events" ROW LEVEL SECURITY OFF`))
	assert.False(t, tableSecurityDeletion(`ALTER TABLE "public"."events" ROW LEVEL SECURITY OFF FOR DATASHARES`))
	assert.False(t, tableSecurityDeletion(`ALTER TABLE "public"."events" ROW LEVEL SECURITY OFF CONJUNCTION TYPE OR`))
}

// TestTableSecurityTranscripts records in-place changes and the minimal configuration.
func TestTableSecurityTranscripts(t *testing.T) {
	model := tableSecurityLifecycleModel()
	off := model
	off.RowLevelSecurity = types.BoolValue(false)
	or := model
	or.ConjunctionType = types.StringValue("OR")
	shared := model
	shared.DatashareRowLevelSecurity = types.BoolValue(false)
	minimal := model
	minimal.ConjunctionType, minimal.DatashareRowLevelSecurity = types.StringUnknown(), types.BoolUnknown()
	unprotected := catalogWith(func(c *catalog) { rlsFakeOf(c).rlsOn = false })
	runTranscripts(t, "rls_transcripts/table_security", newTableSecurityResource, []transcriptCase{
		{name: "create_minimal", operation: "create", catalog: unprotected, planned: minimal},
		{name: "update_off", operation: "update", catalog: catalogWith(), prior: model, planned: off},
		{name: "update_conjunction", operation: "update", catalog: catalogWith(), prior: model, planned: or},
		{name: "update_datashare", operation: "update", catalog: catalogWith(), prior: model, planned: shared},
		{name: "delete_already_off", operation: "delete", catalog: unprotected, prior: off},
	})
}

// tableSecurityOperation runs one lifecycle RPC and decodes the resulting state.
func tableSecurityOperation(t *testing.T, client sqlclient.Client, operation string, prior, planned any) (tableSecurityModel, bool) {
	t.Helper()
	r := newTableSecurityResource()
	configureTestResource(t, r, client)
	state, diagnostics := applyOperation(t, r, operation, prior, planned, nil)
	var data tableSecurityModel
	if !state.Raw.IsNull() {
		require.False(t, state.Get(context.Background(), &data).HasError())
	}
	return data, diagnostics.HasError()
}

// TestTableSecurityLifecycle turns RLS on and off, keeps unset settings, and leaves policies alone on delete.
func TestTableSecurityLifecycle(t *testing.T) {
	c := fullCatalog()
	f := rlsFakeOf(c)
	f.rlsOn, f.conjunction, f.datashare = false, "or", false
	minimal := tableSecurityLifecycleModel()
	minimal.ConjunctionType, minimal.DatashareRowLevelSecurity = types.StringUnknown(), types.BoolUnknown()
	created, failed := tableSecurityOperation(t, c, "create", nil, minimal)
	require.False(t, failed)
	assert.True(t, f.rlsOn)
	assert.Equal(t, types.StringValue("OR"), created.ConjunctionType, "an unset conjunction keeps the padded catalog value")
	assert.Equal(t, types.BoolValue(false), created.DatashareRowLevelSecurity, "an unset datashare setting keeps the catalog value")
	assertLookupIdentity(t, created.ID, "admin", map[string]string{"schema": "public", "relation": "events"})

	f.rlsOn = false
	drifted, failed := tableSecurityOperation(t, c, "read", created, nil)
	require.False(t, failed)
	assert.Equal(t, types.BoolValue(false), drifted.RowLevelSecurity, "refresh reports an outside change")

	repaired, failed := tableSecurityOperation(t, c, "update", drifted, created)
	require.False(t, failed)
	assert.True(t, repaired.RowLevelSecurity.ValueBool())
	assert.True(t, f.rlsOn)

	_, failed = tableSecurityOperation(t, c, "delete", repaired, nil)
	require.False(t, failed)
	assert.False(t, f.rlsOn, "deletion turns row-level security off")
	assert.True(t, f.policy && f.attached, "deletion keeps policies and attachments")
	assert.Equal(t, "or", f.conjunction)
}

// TestTableSecurityUnlistedRelationDefaults reads a relation svv_rls_relation does not list as never protected.
func TestTableSecurityUnlistedRelationDefaults(t *testing.T) {
	client := queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
		if strings.HasPrefix(sql, "SELECT c.relname") {
			return []sqlclient.Row{{"relname": "events"}}, nil
		}
		return nil, nil
	})
	r := &tableSecurityResource{testResourceClient(client)}
	data := tableSecurityLifecycleModel()
	found, err := r.read(context.Background(), &data)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, types.BoolValue(false), data.RowLevelSecurity)
	assert.Equal(t, types.StringValue("AND"), data.ConjunctionType)
	assert.Equal(t, types.BoolValue(true), data.DatashareRowLevelSecurity)
}

// TestTableSecurityRejectsMalformedCatalog reports unparsable or ambiguous rows instead of guessing.
func TestTableSecurityRejectsMalformedCatalog(t *testing.T) {
	valid := sqlclient.Row{"is_rls_on": "t", "is_rls_datashare_on": "f", "rls_conjunction_type": "and"}
	for name, rows := range map[string][]sqlclient.Row{
		"switch":    {{"is_rls_on": "maybe", "is_rls_datashare_on": "f"}},
		"datashare": {{"is_rls_on": "t", "is_rls_datashare_on": ""}},
		"ambiguous": {valid, valid},
	} {
		t.Run(name, func(t *testing.T) {
			client := queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
				if strings.HasPrefix(sql, "SELECT c.relname") {
					return []sqlclient.Row{{"relname": "events"}}, nil
				}
				return rows, nil
			})
			data := tableSecurityLifecycleModel()
			_, err := (&tableSecurityResource{testResourceClient(client)}).read(context.Background(), &data)
			require.Error(t, err)
		})
	}
}

// TestTableSecurityVerifiesConvergence fails Create and Update when the catalog ignores a setting.
func TestTableSecurityVerifiesConvergence(t *testing.T) {
	for name, row := range map[string]sqlclient.Row{
		"row_level_security":           {"is_rls_on": "false", "is_rls_datashare_on": "true", "rls_conjunction_type": "and"},
		"conjunction_type":             {"is_rls_on": "true", "is_rls_datashare_on": "true", "rls_conjunction_type": "or "},
		"datashare_row_level_security": {"is_rls_on": "true", "is_rls_datashare_on": "false", "rls_conjunction_type": "and"},
	} {
		t.Run(name, func(t *testing.T) {
			client := queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
				switch {
				case strings.HasPrefix(sql, "SELECT c.relname"):
					return []sqlclient.Row{{"relname": "events"}}, nil
				case strings.HasPrefix(sql, "SELECT is_rls_on"):
					return []sqlclient.Row{row}, nil
				}
				return nil, nil
			})
			r := newTableSecurityResource()
			configureTestResource(t, r, client)
			state, diagnostics := applyOperation(t, r, "create", nil, tableSecurityLifecycleModel(), nil)
			require.True(t, diagnostics.HasError())
			assert.Contains(t, diagnostics.Errors()[0].Detail(), name)
			assert.True(t, state.Raw.IsFullyKnown(), "a failed verification still stores known state")
			_, diagnostics = applyOperation(t, r, "update", tableSecurityLifecycleModel(), tableSecurityLifecycleModel(), nil)
			require.True(t, diagnostics.HasError())
		})
	}
}

// TestTableSecurityRejectsInvalidPlansBeforeSQL keeps invalid settings out of SQL and state.
func TestTableSecurityRejectsInvalidPlansBeforeSQL(t *testing.T) {
	r := newTableSecurityResource()
	configureTestResource(t, r, queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
		return nil, errors.New("no SQL expected")
	}))
	invalid := tableSecurityLifecycleModel()
	invalid.Relation = types.StringValue("")
	state, diagnostics := applyOperation(t, r, "create", nil, invalid, nil)
	require.True(t, diagnostics.HasError())
	assert.True(t, state.Raw.IsNull())
	_, diagnostics = applyOperation(t, r, "update", tableSecurityLifecycleModel(), invalid, nil)
	require.True(t, diagnostics.HasError())
}
