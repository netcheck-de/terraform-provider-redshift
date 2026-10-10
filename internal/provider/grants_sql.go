package provider

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// grantsObjectTypes are the objects SHOW GRANTS ON lists without a function signature.
// https://docs.aws.amazon.com/redshift/latest/dg/r_SHOW_GRANTS.html
var grantsObjectTypes = []sqlclient.Keyword{"DATABASE", "SCHEMA", "TABLE"}

// grantsFilters are the configured filters of a redshift_grants listing; empty means unset.
type grantsFilters struct {
	// database is the database whose grants are listed and where SHOW GRANTS runs.
	database string
	// objectType selects SHOW GRANTS ON DATABASE, SCHEMA, or TABLE; empty selects SHOW GRANTS FOR the grantee.
	objectType string
	// schema names the schema of SCHEMA and TABLE objects, or narrows a grantee listing.
	schema string
	// object names the table of TABLE objects, or narrows a grantee listing.
	object string
	// grantee narrows the rows to one identity name.
	grantee string
	// granteeType narrows the rows to USER, ROLE, GROUP, or PUBLIC.
	granteeType string
}

// grantsFiltersFrom reads the filters of a listing configuration, defaulting the database to the provider's.
func grantsFiltersFrom(data types.Object, admin string) grantsFilters {
	filters := grantsFilters{
		database: objectString(data, "database_name"), objectType: objectString(data, "object_type"),
		schema: objectString(data, "schema_name"), object: objectString(data, "object_name"),
		grantee: objectString(data, "grantee"), granteeType: objectString(data, "grantee_type"),
	}
	if filters.database == "" {
		filters.database = admin
	}
	return filters
}

// readGrantsStatement renders SHOW GRANTS ON the selected object, or SHOW GRANTS FOR a user or role FROM DATABASE
// when no object is selected. SHOW GRANTS FOR accepts only users and roles, so groups and PUBLIC need an object.
func readGrantsStatement(filters grantsFilters) (string, error) {
	if filters.objectType == "" {
		if filters.grantee == "" {
			return "", fmt.Errorf("set object_type, or grantee and grantee_type, to select what SHOW GRANTS lists")
		}
		var grantee sqlclient.Statement
		switch filters.granteeType {
		case "USER":
			grantee = sqlclient.Ident(filters.grantee)
		case "ROLE":
			grantee = sqlclient.Kw("ROLE").Ident(filters.grantee)
		default:
			return "", fmt.Errorf("SHOW GRANTS FOR lists only USER and ROLE grantees; set object_type to list grants of %q grantees", filters.granteeType)
		}
		return sqlclient.Stmt("SHOW GRANTS FOR").Append(grantee).KwIdent("FROM DATABASE", filters.database).String(), nil
	}
	kind, err := sqlclient.OneOf(filters.objectType, grantsObjectTypes...)
	if err != nil {
		return "", fmt.Errorf("unsupported object_type: %w", err)
	}
	parts := []string{filters.database}
	switch kind {
	case "DATABASE":
		if filters.schema != "" || filters.object != "" {
			return "", fmt.Errorf("DATABASE does not accept schema_name or object_name")
		}
	case "SCHEMA":
		if filters.schema == "" || filters.object != "" {
			return "", fmt.Errorf("SCHEMA requires schema_name and does not accept object_name")
		}
		parts = append(parts, filters.schema)
	default:
		if filters.schema == "" || filters.object == "" {
			return "", fmt.Errorf("TABLE requires schema_name and object_name")
		}
		parts = append(parts, filters.schema, filters.object)
	}
	return sqlclient.Stmt("SHOW GRANTS ON", kind).Qualified(parts...).String(), nil
}

// grantsNullable maps an absent or empty catalog column to null, because SHOW GRANTS omits columns per form.
func grantsNullable(value string) types.String {
	if value == "" {
		return types.StringNull()
	}
	return types.StringValue(value)
}

// grantsAdminOption maps the admin_option column, which SHOW GRANTS FOR does not return, to a nullable bool.
func grantsAdminOption(value string) types.Bool {
	switch strings.ToLower(value) {
	case "t", "true":
		return types.BoolValue(true)
	case "f", "false":
		return types.BoolValue(false)
	default:
		return types.BoolNull()
	}
}

// grantsItems converts SHOW GRANTS rows into listing elements, applying the filters the statement cannot express,
// and sorts them so the listing does not depend on catalog order.
func grantsItems(filters grantsFilters, rows []sqlclient.Row) []map[string]attr.Value {
	type keyed struct {
		key  []string
		item map[string]attr.Value
	}
	var items []keyed
	for _, row := range rows {
		granteeType := strings.ToUpper(row["identity_type"])
		if (filters.grantee != "" && row["identity_name"] != filters.grantee) || (filters.granteeType != "" && granteeType != filters.granteeType) {
			continue
		}
		// Object listings already select their object; only grantee listings narrow by schema and object.
		if filters.objectType == "" && ((filters.schema != "" && row["schema_name"] != filters.schema) || (filters.object != "" && row["object_name"] != filters.object)) {
			continue
		}
		database, objectType := cmp.Or(row["database_name"], filters.database), cmp.Or(row["object_type"], filters.objectType)
		privilege := normalizePrivilege(row["privilege_type"])
		items = append(items, keyed{
			key: []string{database, row["schema_name"], row["object_name"], objectType, granteeType, row["identity_name"], row["privilege_scope"], privilege},
			item: map[string]attr.Value{
				"database_name": types.StringValue(database), "schema_name": grantsNullable(row["schema_name"]),
				"object_name": grantsNullable(row["object_name"]), "object_type": grantsNullable(objectType),
				"privilege": types.StringValue(privilege), "privilege_scope": grantsNullable(row["privilege_scope"]),
				"grantee": types.StringValue(row["identity_name"]), "grantee_type": types.StringValue(granteeType),
				"admin_option": grantsAdminOption(row["admin_option"]), "grantor": grantsNullable(row["grantor_name"]),
			},
		})
	}
	slices.SortStableFunc(items, func(a, b keyed) int { return slices.Compare(a.key, b.key) })
	result := make([]map[string]attr.Value, 0, len(items))
	for _, item := range items {
		result = append(result, item.item)
	}
	return result
}

// readColumnGrantsQuery lists explicit column privileges in the current database, narrowed by the optional filters.
// https://docs.aws.amazon.com/redshift/latest/dg/r_SVV_COLUMN_PRIVILEGES.html
func readColumnGrantsQuery(filters grantsFilters) sqlclient.Query {
	return sqlclient.Select("namespace_name", "relation_name", "column_name", "privilege_type", "identity_name", "identity_type").
		From("svv_column_privileges").
		OptEq("namespace_name", "schema", filters.schema).
		OptEq("relation_name", "object", filters.object).
		OptEq("identity_name", "grantee", filters.grantee).
		OptEq("identity_type", "identity_type", columnGrantIdentityType(filters.granteeType)).
		OrderBy("namespace_name", "relation_name", "column_name", "privilege_type", "identity_type", "identity_name")
}

// columnGrantsItems converts SVV_COLUMN_PRIVILEGES rows into listing elements of the selected database.
func columnGrantsItems(database string, rows []sqlclient.Row) []map[string]attr.Value {
	items := make([]map[string]attr.Value, 0, len(rows))
	for _, row := range rows {
		items = append(items, map[string]attr.Value{
			"database_name": types.StringValue(database), "schema_name": types.StringValue(row["namespace_name"]),
			"object_name": types.StringValue(row["relation_name"]), "column_name": types.StringValue(row["column_name"]),
			"privilege": types.StringValue(normalizePrivilege(row["privilege_type"])),
			"grantee":   types.StringValue(row["identity_name"]), "grantee_type": types.StringValue(strings.ToUpper(row["identity_type"])),
		})
	}
	return items
}
