package provider

import (
	"context"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

var _ = registerDataSource(newLanguageGrantsDataSource)

// newLanguageGrantsDataSource lists the explicit SQL and PLPGSQL language privileges of one database.
func newLanguageGrantsDataSource() datasource.DataSource {
	return newCollectionDataSource(collectionSpec{
		name:          "language_grants",
		databaseField: "database_name",
		description: "Lists explicit `USAGE` grants on the `SQL` and `PLPGSQL` languages of one local database from `SVV_LANGUAGE_PRIVILEGES`, one element per language and grantee, without taking ownership. " +
			"The view lists only explicit grants, so `PUBLIC`'s built-in `USAGE` may have no element. " +
			"Superusers and users with the `ACCESS SYSTEM TABLE` permission see every grant; other users see only the identities they have access to or own. " + discoveryLocalScope,
		filters: map[string]schema.Attribute{
			"database_name": discoveryDatabaseFilter("language grants"),
			"grantee":       discoveryFilter("Only grants to this identity name; `public` for `PUBLIC`."),
			"grantee_type":  discoveryGranteeTypeFilter(),
		},
		element: describedOutputs(collectionElement(newLanguageGrantResource, "database_name", "language_name", "grantee", "grantee_type", "privileges", "grant_option_privileges"), map[string]string{
			"database_name":           "Database whose language privileges are listed.",
			"language_name":           "`SQL` or `PLPGSQL`.",
			"grantee":                 "Receiving identity name; `public` for `PUBLIC`.",
			"grantee_type":            "`USER`, `ROLE`, `GROUP`, or `PUBLIC`.",
			"privileges":              "Explicit privileges on the language: `USAGE`.",
			"grant_option_privileges": languageGrantOptionLookupDescription,
		}),
		list: func(ctx context.Context, client *resourceClient, data types.Object) ([]map[string]attr.Value, error) {
			database, local, err := discoveryLocalDatabase(ctx, client, data, "database_name")
			if err != nil || !local {
				return []map[string]attr.Value{}, err
			}

			selectedType := objectString(data, "grantee_type")

			rows, err := client.selectRows(ctx, database, listLanguageGrantsQuery(objectString(data, "grantee")))
			if err != nil {
				return nil, err
			}

			groups := groupPrivilegeRows(rows, func(row sqlclient.Row) []string {
				granteeType := strings.ToUpper(row["identity_type"])
				if !slices.Contains(grantGranteeTypes, granteeType) || (selectedType != "" && granteeType != selectedType) {
					return nil
				}

				return []string{strings.ToUpper(row["language_name"]), granteeType, row["identity_name"]}
			})

			items := make([]map[string]attr.Value, 0, len(groups))
			for _, group := range groups {
				items = append(items, map[string]attr.Value{
					"database_name": types.StringValue(database), "language_name": types.StringValue(group.key[0]),
					"grantee_type": types.StringValue(group.key[1]), "grantee": types.StringValue(group.key[2]),
					"privileges": group.fold.privilegeSet(), "grant_option_privileges": group.fold.optionSet(),
				})
			}

			return items, nil
		},
	})
}
