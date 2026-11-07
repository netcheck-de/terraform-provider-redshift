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
		"database_name": schema.StringAttribute{Optional: true, MarkdownDescription: "Local database whose grants are listed; defaults to the provider database.", Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
		"schema_name":   schema.StringAttribute{Optional: true, MarkdownDescription: "Only grants on objects in this schema.", Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
		"object_name":   schema.StringAttribute{Optional: true, MarkdownDescription: "Only grants on this table or view.", Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
		"grantee":       schema.StringAttribute{Optional: true, MarkdownDescription: "Only grants to this identity name; `public` for `PUBLIC`.", Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
		"grantee_type":  schema.StringAttribute{Optional: true, MarkdownDescription: "Only grants to `USER`, `ROLE`, `GROUP`, or `PUBLIC` identities.", Validators: []validator.String{stringvalidator.OneOf(grantGranteeTypes...)}},
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
	filters["object_name"] = schema.StringAttribute{Optional: true, MarkdownDescription: "Table, view, or template whose grants are listed with `object_type`; otherwise only grants on objects of this name.", Validators: []validator.String{stringvalidator.LengthAtLeast(1)}}
	filters["object_type"] = schema.StringAttribute{
		Optional: true, Validators: []validator.String{stringvalidator.OneOf(privilegeNames(objectGrantsObjectTypes)...)},
		MarkdownDescription: "`DATABASE`, `SCHEMA`, `TABLE` (including views), or `TEMPLATE` lists `SHOW GRANTS ON` that object, named by `database_name`, `schema_name`, and `object_name`. " +
			"Omit it to list `SHOW GRANTS FOR` the `USER` or `ROLE` named by `grantee` and `grantee_type`. One of the two forms is required.",
	}

	return newCollectionDataSource(collectionSpec{
		name:          "object_grants",
		databaseField: "database_name",
		description: "Lists the explicit object grants `SHOW GRANTS` reports on one database, schema, table, or template, or for one user or role in one database, one element per object and grantee, without taking ownership. " +
			"Elements are the `DATABASE`, `SCHEMA`, `TABLE`, and `TEMPLATE` grants `redshift_object_grant` manages; scoped grants (see `redshift_grants`), routine and model grants, `ALL …` snapshots, and identities other than users, roles, groups, and `PUBLIC` are left out. " +
			"`SHOW GRANTS` shows superusers every grant and other users only the grants visible to them, and documents no column for grants inherited through roles, so none is exposed.",
		filters: filters,
		element: describedOutputs(collectionElement(newObjectGrantResource, "database_name", "schema_name", "object_type", "object_name", "grantee", "grantee_type", "privileges", "grant_option_privileges"), objectGrantsOutputDescriptions),
		list: func(ctx context.Context, client *resourceClient, data types.Object) ([]map[string]attr.Value, error) {
			filters := objectGrantsFiltersFrom(data, client.database.ValueString())

			statement, err := readObjectGrantsStatement(filters)
			if err != nil {
				return nil, err
			}

			rows, err := client.queryDatabase(ctx, filters.database, statement, nil)
			if err != nil {
				return nil, err
			}

			return objectGrantsItems(filters, rows), nil
		},
	})
}
