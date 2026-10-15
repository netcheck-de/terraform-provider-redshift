package provider

import (
	"errors"
	"strings"

	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// Names of the fake's routines; statements naming other routines pass to the next family.
const (
	fakeExternalFunctionName = "f_exfunc_upper"
	fakeProcedureName        = "sp_refresh"
)

// externalFunctionFake emulates one Lambda UDF and one stored procedure in schema serving.
type externalFunctionFake struct {
	// exists records whether the external function exists.
	exists bool
	// owner is the function owner.
	owner string
	// volatility is the pg_proc.provolatile code.
	volatility string
	// language is the pg_language name of the function, exfunc unless a test simulates a name clash.
	language string
	// procedure records whether the stored procedure exists, for the listings.
	procedure bool
	// replaced counts CREATE OR REPLACE statements, so tests can see a restated definition.
	replaced int
}

var _ = registerFakeFamily("external_function", func() fakeFamily {
	return &externalFunctionFake{owner: "admin", volatility: "v", language: "exfunc"}
})

// populate makes the function and the procedure exist.
func (f *externalFunctionFake) populate() {
	f.exists, f.procedure, f.volatility = true, true, "s"
}

// functionRow is the catalog row of the external function.
func (f *externalFunctionFake) functionRow() dataapi.Row {
	return dataapi.Row{"schema_name": "serving", "routine_name": fakeExternalFunctionName, "arguments": "character varying", "return_type": "character varying", "volatility": f.volatility, "security_definer": "false", "language": f.language, "owner": f.owner, "kind": "f"}
}

// procedureRow is the catalog row of the stored procedure.
func (f *externalFunctionFake) procedureRow() dataapi.Row {
	return dataapi.Row{"schema_name": "serving", "routine_name": fakeProcedureName, "arguments": "integer", "return_type": "void", "volatility": "v", "security_definer": "true", "language": "plpgsql", "owner": "etl", "kind": "p"}
}

// listing answers the pg_proc_info listing with the routines matching its kind and filters.
func (f *externalFunctionFake) listing(sql string, parameters map[string]string) []dataapi.Row {
	var rows []dataapi.Row
	if f.exists && (strings.Contains(sql, "p.prokind = 'f'") || strings.Contains(sql, "p.prokind IN")) {
		rows = append(rows, f.functionRow())
	}
	if f.procedure && (strings.Contains(sql, "p.prokind = 'p'") || strings.Contains(sql, "p.prokind IN")) {
		rows = append(rows, f.procedureRow())
	}
	var matching []dataapi.Row
	for _, row := range rows {
		if schema, ok := parameters["schema"]; ok && schema != row["schema_name"] {
			continue
		}
		if name, ok := parameters["name"]; ok && name != row["routine_name"] {
			continue
		}
		matching = append(matching, row)
	}
	return matching
}

// query applies the statements of the two routines and answers their catalog reads.
func (f *externalFunctionFake) query(_ *catalog, _ dataapi.Connection, sql string, parameters map[string]string) ([]dataapi.Row, bool, error) {
	function := strings.Contains(sql, `."`+fakeExternalFunctionName+`"(`)
	switch {
	case strings.HasPrefix(sql, "SELECT n.nspname AS schema_name, p.proname AS routine_name,") && strings.Contains(sql, " FROM pg_proc_info p JOIN "):
		return f.listing(sql, parameters), true, nil
	case strings.Contains(sql, "FROM pg_proc p JOIN") && parameters["name"] == fakeExternalFunctionName:
		if f.exists {
			return []dataapi.Row{f.functionRow()}, true, nil
		}
		return nil, true, nil
	case strings.HasPrefix(sql, "SHOW PARAMETERS OF FUNCTION") && strings.HasSuffix(sql, `."`+fakeExternalFunctionName+`"(CHARACTER VARYING)`):
		return []dataapi.Row{
			{"parameter_name": "", "ordinal_position": "0", "parameter_type": "RETURN", "data_type": "character varying"},
			{"parameter_name": "", "ordinal_position": "1", "parameter_type": "IN", "data_type": "character varying"},
		}, true, nil
	case strings.HasPrefix(sql, "SHOW PARAMETERS OF PROCEDURE") && strings.HasSuffix(sql, `."`+fakeProcedureName+`"(INTEGER)`):
		return []dataapi.Row{
			{"parameter_name": "batch", "ordinal_position": "1", "parameter_type": "IN", "data_type": "integer"},
			{"parameter_name": "refreshed", "ordinal_position": "2", "parameter_type": "OUT", "data_type": "bigint"},
		}, true, nil
	case !function:
		return nil, false, nil
	case strings.HasPrefix(sql, "CREATE EXTERNAL FUNCTION"), strings.HasPrefix(sql, "CREATE OR REPLACE EXTERNAL FUNCTION"):
		replace := strings.HasPrefix(sql, "CREATE OR REPLACE ")
		if f.exists && !replace {
			return nil, true, errors.New("function already exists with same argument types")
		}
		if replace {
			f.replaced++
		} else {
			f.owner = "admin"
		}
		f.exists, f.volatility = true, "v"
		if strings.Contains(sql, " STABLE LAMBDA ") {
			f.volatility = "s"
		}
		return nil, true, nil
	case strings.HasPrefix(sql, "ALTER FUNCTION") && strings.Contains(sql, ` OWNER TO "`):
		if !f.exists {
			return nil, true, errors.New("function does not exist")
		}
		_, owner, _ := strings.Cut(sql, ` OWNER TO "`)
		f.owner = strings.ReplaceAll(strings.TrimSuffix(owner, `"`), `""`, `"`)
		return nil, true, nil
	case strings.HasPrefix(sql, "DROP FUNCTION"):
		if !f.exists {
			return nil, true, errors.New("function does not exist")
		}
		f.exists = false
		return nil, true, nil
	}
	return nil, false, nil
}
