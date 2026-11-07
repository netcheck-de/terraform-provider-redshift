package provider

import (
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// listGrantsStatement lists every identity's grants on the database, or on one of its schemas. Both forms report
// scoped grants with their privilege_scope and admin_option, which SHOW GRANTS FOR omits.
// https://docs.aws.amazon.com/redshift/latest/dg/r_SHOW_GRANTS.html
func listGrantsStatement(database, schema string) string {
	if schema != "" {
		return sqlclient.Stmt("SHOW GRANTS ON SCHEMA").Qualified(database, schema).String()
	}

	return sqlclient.Stmt("SHOW GRANTS ON DATABASE").Ident(database).String()
}

// grantsRecipient builds the redshift_grant tuple a SHOW GRANTS row belongs to, with the identity name its rows
// carry, or reports false for rows of other identity types. Datashares appear as ds:<name>.
func grantsRecipient(database, schema string, row sqlclient.Row) (grantModel, string, bool) {
	data := grantModel{
		DatabaseName: types.StringValue(database), SchemaName: grantsNullable(schema), Scope: types.StringValue(row["privilege_scope"]),
		Role: types.StringNull(), User: types.StringNull(), Datashare: types.StringNull(),
	}
	name, kind := row["identity_name"], strings.ToLower(row["identity_type"])

	switch share, datashare := strings.CutPrefix(name, "ds:"); {
	case kind == "role":
		data.Role = types.StringValue(name)
	case kind == "user":
		data.User = types.StringValue(name)
	case datashare && share != "" && kind != "group" && kind != "public":
		data.Datashare = types.StringValue(share)
	default:
		return data, "", false
	}

	return data, name, true
}

// grantsItems folds SHOW GRANTS rows into one element per recipient and scope, keeping only the rows the matching
// redshift_grant tuple reads: valid scopes for the database or schema, as grantRowMatches selects them, narrowed by
// the role and user filters.
func grantsItems(database, schema, role, user string, rows []sqlclient.Row) []map[string]attr.Value {
	models := map[string]grantModel{}
	groups := groupPrivilegeRows(rows, func(row sqlclient.Row) []string {
		data, identity, ok := grantsRecipient(database, schema, row)
		if !ok || !slices.Contains(grantScopes, data.Scope.ValueString()) || data.validateTuple() != nil || !data.grantRowMatches(row, identity) {
			return nil
		}

		if (role != "" && data.Role.ValueString() != role) || (user != "" && data.User.ValueString() != user) {
			return nil
		}

		kind, name, _ := data.recipient()
		key := []string{data.Scope.ValueString(), kind, name}
		models[strings.Join(key, "\x00")] = data

		return key
	})

	items := make([]map[string]attr.Value, 0, len(groups))
	for _, group := range groups {
		data := models[strings.Join(group.key, "\x00")]
		items = append(items, map[string]attr.Value{
			"database_name": data.DatabaseName, "schema_name": data.SchemaName, "scope": data.Scope,
			"role": data.Role, "user": data.User, "datashare": data.Datashare,
			"privileges": group.fold.privilegeSet(), "grant_option_privileges": group.fold.optionSet(),
		})
	}

	return items
}
