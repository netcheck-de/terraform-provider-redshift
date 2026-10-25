package provider

import (
	"fmt"
	"strings"

	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// readDatashareMembershipSuperuserQuery reports whether the session can see every catalog row, which absence checks
// require.
func readDatashareMembershipSuperuserQuery() sqlclient.Query {
	return sqlclient.Select("usesuper").From("pg_user").Where("usename = current_user")
}

// datashareMembershipKind validates the tuple and returns its TABLES or FUNCTIONS keyword.
func datashareMembershipKind(data datashareMembershipModel) (sqlclient.Keyword, error) {
	if data.Database.ValueString() == "" || data.Datashare.ValueString() == "" || data.Schema.ValueString() == "" {
		return "", fmt.Errorf("database, datashare, and schema must be nonempty")
	}
	// The member catalog flattens qualification without quoting or a schema column.
	if strings.Contains(data.Schema.ValueString(), ".") {
		return "", fmt.Errorf("exclusive membership cannot distinguish catalog names for a schema containing a period")
	}

	if data.Kind.ValueString() != "TABLES" && data.Kind.ValueString() != "FUNCTIONS" {
		return "", fmt.Errorf("kind must be TABLES or FUNCTIONS")
	}

	return sqlclient.OneOf(data.Kind.ValueString(), "TABLES", "FUNCTIONS")
}

// createDatashareMembershipStatement adds every current member of the kind in the schema to the share.
func createDatashareMembershipStatement(data datashareMembershipModel) (string, error) {
	kind, err := datashareMembershipKind(data)
	if err != nil {
		return "", err
	}

	return sqlclient.Stmt("ALTER DATASHARE").Ident(data.Datashare.ValueString()).Kw("ADD ALL", kind, "IN SCHEMA").Ident(data.Schema.ValueString()).String(), nil
}

// dropDatashareMembershipStatement removes every member of the kind in the schema from the share.
func dropDatashareMembershipStatement(data datashareMembershipModel) (string, error) {
	kind, err := datashareMembershipKind(data)
	if err != nil {
		return "", err
	}

	return sqlclient.Stmt("ALTER DATASHARE").Ident(data.Datashare.ValueString()).Kw("REMOVE ALL", kind, "IN SCHEMA").Ident(data.Schema.ValueString()).String(), nil
}

// readDatashareMembershipObjectsQuery lists the outbound share's current members of the kind.
func readDatashareMembershipObjectsQuery(data datashareMembershipModel) sqlclient.Query {
	query := sqlclient.Select("object_name").From("svv_datashare_objects").
		Where("share_type = 'OUTBOUND'").
		Where("share_name = :share", sqlclient.Bind("share", data.Datashare.ValueString()))
	if data.Kind.ValueString() == "FUNCTIONS" {
		return query.Where("object_type = 'function'")
	}

	return query.Where("object_type IN ('table', 'view', 'late binding view', 'materialized view')")
}

// readDatashareMembershipCandidatesQuery lists the schema's objects that ADD ALL of the kind would share.
func readDatashareMembershipCandidatesQuery(data datashareMembershipModel) sqlclient.Query {
	if data.Kind.ValueString() == "FUNCTIONS" {
		// Each SQL overload contributes one row; names alone cannot identify an individual overload, but counts
		// per name can verify that every overload is included by ADD ALL FUNCTIONS.
		return sqlclient.Select("p.proname AS member_name").
			From("pg_proc_info p JOIN pg_namespace n ON n.oid = p.pronamespace JOIN pg_language l ON l.oid = p.prolang").
			Where("n.nspname = :schema", sqlclient.Bind("schema", data.Schema.ValueString())).
			Where("p.prokind = 'f'").Where("l.lanname = 'sql'")
	}

	return sqlclient.Select("table_name AS member_name").From("svv_all_tables").
		Where("database_name = :database", sqlclient.Bind("database", data.Database.ValueString())).
		Where("schema_name = :schema", sqlclient.Bind("schema", data.Schema.ValueString())).
		Where("table_type <> 'EXTERNAL TABLE'")
}
