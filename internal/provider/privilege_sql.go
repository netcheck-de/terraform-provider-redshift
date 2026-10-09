package provider

import (
	"fmt"
	"slices"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// grantSpec renders the GRANT and REVOKE statements of one permission tuple, so every grant resource shares
// one statement shape: [prefix] GRANT privilege [object] TO grantee [option], and REVOKE … FROM grantee.
type grantSpec struct {
	// prefix precedes GRANT/REVOKE, for example ALTER DEFAULT PRIVILEGES FOR USER "owner"; empty for plain grants.
	prefix sqlclient.Statement
	// object follows the privilege keyword, including its ON or FOR clause; empty for system privileges.
	object sqlclient.Statement
	// grantee is the receiving identity with any ROLE, GROUP or DATASHARE keyword it requires.
	grantee sqlclient.Statement
	// option follows the grantee on GRANT only, such as WITH GRANT OPTION; REVOKE removes the privilege with it.
	option sqlclient.Keyword
	// optionRevoke opens the statement that removes only the grant option; empty means REVOKE GRANT OPTION FOR,
	// the object form. Scoped permissions document REVOKE GRANT OPTION without FOR (scopedOptionRevoke).
	optionRevoke sqlclient.Keyword
	// render replaces the standard form for commands with another shape, such as GRANT ASSUMEROLE … FOR command.
	render func(grant bool, privilege sqlclient.Keyword) string
}

// statement renders one GRANT (grant = true) or REVOKE of privilege.
func (s grantSpec) statement(grant bool, privilege sqlclient.Keyword) string {
	if s.render != nil {
		return s.render(grant, privilege)
	}
	if grant {
		return s.prefix.Kw("GRANT", privilege).Append(s.object).Kw("TO").Append(s.grantee).OptKw(s.option).String()
	}
	return s.prefix.Kw("REVOKE", privilege).Append(s.object).Kw("FROM").Append(s.grantee).String()
}

// scopedOptionRevoke is the grant option revoke of scoped permissions (FOR TABLES IN …), which r_REVOKE documents
// as REVOKE [ GRANT OPTION ] rather than the REVOKE [ GRANT OPTION FOR ] of object permissions.
// https://docs.aws.amazon.com/redshift/latest/dg/r_REVOKE.html#revoke-scoped-permissions
const scopedOptionRevoke sqlclient.Keyword = "REVOKE GRANT OPTION"

// optionStatement renders GRANT … WITH GRANT OPTION (grant = true) for a user that should pass privilege on, or
// REVOKE GRANT OPTION FOR …, which removes only that right and keeps the privilege itself (r_GRANT, r_REVOKE).
// Redshift reports dependent grants as an error instead of revoking them, because the statement never adds CASCADE.
func (s grantSpec) optionStatement(grant bool, privilege sqlclient.Keyword) (string, error) {
	if s.render != nil || s.option != "" {
		return "", fmt.Errorf("grant options are not supported for this permission")
	}
	if grant {
		return s.prefix.Kw("GRANT", privilege).Append(s.object).Kw("TO").Append(s.grantee).Kw("WITH GRANT OPTION").String(), nil
	}
	revoke := s.optionRevoke
	if revoke == "" {
		revoke = "REVOKE GRANT OPTION FOR"
	}
	return s.prefix.Kw(revoke, privilege).Append(s.object).Kw("FROM").Append(s.grantee).String(), nil
}

// privilegeStatements revokes current privileges that are not desired before granting desired ones that are missing,
// so a failure part-way never leaves more access than either set. Each privilege is matched against allowed again
// here, because it becomes unquoted SQL text.
func privilegeStatements(spec grantSpec, allowed []sqlclient.Keyword, current, desired []string) ([]string, error) {
	return privilegeOptionStatements(spec, allowed, privilegeSets{privileges: current}, privilegeSets{privileges: desired})
}

// privilegeSets is one side of a reconciliation: the explicit privileges and the subset held with grant option.
type privilegeSets struct {
	// privileges is the sorted explicit privilege set.
	privileges []string
	// options is the sorted subset of privileges the grantee may grant to others.
	options []string
}

// privilegeOptionStatements reconciles privileges and grant options. Removals run first, then downgrades, then
// plain grants, then upgrades, so a failure part-way never leaves more access or grant rights than either side.
// A privilege that is removed loses its grant option with it, and a new privilege that should carry the option is
// granted with it directly instead of being granted twice.
func privilegeOptionStatements(spec grantSpec, allowed []sqlclient.Keyword, current, desired privilegeSets) ([]string, error) {
	var statements []string
	for _, step := range []struct {
		grant, option bool
		from, to      []string
		skip          []string
	}{
		{grant: false, from: current.privileges, to: desired.privileges},
		{grant: false, option: true, from: current.options, to: desired.options, skip: privilegesAbsent(desired.privileges, current.privileges)},
		{grant: true, from: desired.privileges, to: current.privileges, skip: desired.options},
		{grant: true, option: true, from: desired.options, to: current.options},
	} {
		for _, privilege := range step.from {
			if slices.Contains(step.to, privilege) || slices.Contains(step.skip, privilege) {
				continue
			}
			keyword, err := sqlclient.OneOf(privilege, allowed...)
			if err != nil {
				return nil, fmt.Errorf("unsupported privilege: %w", err)
			}
			statement := spec.statement(step.grant, keyword)
			if step.option {
				if statement, err = spec.optionStatement(step.grant, keyword); err != nil {
					return nil, err
				}
			}
			statements = append(statements, statement)
		}
	}
	return statements, nil
}

// privilegesAbsent returns the values of from that are absent from in.
func privilegesAbsent(in, from []string) []string {
	var absent []string
	for _, value := range from {
		if !slices.Contains(in, value) {
			absent = append(absent, value)
		}
	}
	return absent
}

// privilegeAllowed reports whether name is exactly one of the allowlisted privilege keywords.
func privilegeAllowed(allowed []sqlclient.Keyword, name string) bool {
	return slices.ContainsFunc(allowed, func(keyword sqlclient.Keyword) bool { return string(keyword) == name })
}

// privilegeNames returns the allowlisted keywords as strings for schema validators.
func privilegeNames(allowed []sqlclient.Keyword) []string {
	names := make([]string, len(allowed))
	for i, keyword := range allowed {
		names[i] = string(keyword)
	}
	return names
}

// newCatalogCheck builds query once, so a placeholder without a binding or an empty value fails while the tuple
// is validated instead of on a later refresh.
func newCatalogCheck(query sqlclient.Query) (catalogCheck, error) {
	sql, parameters, err := query.Build()
	return catalogCheck{sql: sql, parameters: parameters}, err
}

// newCatalogChecks builds several existence checks in order.
func newCatalogChecks(queries ...sqlclient.Query) ([]catalogCheck, error) {
	checks := make([]catalogCheck, 0, len(queries))
	for _, query := range queries {
		check, err := newCatalogCheck(query)
		if err != nil {
			return nil, err
		}
		checks = append(checks, check)
	}
	return checks, nil
}

// privilegeRoleQuery confirms that a SQL role exists.
func privilegeRoleQuery(name string) sqlclient.Query {
	return sqlclient.Select("role_name").From("svv_roles").Where("role_name = :name", sqlclient.Bind("name", name))
}

// privilegeUserQuery confirms that a database user exists.
func privilegeUserQuery(name string) sqlclient.Query {
	return sqlclient.Select("usename").From("pg_user").Where("usename = :name", sqlclient.Bind("name", name))
}

// privilegeGroupQuery confirms that a user group exists.
func privilegeGroupQuery(name string) sqlclient.Query {
	return sqlclient.Select("groname").From("pg_group").Where("groname = :name", sqlclient.Bind("name", name))
}

// privilegeSchemaQuery confirms that a schema exists in database.
func privilegeSchemaQuery(database, schema string) sqlclient.Query {
	return sqlclient.Select("schema_name").From("svv_all_schemas").
		Where("database_name = :database", sqlclient.Bind("database", database)).
		Where("schema_name = :schema", sqlclient.Bind("schema", schema))
}

// principal renders a grantee and supplies its catalog existence check; PUBLIC needs none.
func principal(data types.Object) (sqlclient.Statement, catalogCheck, error) {
	name, kind := objectString(data, "grantee"), objectString(data, "grantee_type")
	var grantee sqlclient.Statement
	var query sqlclient.Query
	switch kind {
	case "ROLE":
		grantee, query = sqlclient.Kw("ROLE").Ident(name), privilegeRoleQuery(name)
	case "USER":
		grantee, query = sqlclient.Ident(name), privilegeUserQuery(name)
	case "GROUP":
		grantee, query = sqlclient.Kw("GROUP").Ident(name), privilegeGroupQuery(name)
	case "PUBLIC":
		if name != "public" {
			return grantee, catalogCheck{}, fmt.Errorf("PUBLIC requires grantee = public")
		}
		return sqlclient.Kw("PUBLIC"), catalogCheck{}, nil
	default:
		return grantee, catalogCheck{}, fmt.Errorf("unsupported grantee_type %q", kind)
	}
	check, err := newCatalogCheck(query)
	return grantee, check, err
}
