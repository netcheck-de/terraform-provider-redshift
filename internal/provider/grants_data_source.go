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

var _ = registerDataSource(newGrantsDataSource)

// grantsGranteeFilters are the grantee filters shared by both grant listings.
func grantsGranteeFilters() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"database_name": schema.StringAttribute{Optional: true, MarkdownDescription: "Local database whose grants are listed; defaults to the provider database.", Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
		"schema_name":   schema.StringAttribute{Optional: true, MarkdownDescription: "Only grants on objects in this schema.", Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
		"object_name":   schema.StringAttribute{Optional: true, MarkdownDescription: "Only grants on this table or view.", Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
		"grantee":       schema.StringAttribute{Optional: true, MarkdownDescription: "Only grants to this identity name; `public` for `PUBLIC`.", Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
		"grantee_type":  schema.StringAttribute{Optional: true, MarkdownDescription: "Only grants to `USER`, `ROLE`, `GROUP`, or `PUBLIC` identities.", Validators: []validator.String{stringvalidator.OneOf("USER", "ROLE", "GROUP", "PUBLIC")}},
	}
}

// grantsString is a computed element attribute.
func grantsString(description string) schema.StringAttribute {
	return schema.StringAttribute{Computed: true, MarkdownDescription: description}
}

// newGrantsDataSource lists the grants SHOW GRANTS reports on one object or for one user or role.
func newGrantsDataSource() datasource.DataSource {
	filters := grantsGranteeFilters()
	filters["object_type"] = schema.StringAttribute{
		Optional: true, Validators: []validator.String{stringvalidator.OneOf(privilegeNames(grantsObjectTypes)...)},
		MarkdownDescription: "`DATABASE`, `SCHEMA`, or `TABLE` (including views) lists `SHOW GRANTS ON` that object, named by `database_name`, `schema_name`, and `object_name`. " +
			"Omit it to list `SHOW GRANTS FOR` the `USER` or `ROLE` named by `grantee` and `grantee_type`.",
	}
	return newCollectionDataSource(collectionSpec{
		name: "grants",
		description: "Lists the grants `SHOW GRANTS` reports on one database, schema, or table, or for one user or role in one database, without taking ownership. " +
			"Rows include scoped (`privilege_scope`) grants; `SHOW GRANTS` documents no column for grants inherited through roles, so none is exposed.",
		filters: filters,
		element: map[string]schema.Attribute{
			"database_name":   grantsString("Database of the granted object."),
			"schema_name":     grantsString("Schema of the granted object; null for database grants."),
			"object_name":     grantsString("Granted object name as SHOW GRANTS reports it; null when not reported."),
			"object_type":     grantsString("`DATABASE`, `SCHEMA`, `TABLE`, or another object type SHOW GRANTS reports."),
			"privilege":       grantsString("Privilege name, with `TEMP` reported as `TEMPORARY` and `EXFUNC` as `EXTERNAL FUNCTION`."),
			"privilege_scope": grantsString("Scope of the grant, such as `TABLE`, `SCHEMA`, `DATABASE`, or `TABLES` for a scoped grant on all tables."),
			"grantee":         grantsString("Receiving identity name; `public` for `PUBLIC`."),
			"grantee_type":    grantsString("`USER`, `ROLE`, `GROUP`, `PUBLIC`, or another identity type SHOW GRANTS reports, in upper case."),
			"admin_option":    schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether the grantee holds the grant option; null when `SHOW GRANTS FOR` does not report it."},
			"grantor":         grantsString("Identity that granted the privilege; null when not reported."),
		},
		list: func(ctx context.Context, client *resourceClient, data types.Object) ([]map[string]attr.Value, error) {
			filters := grantsFiltersFrom(data, client.database.ValueString())
			statement, err := readGrantsStatement(filters)
			if err != nil {
				return nil, err
			}
			rows, err := client.queryDatabase(ctx, filters.database, statement, nil)
			if err != nil {
				return nil, err
			}
			return grantsItems(filters, rows), nil
		},
	})
}
