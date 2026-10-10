package provider

import (
	"testing"

	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRoutineListingSQL pins the routine listing reads against PG_PROC_INFO and SHOW PARAMETERS, including
// names that need quoting and catalog types that are not valid Redshift types.
func TestRoutineListingSQL(t *testing.T) {
	built := func(query sqlclient.Query) func() (string, error) {
		return func() (string, error) {
			sql, _, err := query.Build()
			return sql, err
		}
	}
	show := func(database, schema, name, kind, argumentTypes string) func() (string, error) {
		return func() (string, error) {
			return showRoutineParametersStatement(database, schema, name, kind, argumentTypes)
		}
	}
	checkSQL(t, "routine_listing", []sqlCase{
		{"functions", built(routineListingQuery(routineListingFunctions, "", ""))},
		{"functions_filtered", built(routineListingQuery(routineListingFunctions, `Odd"Schema`, `it's \f`))},
		{"procedures", built(routineListingQuery(routineListingProcedures, "serving", ""))},
		{"routines", built(routineListingQuery(routineListingRoutines, "", "f_exfunc_upper"))},
		{"show_function_parameters", show("analytics", "serving", "f_exfunc_upper", "FUNCTION", "character varying, numeric")},
		{"show_procedure_parameters_quoted", show(`Odd"Db`, `Odd"Schema`, `Sp"Refresh`, "PROCEDURE", "integer")},
		{"show_parameters_without_arguments", show("analytics", "serving", "f_now", "FUNCTION", "")},
		{"show_parameters_error_type", show("analytics", "serving", "f_x", "FUNCTION", "integer); DROP TABLE t; --")},
		{"show_parameters_error_kind", show("analytics", "serving", "f_x", "AGGREGATE", "integer")},
	})
}

// TestRoutineListingBindings sends listing filters as bindings and omits unset ones.
func TestRoutineListingBindings(t *testing.T) {
	_, parameters, err := routineListingQuery(routineListingRoutines, `Odd"Schema`, `it's \f`).Build()
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"schema": `Odd"Schema`, "name": `it's \f`}, parameters)
	_, parameters, err = routineListingQuery(routineListingFunctions, "", "").Build()
	require.NoError(t, err)
	assert.Nil(t, parameters)
	assert.Equal(t, routineListingRoutines, routineListingTypes[""])
}
