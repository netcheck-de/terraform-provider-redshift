package provider

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	_ = registerParity(parityCase{source: newFunctionsDataSource, collection: true, filters: []string{"database", "schema", "name"}})
	_ = registerParity(parityCase{source: newProceduresDataSource, collection: true, filters: []string{"database", "schema", "name"}})
	_ = registerParity(parityCase{source: newRoutineParametersDataSource, collection: true, filters: []string{"database", "schema", "routine_name", "routine_type"}})
)

// routineListingRead reads a listing against client and returns its identity and items.
func routineListingRead(t *testing.T, source datasource.DataSource, filters map[string]string, client dataapi.Client) (map[string]string, []map[string]any) {
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

// TestFunctionsListing lists the Lambda UDF with its observable metadata and honors the filters.
func TestFunctionsListing(t *testing.T) {
	identity, items := routineListingRead(t, newFunctionsDataSource(), nil, fullCatalog())
	assert.Equal(t, map[string]string{"workgroup_name": "warehouse", "database": "admin"}, identity)
	require.Len(t, items, 1)
	assert.Equal(t, map[string]any{
		"database": "admin", "schema": "serving", "name": fakeExternalFunctionName, "arguments": []string{"CHARACTER VARYING"},
		"return_type": "CHARACTER VARYING", "volatility": "STABLE", "language": "EXFUNC", "owner": "admin",
	}, items[0])
	identity, items = routineListingRead(t, newFunctionsDataSource(), map[string]string{"database": "warehouse", "schema": "serving", "name": "other"}, fullCatalog())
	assert.Equal(t, map[string]string{"workgroup_name": "warehouse", "database": "warehouse", "schema": "serving", "name": "other"}, identity)
	assert.Empty(t, items)
}

// TestProceduresListing reports the procedure's security mode.
func TestProceduresListing(t *testing.T) {
	_, items := routineListingRead(t, newProceduresDataSource(), map[string]string{"schema": "serving"}, fullCatalog())
	require.Len(t, items, 1)
	assert.Equal(t, map[string]any{
		"database": "admin", "schema": "serving", "name": fakeProcedureName, "arguments": []string{"INTEGER"},
		"security": "DEFINER", "language": "PLPGSQL", "owner": "etl",
	}, items[0])
}

// TestRoutineParametersListing runs SHOW PARAMETERS per overload and filters by routine type.
func TestRoutineParametersListing(t *testing.T) {
	_, items := routineListingRead(t, newRoutineParametersDataSource(), nil, fullCatalog())
	require.Len(t, items, 4)
	assert.Equal(t, map[string]any{
		"database": "admin", "schema": "serving", "routine_name": fakeExternalFunctionName, "routine_type": "FUNCTION",
		"arguments": []string{"CHARACTER VARYING"}, "parameter_name": "", "ordinal_position": int64(0), "mode": "RETURN", "data_type": "CHARACTER VARYING",
	}, items[0])
	assert.Equal(t, map[string]any{
		"database": "admin", "schema": "serving", "routine_name": fakeProcedureName, "routine_type": "PROCEDURE",
		"arguments": []string{"INTEGER"}, "parameter_name": "refreshed", "ordinal_position": int64(2), "mode": "OUT", "data_type": "BIGINT",
	}, items[3])
	identity, items := routineListingRead(t, newRoutineParametersDataSource(), map[string]string{"routine_type": "PROCEDURE"}, fullCatalog())
	assert.Equal(t, "PROCEDURE", identity["routine_type"])
	require.Len(t, items, 2)
	for _, item := range items {
		assert.Equal(t, "PROCEDURE", item["routine_type"])
	}
}

// TestRoutineListingsReportFailures turns catalog errors and malformed rows into diagnostics.
func TestRoutineListingsReportFailures(t *testing.T) {
	routine := dataapi.Row{"schema_name": "serving", "routine_name": "f", "arguments": "integer", "return_type": "integer", "volatility": "v", "security_definer": "false", "language": "sql", "owner": "admin", "kind": "f"}
	with := func(change func(dataapi.Row)) dataapi.Row {
		row := dataapi.Row{}
		for key, value := range routine {
			row[key] = value
		}
		change(row)
		return row
	}
	for name, test := range map[string]struct {
		source     func() datasource.DataSource
		routines   []dataapi.Row
		parameters []dataapi.Row
		showErr    error
	}{
		"functions catalog error":  {source: newFunctionsDataSource},
		"functions volatility":     {source: newFunctionsDataSource, routines: []dataapi.Row{with(func(row dataapi.Row) { row["volatility"] = "?" })}},
		"procedures security":      {source: newProceduresDataSource, routines: []dataapi.Row{with(func(row dataapi.Row) { row["security_definer"] = "maybe" })}},
		"procedures catalog error": {source: newProceduresDataSource},
		"parameters catalog error": {source: newRoutineParametersDataSource},
		"parameters kind":          {source: newRoutineParametersDataSource, routines: []dataapi.Row{with(func(row dataapi.Row) { row["kind"] = "a" })}},
		"parameters type":          {source: newRoutineParametersDataSource, routines: []dataapi.Row{with(func(row dataapi.Row) { row["arguments"] = "integer[]" })}},
		"parameters show error":    {source: newRoutineParametersDataSource, routines: []dataapi.Row{routine}, showErr: errors.New("permission denied")},
		"parameters bad ordinal":   {source: newRoutineParametersDataSource, routines: []dataapi.Row{routine}, parameters: []dataapi.Row{{"ordinal_position": "first"}}},
		"parameters empty ordinal": {source: newRoutineParametersDataSource, routines: []dataapi.Row{routine}, parameters: []dataapi.Row{{"parameter_name": "x"}}},
	} {
		t.Run(name, func(t *testing.T) {
			client := queryFunc(func(_ context.Context, _ dataapi.Connection, sql string, _ map[string]string) ([]dataapi.Row, error) {
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
