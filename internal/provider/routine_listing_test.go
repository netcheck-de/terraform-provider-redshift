package provider

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newRoutineParametersDataSource, collection: true, filters: []string{"database", "schema", "routine_name", "routine_type"}})

// routineListingRead reads a listing against client and returns its identity and items.
func routineListingRead(t *testing.T, source datasource.DataSource, filters map[string]string, client sqlclient.Client) (map[string]string, []map[string]any) {
	t.Helper()
	state, diagnostics := readSource(t, source, collectionConfig(t, source, filters), client)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)

	var observed types.Object
	require.False(t, state.Get(context.Background(), &observed).HasError())

	identity := map[string]string{}
	require.NoError(t, json.Unmarshal([]byte(observed.Attributes()["id"].(types.String).ValueString()), &identity))

	var items []map[string]any

	for _, element := range collectionResult(source, observed).Elements() {
		item := map[string]any{}

		for name, value := range element.(types.Object).Attributes() {
			switch value := value.(type) {
			case types.String:
				item[name] = value.ValueString()
			case types.Int64:
				item[name] = value.ValueInt64()
			case types.List:
				item[name] = externalFunctionTypeList(value)
			}
		}

		items = append(items, item)
	}

	return identity, items
}

// TestRoutineParametersListing runs SHOW PARAMETERS per overload, filters by routine type, and records the database
// that was read in the identity.
func TestRoutineParametersListing(t *testing.T) {
	identity, items := routineListingRead(t, newRoutineParametersDataSource(), nil, fullCatalog())
	assert.Equal(t, map[string]string{"database": "admin"}, identity, "the identity records the provider database that was read")
	assert.Len(t, items, 7, "SQL UDF, Lambda UDF, and two procedures")
	_, items = routineListingRead(t, newRoutineParametersDataSource(), map[string]string{"schema": "serving"}, fullCatalog())
	require.Len(t, items, 4)
	assert.Equal(t, map[string]any{
		"database": "admin", "schema": "serving", "routine_name": fakeExternalFunctionName, "routine_type": "FUNCTION",
		"arguments": []string{"CHARACTER VARYING"}, "parameter_name": "", "ordinal_position": int64(0), "mode": "RETURN", "data_type": "CHARACTER VARYING",
	}, items[0])
	assert.Equal(t, map[string]any{
		"database": "admin", "schema": "serving", "routine_name": fakeProcedureName, "routine_type": "PROCEDURE",
		"arguments": []string{"INTEGER"}, "parameter_name": "refreshed", "ordinal_position": int64(2), "mode": "OUT", "data_type": "BIGINT",
	}, items[3])
	identity, items = routineListingRead(t, newRoutineParametersDataSource(), map[string]string{"schema": "serving", "routine_type": "PROCEDURE"}, fullCatalog())
	assert.Equal(t, map[string]string{"database": "admin", "schema": "serving", "routine_type": "PROCEDURE"}, identity)
	require.Len(t, items, 2)

	for _, item := range items {
		assert.Equal(t, "PROCEDURE", item["routine_type"])
	}
}

// TestRoutineListingsReportFailures turns catalog errors and malformed rows into diagnostics.
func TestRoutineListingsReportFailures(t *testing.T) {
	routine := sqlclient.Row{"schema_name": "serving", "routine_name": "f", "arguments": "integer", "return_type": "integer", "volatility": "v", "security_definer": "false", "language": "sql", "owner": "admin", "kind": "f"}

	with := func(change func(sqlclient.Row)) sqlclient.Row {
		row := sqlclient.Row{}
		maps.Copy(row, routine)
		change(row)

		return row
	}
	for name, test := range map[string]struct {
		source     func() datasource.DataSource
		routines   []sqlclient.Row
		parameters []sqlclient.Row
		showErr    error
	}{
		"functions catalog error":  {source: newFunctionsDataSource},
		"functions volatility":     {source: newFunctionsDataSource, routines: []sqlclient.Row{with(func(row sqlclient.Row) { row["volatility"] = "?" })}},
		"external catalog error":   {source: newExternalFunctionsDataSource},
		"external volatility":      {source: newExternalFunctionsDataSource, routines: []sqlclient.Row{with(func(row sqlclient.Row) { row["volatility"] = "?" })}},
		"procedures security":      {source: newProceduresDataSource, routines: []sqlclient.Row{with(func(row sqlclient.Row) { row["security_definer"] = "maybe" })}},
		"procedures catalog error": {source: newProceduresDataSource},
		"parameters catalog error": {source: newRoutineParametersDataSource},
		"parameters kind":          {source: newRoutineParametersDataSource, routines: []sqlclient.Row{with(func(row sqlclient.Row) { row["kind"] = "a" })}},
		"parameters type":          {source: newRoutineParametersDataSource, routines: []sqlclient.Row{with(func(row sqlclient.Row) { row["arguments"] = "integer[]" })}},
		"parameters show error":    {source: newRoutineParametersDataSource, routines: []sqlclient.Row{routine}, showErr: errors.New("permission denied")},
		"parameters bad ordinal":   {source: newRoutineParametersDataSource, routines: []sqlclient.Row{routine}, parameters: []sqlclient.Row{{"ordinal_position": "first"}}},
		"parameters empty ordinal": {source: newRoutineParametersDataSource, routines: []sqlclient.Row{routine}, parameters: []sqlclient.Row{{"parameter_name": "x"}}},
	} {
		t.Run(name, func(t *testing.T) {
			client := queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
				switch {
				case strings.HasPrefix(sql, "SHOW PARAMETERS"):
					return test.parameters, test.showErr
				case test.routines == nil:
					return nil, errors.New("catalog unavailable")
				}

				return test.routines, nil
			})
			source := test.source()
			_, diagnostics := readSource(t, source, collectionConfig(t, source, nil), client)
			assert.True(t, diagnostics.HasError())
		})
	}
}
