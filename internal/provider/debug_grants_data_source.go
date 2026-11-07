package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

var _ = registerDataSource(newDebugGrantsDataSource)

// newDebugGrantsDataSource lists the DEBUG grants SHOW GRANTS reports on one database with federated permissions.
func newDebugGrantsDataSource() datasource.DataSource {
	return newCollectionDataSource(collectionSpec{
		name:          "debug_grants",
		databaseField: "database",
		description: "Lists the `DEBUG` grants `SHOW GRANTS ON DATABASE` reports on one database of a warehouse with federated permissions, one element per grantee, without taking ownership. " +
			"Only producer-local database names are accepted: for a remote `database@catalog` alias, `SHOW GRANTS ON` can silently omit existing grants, so use the `redshift_debug_grant` lookup there. " +
			"`SHOW GRANTS` shows superusers every grant and other users only the grants visible to them.",
		filters: map[string]schema.Attribute{
			"database": schema.StringAttribute{Optional: true, MarkdownDescription: "Database whose `DEBUG` grants are listed; defaults to the provider database.", Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
		},
		element: describedOutputs(collectionElement(newDebugGrantResource, "database", "grantee", "grantee_type"), map[string]string{
			"database":     "Database the `DEBUG` grant applies to.",
			"grantee":      "Global identity `<prefix>:<name>` for `USER`, Identity Center group `<prefix>:<group>` for `ROLE`, or 12-digit AWS account ID for `ACCOUNT`, as `SHOW GRANTS` reports it.",
			"grantee_type": "`USER` for a global identity, `ROLE` for an Identity Center group, or `ACCOUNT` for the administrators of an account.",
		}),
		list: func(ctx context.Context, client *resourceClient, data types.Object) ([]map[string]attr.Value, error) {
			database := discoveryDatabase(client, data)
			if strings.Contains(database, "@") {
				return nil, fmt.Errorf("SHOW GRANTS ON can omit DEBUG grants of the remote database %q; list a producer-local database or use the redshift_debug_grant lookup", database)
			}

			// The statement names the database, so it runs in admin, like the resource's read.
			rows, err := client.query(ctx, readDebugGrantStatement(database), nil)
			if err != nil {
				return nil, err
			}

			return debugGrantsItems(database, rows), nil
		},
	})
}

// debugGrantsItems converts the DEBUG rows of SHOW GRANTS into one element per grantee, sorted by type and name.
func debugGrantsItems(database string, rows []sqlclient.Row) []map[string]attr.Value {
	groups := groupPrivilegeRows(rows, func(row sqlclient.Row) []string {
		if !strings.EqualFold(row["privilege_type"], "DEBUG") {
			return nil
		}

		for _, kind := range debugGranteeTypeNames {
			if strings.EqualFold(row["identity_type"], debugGranteeTypes[string(kind)]) {
				return []string{string(kind), row["identity_name"]}
			}
		}

		return nil
	})

	items := make([]map[string]attr.Value, 0, len(groups))
	for _, group := range groups {
		items = append(items, map[string]attr.Value{
			"database": types.StringValue(database), "grantee_type": types.StringValue(group.key[0]), "grantee": types.StringValue(group.key[1]),
		})
	}

	return items
}
