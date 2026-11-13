package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newObjectGrantsDataSource)

// grantsGranteeFilters are the database, relation, and grantee filters shared by the object and column grant listings.
func grantsGranteeFilters() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"database_name": discoveryDatabaseFilter("grants"),
		"schema_name":   discoveryFilter("Only grants on objects in this schema."),
		"object_name":   discoveryFilter("Only grants on this table or view."),
		"grantee":       discoveryFilter("Only grants to this identity name; `public` for `PUBLIC`."),
		"grantee_type":  discoveryGranteeTypeFilter(),
	}
}

// grantsString is a computed element attribute.
func grantsString(description string) schema.StringAttribute {
	return schema.StringAttribute{Computed: true, MarkdownDescription: description}
}

// objectGrantsOutputDescriptions describe listed values where the resource's text explains configuration.
var objectGrantsOutputDescriptions = map[string]string{
	"database_name":           "Database of the granted object.",
	"schema_name":             "Schema of the granted object; null for `DATABASE`.",
	"object_name":             "Table, view, or template name; null for `DATABASE` and `SCHEMA`.",
	"object_type":             "`DATABASE`, `SCHEMA`, `TABLE` (including views), or `TEMPLATE`.",
	"grantee":                 "Receiving identity name; `public` for `PUBLIC`.",
	"grantee_type":            "`USER`, `ROLE`, `GROUP`, or `PUBLIC`.",
	"privileges":              "Explicit privileges the grantee holds on the object, with `TEMP` reported as `TEMPORARY`.",
	"grant_option_privileges": "Subset of `privileges` the grantee holds `WITH GRANT OPTION`; null for grantee listings, because `SHOW GRANTS FOR` does not report grant options.",
}

// newObjectGrantsDataSource lists the object grants SHOW GRANTS reports on one object or for one user or role.
func newObjectGrantsDataSource() datasource.DataSource {
	filters := grantsGranteeFilters()
	filters["object_name"] = discoveryFilter("Table, view, or template whose grants are listed with `object_type`; otherwise only grants on objects of this name.")
	filters["object_type"] = schema.StringAttribute{
		Optional: true, Validators: []validator.String{stringvalidator.LengthAtLeast(1), stringvalidator.OneOf(privilegeNames(objectGrantsObjectTypes)...)},
		MarkdownDescription: "`DATABASE`, `SCHEMA`, `TABLE` (including views), or `TEMPLATE` lists `SHOW GRANTS ON` that object, named by `database_name`, `schema_name`, and `object_name`. " +
			"Omit it to list `SHOW GRANTS FOR` the `USER` or `ROLE` named by `grantee` and `grantee_type`. One of the two forms is required.",
	}

	return newCollectionDataSource(collectionSpec{
		name:          "object_grants",
		databaseField: "database_name",
		description: "Lists the explicit object grants `SHOW GRANTS` reports on one database, schema, table, or template, or for one user or role in one database, one element per object and grantee, without taking ownership. " +
			"Elements are the `DATABASE`, `SCHEMA`, `TABLE`, and `TEMPLATE` grants `redshift_object_grant` manages; scoped grants (see `redshift_grants`), routine and model grants, `ALL …` snapshots, and identities other than users, roles, groups, and `PUBLIC` are left out. " +
			"Superusers see every grant; other users see only the grants `SHOW GRANTS` shows them. `SHOW GRANTS` documents no column for grants inherited through roles, so none is exposed. " + discoverySharedScope,
		filters: filters,
		element: describedOutputs(collectionElement(newObjectGrantResource, "database_name", "schema_name", "object_type", "object_name", "grantee", "grantee_type", "privileges", "grant_option_privileges"), objectGrantsOutputDescriptions),
		list: func(ctx context.Context, client *resourceClient, data types.Object) ([]map[string]attr.Value, error) {
			filters := objectGrantsFiltersFrom(data, client.database.ValueString())

			statement, err := readObjectGrantsStatement(filters)
			if err != nil {
				return nil, err
			}

			// Local grants are read in their database and shared ones from admin, as redshift_object_grant reads them;
			// the statement qualifies every name with the database.
			target, exists, err := discoveryGrantTarget(ctx, client, filters.database)
			if err != nil || !exists {
				return []map[string]attr.Value{}, err
			}

			rows, err := client.queryDatabase(ctx, target, statement, nil)
			if err != nil {
				return nil, err
			}

			return objectGrantsItems(filters, rows), nil
		},
	})
}
