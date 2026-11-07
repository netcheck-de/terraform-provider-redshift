package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

var _ = registerDataSource(newUsersDataSource)

// userInfoHidden describes an SVV_USER_INFO option a regular user sees only for itself.
const userInfoHidden = " Null when `SVV_USER_INFO` does not show the user, as for other users when a regular user reads the listing."

// usersOutputDescriptions describe the listed user attributes as the lookup reports them.
var usersOutputDescriptions = map[string]string{
	"name":             "Database user name.",
	"superuser":        "Whether the user has CREATEUSER.",
	"create_database":  "Whether the user has CREATEDB.",
	"valid_until":      "Password expiration as an RFC 3339 timestamp; null when the password does not expire.",
	"connection_limit": "Maximum concurrent connections; `-1` stands for `UNLIMITED`." + userInfoHidden,
	"session_timeout":  "Idle-session timeout in seconds; `0` when the user has none." + userInfoHidden,
	"syslog_access":    "`RESTRICTED` or `UNRESTRICTED`." + userInfoHidden,
	"external_id":      "Identity-provider user identifier; null when none is set or `SVV_USER_INFO` does not show the user.",
	"search_path":      "Stored `search_path` default in search order; null when none is stored.",
	"session_defaults": "Stored defaults of the session parameters `redshift_user` manages in `session_defaults`; null when none is stored.",
}

// newUsersDataSource lists every database user from pg_user with the SVV_USER_INFO options the caller may see.
func newUsersDataSource() datasource.DataSource {
	return newCollectionDataSource(collectionSpec{
		name: "users",
		description: "Lists database users from `pg_user`, which every user can read in full, with the options of `SVV_USER_INFO`. " +
			"Superusers see those options for every user; regular users see them only for themselves, so they are null for other users. " +
			"Passwords and whether a password is disabled are not reported.",
		element: describedOutputs(collectionElement(newUserResource, "name", "superuser", "create_database", "valid_until",
			"connection_limit", "session_timeout", "syslog_access", "external_id", "search_path", "session_defaults"), usersOutputDescriptions),
		list: func(ctx context.Context, client *resourceClient, _ types.Object) ([]map[string]attr.Value, error) {
			database := client.database.ValueString()

			rows, err := client.selectRows(ctx, database, listUsersQuery())
			if err != nil {
				return nil, err
			}

			infoRows, err := client.selectRows(ctx, database, listUserInfoQuery())
			if err != nil {
				return nil, err
			}

			info := map[string][]sqlclient.Row{}
			for _, row := range infoRows {
				info[row["user_name"]] = append(info[row["user_name"]], row)
			}

			items := make([]map[string]attr.Value, 0, len(rows))
			for _, row := range rows {
				user := userNullModel(types.StringNull())
				if err := userCatalog(&user, row); err != nil {
					return nil, err
				}

				if err := userInfo(&user, info[row["usename"]]); err != nil {
					return nil, err
				}

				items = append(items, map[string]attr.Value{
					"name": user.Name, "superuser": user.Superuser, "create_database": user.CreateDB,
					"valid_until": userLookupValidUntil(user.ValidUntil), "connection_limit": user.ConnectionLimit,
					"session_timeout": user.SessionTimeout, "syslog_access": user.SyslogAccess, "external_id": user.ExternalID,
					"search_path": user.SearchPath, "session_defaults": user.SessionDefaults,
				})
			}

			return items, nil
		},
	})
}
