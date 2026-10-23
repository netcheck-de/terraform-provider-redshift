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

var _ = registerParity(parityCase{source: newConfigurationDataSource, standalone: true, selectors: []string{"name"}})

// TestConfigurationSQL pins SHOW for valid parameter names and the names that must never reach the statement.
func TestConfigurationSQL(t *testing.T) {
	render := func(name string) func() (string, error) {
		return func() (string, error) { return readConfigurationStatement(name) }
	}
	checkSQL(t, "configuration", []sqlCase{
		{"search_path", render("search_path")},
		{"datestyle", render("datestyle")},
		{"enable_case_sensitive_identifier", render("enable_case_sensitive_identifier")},
		{"uppercase_rejected", render("Search_Path")},
		{"statement_rejected", render("search_path; DROP TABLE t")},
		{"quoted_rejected", render(`"search_path"`)},
		{"context_variable", render("app_context.user_id")},
		{"empty_context_segment_rejected", render("app_context..user_id")},
		{"leading_dot_rejected", render(".user_id")},
		{"trailing_dot_rejected", render("app_context.")},
		{"context_statement_rejected", render("app_context.user_id; SELECT 1")},
		{"long_context_segment_rejected", render("app_context." + strings.Repeat("a", 128))},
		{"all_rejected", render("all")},
		{"empty_rejected", render("")},
	})
}

// configurationRead reads name against client and returns the observed model.
func configurationRead(t *testing.T, name string, client sqlclient.Client) (configurationModel, bool, string) {
	t.Helper()
	source := newConfigurationDataSource()
	state, diagnostics := readSource(t, source, configurationModel{ID: types.StringNull(), Name: types.StringValue(name), Value: types.StringNull()}, client)
	if diagnostics.HasError() {
		return configurationModel{}, false, diagnostics.Errors()[0].Detail()
	}
	var observed configurationModel
	require.False(t, state.Get(context.Background(), &observed).HasError())
	return observed, true, ""
}

// TestConfigurationRead reads the value from the column named after the parameter, or from a single column of
// another spelling, in the administration database without writing.
func TestConfigurationRead(t *testing.T) {
	for _, test := range []struct {
		name  string
		row   sqlclient.Row
		value string
	}{
		{"search_path", sqlclient.Row{"search_path": "$user, public"}, "$user, public"},
		{"timezone", sqlclient.Row{"TimeZone": "UTC"}, "UTC"},
		{"query_group", sqlclient.Row{"other": "unset"}, "unset"},
		{"app_context.user_id", sqlclient.Row{"app_context.user_id": "reader"}, "reader"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var statements []string
			observed, ok, detail := configurationRead(t, test.name, queryFunc(func(_ context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
				statements = append(statements, target.Database+": "+sql)
				assert.Empty(t, parameters)
				return []sqlclient.Row{test.row}, nil
			}))
			require.True(t, ok, detail)
			assert.Equal(t, types.StringValue(test.value), observed.Value)
			assertLookupIdentity(t, observed.ID, "admin", map[string]string{"name": test.name})
			assert.Equal(t, []string{"admin: SHOW " + test.name}, statements)
		})
	}
}

// TestConfigurationReadFailures reports unknown parameters, unexpected results, and invalid names as errors.
func TestConfigurationReadFailures(t *testing.T) {
	for _, test := range []struct {
		name   string
		rows   []sqlclient.Row
		err    error
		detail string
	}{
		{"search_path", nil, errors.New(`unrecognized configuration parameter "search_path"`), `unrecognized configuration parameter "search_path"`},
		{"search_path", nil, nil, "SHOW search_path returned 0 rows, expected 1"},
		{"search_path", []sqlclient.Row{{"a": "1", "b": "2"}}, nil, "SHOW search_path returned no column named after the parameter"},
		{"Search_Path", nil, nil, `parameter name "Search_Path" must be a lowercase identifier or dotted session context name, such as search_path or app_context.user_id`},
		{"all", nil, nil, "parameter name must name one parameter, not all"},
	} {
		queried := false
		_, ok, detail := configurationRead(t, test.name, queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
			queried = true
			return test.rows, test.err
		}))
		assert.False(t, ok, test.detail)
		assert.Equal(t, test.detail, detail)
		assert.Equal(t, test.name == "search_path", queried, test.detail)
	}
}
