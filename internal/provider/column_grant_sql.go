package provider

import (
	"fmt"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// columnGrantPrivileges are the only privileges Redshift grants per column: SELECT and UPDATE on tables, SELECT on
// views. https://docs.aws.amazon.com/redshift/latest/dg/r_GRANT-usage-notes.html#r_GRANT-usage-notes-clp
var columnGrantPrivileges = []sqlclient.Keyword{"SELECT", "UPDATE"}

// columnGrantColumns maps each configured privilege to its sorted column names; absent privileges hold no columns.
type columnGrantColumns map[string][]string

// columnGrantTarget is the validated catalog and mutation contract of one relation/grantee tuple.
type columnGrantTarget struct {
	// database is the local database holding the relation, where grants and reads run.
	database string
	// checks confirm the grantee, database, and relation still exist.
	checks []catalogCheck
	// relation names the table or view in ON TABLE.
	relation []string
	// grantee is the rendered recipient, as principal renders it for every grant type.
	grantee sqlclient.Statement
	// query reads the explicit column privileges of this grantee on the relation.
	query sqlclient.Query
}

// columnGrantPrincipal adapts the model to principal, so column grants render and check grantees like every grant.
func columnGrantPrincipal(m columnGrantModel) types.Object {
	return types.ObjectValueMust(
		map[string]attr.Type{"grantee": types.StringType, "grantee_type": types.StringType},
		map[string]attr.Value{"grantee": m.Grantee, "grantee_type": m.GranteeType},
	)
}

// columnGrantIdentityType is the identity_type that SVV_COLUMN_PRIVILEGES reports: user, role, group, or public.
// https://docs.aws.amazon.com/redshift/latest/dg/r_SVV_COLUMN_PRIVILEGES.html
func columnGrantIdentityType(granteeType string) string {
	return strings.ToLower(granteeType)
}

// prepareColumnGrant validates the tuple and renders its checks and privilege read; it issues no SQL.
func prepareColumnGrant(m columnGrantModel) (columnGrantTarget, error) {
	grantee, check, err := principal(columnGrantPrincipal(m))
	if err != nil {
		return columnGrantTarget{}, err
	}
	database, schemaName, object := knownString(m.DatabaseName), knownString(m.SchemaName), knownString(m.ObjectName)
	// The relation check implies the schema, so no separate schema check is needed. Building the checks rejects an
	// empty database, schema, or object name.
	checks, err := newCatalogChecks(localDatabaseQuery(database), objectGrantTableQuery(database, schemaName, object))
	if err != nil {
		return columnGrantTarget{}, err
	}
	if check.sql != "" {
		checks = append([]catalogCheck{check}, checks...)
	}
	return columnGrantTarget{database: database, checks: checks, relation: []string{database, schemaName, object}, grantee: grantee, query: readColumnGrantQuery(m)}, nil
}

// readColumnGrantQuery lists the grantee's explicit column privileges on the relation. SVV_COLUMN_PRIVILEGES covers
// every identity type, including groups, which SHOW COLUMN GRANTS cannot select with FOR; it runs in the relation's
// database, so namespace and relation identify the object.
func readColumnGrantQuery(m columnGrantModel) sqlclient.Query {
	return sqlclient.Select("column_name", "privilege_type").From("svv_column_privileges").
		Where("namespace_name = :schema", sqlclient.Bind("schema", knownString(m.SchemaName))).
		Where("relation_name = :object", sqlclient.Bind("object", knownString(m.ObjectName))).
		Where("identity_name = :grantee", sqlclient.Bind("grantee", knownString(m.Grantee))).
		Where("identity_type = :identity_type", sqlclient.Bind("identity_type", columnGrantIdentityType(knownString(m.GranteeType)))).
		OrderBy("privilege_type", "column_name")
}

// spec renders the GRANT/REVOKE shape for columns: privilege ( column [, ...] ) ON TABLE relation, per the
// column-level syntax of r_GRANT and r_REVOKE. Redshift accepts ON TABLE for views as well.
func (target columnGrantTarget) spec(columns []string) grantSpec {
	items := make([]sqlclient.Statement, 0, len(columns))
	for _, column := range columns {
		items = append(items, sqlclient.Ident(column))
	}
	return grantSpec{object: sqlclient.Fragment().Paren(items...).Kw("ON TABLE").Qualified(target.relation...), grantee: target.grantee}
}

// columnGrantStatements revokes columns that are no longer desired before granting missing ones, one statement per
// privilege and direction, so a failure part-way never leaves more access than either side. Grant options are not
// rendered because column-level privileges do not support WITH GRANT OPTION.
func columnGrantStatements(target columnGrantTarget, current, desired columnGrantColumns) ([]string, error) {
	for _, side := range []columnGrantColumns{current, desired} {
		for privilege := range side {
			if !privilegeAllowed(columnGrantPrivileges, privilege) {
				return nil, fmt.Errorf("unsupported column privilege %q; use SELECT or UPDATE", privilege)
			}
		}
	}
	var revokes, grants []string
	for _, privilege := range columnGrantPrivileges {
		name := string(privilege)
		if removed := privilegesAbsent(desired[name], current[name]); len(removed) > 0 {
			revokes = append(revokes, target.spec(removed).statement(false, privilege))
		}
		if added := privilegesAbsent(current[name], desired[name]); len(added) > 0 {
			grants = append(grants, target.spec(added).statement(true, privilege))
		}
	}
	return append(revokes, grants...), nil
}

// columnGrantRows groups catalog rows by privilege with sorted, unique columns.
func columnGrantRows(rows []sqlclient.Row) columnGrantColumns {
	columns := columnGrantColumns{}
	for _, row := range rows {
		privilege := normalizePrivilege(row["privilege_type"])
		if !slices.Contains(columns[privilege], row["column_name"]) {
			columns[privilege] = append(columns[privilege], row["column_name"])
		}
	}
	for privilege := range columns {
		slices.Sort(columns[privilege])
	}
	return columns
}

// columnGrantValue converts grouped columns into the privileges map value.
func columnGrantValue(columns columnGrantColumns) types.Map {
	elements := make(map[string]attr.Value, len(columns))
	for privilege, names := range columns {
		values := make([]attr.Value, 0, len(names))
		for _, name := range names {
			values = append(values, types.StringValue(name))
		}
		elements[privilege] = types.SetValueMust(types.StringType, values)
	}
	return types.MapValueMust(types.SetType{ElemType: types.StringType}, elements)
}

// columnGrantDesired reads the configured map; an empty column set means the privilege holds no columns.
func columnGrantDesired(value types.Map) columnGrantColumns {
	columns := columnGrantColumns{}
	if value.IsNull() || value.IsUnknown() {
		return columns
	}
	for privilege, element := range value.Elements() {
		if names := knownStrings(element.(types.Set)); len(names) > 0 {
			columns[privilege] = names
		}
	}
	return columns
}

// validateColumnGrant checks the tuple and the configured privileges without issuing SQL.
func validateColumnGrant(m columnGrantModel) error {
	if _, err := prepareColumnGrant(m); err != nil {
		return err
	}
	if m.Privileges.IsNull() || m.Privileges.IsUnknown() {
		return nil
	}
	for privilege, element := range m.Privileges.Elements() {
		if !privilegeAllowed(columnGrantPrivileges, privilege) {
			return fmt.Errorf("unsupported column privilege %q; use SELECT or UPDATE", privilege)
		}
		set := element.(types.Set)
		if set.IsUnknown() {
			continue
		}
		if len(set.Elements()) == 0 {
			return fmt.Errorf("privilege %s needs at least one column; remove the key to revoke it", privilege)
		}
		for _, column := range knownStrings(set) {
			if column == "" {
				return fmt.Errorf("privilege %s lists an empty column name", privilege)
			}
		}
	}
	return nil
}
