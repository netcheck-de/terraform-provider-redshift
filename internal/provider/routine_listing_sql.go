package provider

import (
	"slices"

	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// Routine kinds as pg_proc_info.prokind reports them.
// https://docs.aws.amazon.com/redshift/latest/dg/r_PG_PROC_INFO.html
const (
	routineListingFunctions  sqlclient.Keyword = "p.prokind = 'f'"
	routineListingProcedures sqlclient.Keyword = "p.prokind = 'p'"
	routineListingRoutines   sqlclient.Keyword = "p.prokind IN ('f', 'p')"
)

// routineListingTypes maps the routine_type filter of redshift_routine_parameters to its prokind condition.
var routineListingTypes = map[string]sqlclient.Keyword{"": routineListingRoutines, "FUNCTION": routineListingFunctions, "PROCEDURE": routineListingProcedures}

// routineListingKinds maps pg_proc_info.prokind to the keyword SHOW PARAMETERS and routine_type use.
var routineListingKinds = map[string]sqlclient.Keyword{"f": "FUNCTION", "p": "PROCEDURE"}

// routineListingQuery lists user-defined routines of one kind in the connected database. pg_proc_info adds prokind
// to pg_proc; schemas starting with pg_ and information_schema hold only built-in routines, which would otherwise
// swamp the listing.
func routineListingQuery(kind sqlclient.Keyword, schema, name string) sqlclient.Query {
	columns := append(slices.Clone(externalFunctionColumns), "p.prokind AS kind")
	return sqlclient.Select(columns...).From("pg_proc_info p JOIN "+externalFunctionSource).
		Where(kind).
		Where("n.nspname !~ '^pg_'").
		Where("n.nspname <> 'information_schema'").
		OptEq("n.nspname", "schema", schema).
		OptEq("p.proname", "name", name).
		OrderBy("schema_name", "routine_name", "arguments")
}

// showRoutineParametersStatement renders SHOW PARAMETERS for one overload, identified by its database-qualified
// name and the input types the catalog reported for it.
// https://docs.aws.amazon.com/redshift/latest/dg/r_SHOW_PARAMETERS.html
func showRoutineParametersStatement(database, schema, name, kind, argumentTypes string) (string, error) {
	routineKind, err := sqlclient.OneOf(kind, "FUNCTION", "PROCEDURE")
	if err != nil {
		return "", err
	}
	signature, err := sqlclient.Signature(externalFunctionSignatureTypes(argumentTypes)...)
	if err != nil {
		return "", err
	}
	statement := sqlclient.Stmt("SHOW PARAMETERS OF").Kw(routineKind).Qualified(database, schema, name).Args(sqlclient.Kw(signature))
	return statement.String(), statement.Err()
}
