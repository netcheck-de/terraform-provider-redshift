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

// objectGrantsObjectTypes are the objects SHOW GRANTS ON lists without a function signature. They are also the
// object types redshift_object_grant reads through SHOW GRANTS, so only they are listed.
// https://docs.aws.amazon.com/redshift/latest/dg/r_SHOW_GRANTS.html
var objectGrantsObjectTypes = []sqlclient.Keyword{"DATABASE", "SCHEMA", "TABLE", "TEMPLATE"}

// grantGranteeTypes are the SQL identity types object, column, language, and default privilege grants accept.
var grantGranteeTypes = []string{"USER", "ROLE", "GROUP", "PUBLIC"}

// objectGrantsFilters are the configured filters of a redshift_object_grants listing; empty means unset.
type objectGrantsFilters struct {
	// database is the database whose grants are listed and where SHOW GRANTS runs.
	database string
	// objectType selects SHOW GRANTS ON DATABASE, SCHEMA, TABLE, or TEMPLATE; empty selects SHOW GRANTS FOR the
	// grantee.
	objectType string
	// schema names the schema of SCHEMA, TABLE, and TEMPLATE objects, or narrows a grantee listing.
	schema string
	// object names the table or template, or narrows a grantee listing.
	object string
	// grantee narrows the rows to one identity name.
	grantee string
	// granteeType narrows the rows to USER, ROLE, GROUP, or PUBLIC.
	granteeType string
}

// objectGrantsFiltersFrom reads the filters of a listing configuration, defaulting the database to the provider's.
func objectGrantsFiltersFrom(data types.Object, admin string) objectGrantsFilters {
	filters := objectGrantsFilters{
		database: objectString(data, "database_name"), objectType: objectString(data, "object_type"),
		schema: objectString(data, "schema_name"), object: objectString(data, "object_name"),
		grantee: objectString(data, "grantee"), granteeType: objectString(data, "grantee_type"),
	}
	if filters.database == "" {
		filters.database = admin
	}

	return filters
}

// readObjectGrantsStatement renders SHOW GRANTS ON the selected object, or SHOW GRANTS FOR a user or role FROM
// DATABASE when no object is selected. SHOW GRANTS FOR accepts only users and roles, so groups and PUBLIC need an
// object.
func readObjectGrantsStatement(filters objectGrantsFilters) (string, error) {
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

	kind, err := sqlclient.OneOf(filters.objectType, objectGrantsObjectTypes...)
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
			return "", fmt.Errorf("%s requires schema_name and object_name", kind)
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

// objectGrantsKey identifies the object and grantee of a SHOW GRANTS row as redshift_object_grant does, or returns
// nil for rows the listing skips: scoped grants, whose privilege_scope differs from the object type, object and
// identity types the resource does not read through SHOW GRANTS, and rows outside the filters the statement cannot
// express.
func objectGrantsKey(filters objectGrantsFilters, row sqlclient.Row) []string {
	kind := strings.ToUpper(filters.objectType)
	// SHOW GRANTS ON DATABASE has no object_type column; the requested kind fills it.
	objectType, granteeType := strings.ToUpper(cmp.Or(row["object_type"], kind)), strings.ToUpper(row["identity_type"])

	switch {
	case !slices.Contains(privilegeNames(objectGrantsObjectTypes), objectType) || row["privilege_scope"] != objectType:
		return nil
	case !slices.Contains(grantGranteeTypes, granteeType):
		return nil
	case (filters.grantee != "" && row["identity_name"] != filters.grantee) || (filters.granteeType != "" && granteeType != filters.granteeType):
		return nil
	// Object listings already select their object; only grantee listings narrow by schema and object.
	case kind == "" && ((filters.schema != "" && row["schema_name"] != filters.schema) || (filters.object != "" && row["object_name"] != filters.object)):
		return nil
	}

	schemaName, objectName := row["schema_name"], row["object_name"]
	if objectType == "DATABASE" {
		schemaName = ""
	}
	// SHOW GRANTS ON SCHEMA reports the schema as object_name; only tables and templates have a name of their own.
	if objectType == "DATABASE" || objectType == "SCHEMA" {
		objectName = ""
	}

	return []string{cmp.Or(row["database_name"], filters.database), schemaName, objectName, objectType, granteeType, row["identity_name"]}
}

// objectGrantsItems folds SHOW GRANTS rows into one element per object and grantee. SHOW GRANTS FOR reports no
// admin_option, so grantee listings leave grant_option_privileges null.
func objectGrantsItems(filters objectGrantsFilters, rows []sqlclient.Row) []map[string]attr.Value {
	groups := groupPrivilegeRows(rows, func(row sqlclient.Row) []string { return objectGrantsKey(filters, row) })

	items := make([]map[string]attr.Value, 0, len(groups))
	for _, group := range groups {
		options := types.SetNull(types.StringType)
		if filters.objectType != "" {
			options = group.fold.optionSet()
		}

		items = append(items, map[string]attr.Value{
			"database_name": types.StringValue(group.key[0]), "schema_name": grantsNullable(group.key[1]),
			"object_name": grantsNullable(group.key[2]), "object_type": types.StringValue(group.key[3]),
			"grantee_type": types.StringValue(group.key[4]), "grantee": types.StringValue(group.key[5]),
			"privileges": group.fold.privilegeSet(), "grant_option_privileges": options,
		})
	}

	return items
}
