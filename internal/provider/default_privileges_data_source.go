package provider

import (
	"context"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

var _ = registerDataSource(newDefaultPrivilegesDataSource)

// newDefaultPrivilegesDataSource lists the default privileges of one database, one element per rule.
func newDefaultPrivilegesDataSource() datasource.DataSource {
	return newCollectionDataSource(collectionSpec{
		name:          "default_privileges",
		databaseField: "database_name",
		description: "Lists the default privileges of one local database from `SVV_DEFAULT_PRIVILEGES`, one element per owner, schema, object type, and grantee, without taking ownership. " +
			"Redshift grants `EXECUTE` on new functions to `PUBLIC` without being asked, until the owner's first `ALTER DEFAULT PRIVILEGES` on functions; " +
			"that implicit rule is read from `PG_DEFAULT_ACL` and listed as a database-wide `PUBLIC` `FUNCTIONS` element for every such user. " +
			"Superusers and users with the `ACCESS SYSTEM TABLE` permission see every rule; other users see only the defaults granted to them.",
		filters: map[string]schema.Attribute{
			"database_name": schema.StringAttribute{Optional: true, MarkdownDescription: "Local database whose default privileges are listed; defaults to the provider database.", Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
			"owner":         schema.StringAttribute{Optional: true, MarkdownDescription: "Only the defaults of objects this user creates.", Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
			"schema_name":   schema.StringAttribute{Optional: true, MarkdownDescription: "Only the defaults of this schema; database-wide defaults are left out.", Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
			"grantee":       schema.StringAttribute{Optional: true, MarkdownDescription: "Only defaults granted to this identity name; `public` for `PUBLIC`.", Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
		},
		element: describedOutputs(collectionElement(newDefaultPrivilegeResource, "database_name", "owner", "schema_name", "object_type", "grantee", "grantee_type", "privileges", "grant_option_privileges"), map[string]string{
			"database_name":           "Database whose default privileges are listed.",
			"owner":                   "User whose future objects receive the privileges.",
			"schema_name":             "Schema whose future objects receive the privileges; null for database-wide defaults.",
			"object_type":             "`TABLES` (tables and views), `FUNCTIONS`, or `PROCEDURES`.",
			"grantee":                 "Receiving identity name; `public` for `PUBLIC`.",
			"grantee_type":            "`USER`, `ROLE`, `GROUP`, or `PUBLIC`.",
			"privileges":              "Privileges new objects of the owner grant to the grantee.",
			"grant_option_privileges": "Subset of `privileges` the grantee receives `WITH GRANT OPTION`; only users hold grant options.",
		}),
		list: func(ctx context.Context, client *resourceClient, data types.Object) ([]map[string]attr.Value, error) {
			database := objectString(data, "database_name")
			if database == "" {
				database = client.database.ValueString()
			}

			rows, err := client.selectRows(ctx, database, listDefaultPrivilegesQuery(objectString(data, "owner"), objectString(data, "schema_name"), objectString(data, "grantee")))
			if err != nil {
				return nil, err
			}

			return defaultPrivilegesItems(database, rows), nil
		},
	})
}

// defaultPrivilegesItems folds SVV_DEFAULT_PRIVILEGES rows into one element per default privilege rule, skipping
// object and grantee types the resource does not manage.
func defaultPrivilegesItems(database string, rows []sqlclient.Row) []map[string]attr.Value {
	groups := groupPrivilegeRows(rows, func(row sqlclient.Row) []string {
		objectType, granteeType := defaultPrivilegeObjectTypes[strings.ToUpper(row["object_type"])], strings.ToUpper(row["grantee_type"])
		if objectType == "" || !slices.Contains(grantGranteeTypes, granteeType) {
			return nil
		}

		return []string{row["owner_name"], row["schema_name"], objectType, granteeType, row["grantee_name"]}
	})

	items := make([]map[string]attr.Value, 0, len(groups))
	for _, group := range groups {
		items = append(items, map[string]attr.Value{
			"database_name": types.StringValue(database), "owner": types.StringValue(group.key[0]),
			"schema_name": grantsNullable(group.key[1]), "object_type": types.StringValue(group.key[2]),
			"grantee_type": types.StringValue(group.key[3]), "grantee": types.StringValue(group.key[4]),
			"privileges": group.fold.privilegeSet(), "grant_option_privileges": group.fold.optionSet(),
		})
	}

	return items
}
