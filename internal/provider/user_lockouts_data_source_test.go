package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newUserLockoutsDataSource, collection: true, filters: []string{"user_name"}})

// TestUserLockoutsSQL pins SHOW USER LOCKOUT, which has no filter clause.
func TestUserLockoutsSQL(t *testing.T) {
	checkSQL(t, "user_lockouts", []sqlCase{{"all", readUserLockoutsStatement}})
}

// userLockoutRows are the rows of the SHOW USER LOCKOUT example, out of name order.
func userLockoutRows() []sqlclient.Row {
	return []sqlclient.Row{
		{"user_name": "sales_user", "is_locked": "true", "lock_reason": "manual", "locked_at": "2026-08-10 09:15:44", "last_failed_at": "", "failed_login_attempts": "0", "locked_by": "100"},
		{"user_name": "data_analyst", "is_locked": "t", "lock_reason": "automatic", "locked_at": "2026-08-10 14:02:11", "last_failed_at": "2026-08-10 14:02:11", "failed_login_attempts": "5", "locked_by": ""},
		{"user_name": "report_user", "is_locked": "false", "lock_reason": "", "locked_at": "", "last_failed_at": "2026-08-09 22:31:07", "failed_login_attempts": "2", "locked_by": ""},
	}
}

// TestUserLockoutsList checks the unfiltered and filtered listings, null columns, identities, and read-only SQL.
func TestUserLockoutsList(t *testing.T) {
	for _, test := range []struct {
		name    string
		filters map[string]string
		users   []string
	}{
		{"all", nil, []string{"data_analyst", "report_user", "sales_user"}},
		{"one", map[string]string{"user_name": "sales_user"}, []string{"sales_user"}},
		{"none", map[string]string{"user_name": "sales"}, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			var statements []string

			source := newUserLockoutsDataSource()
			state, diagnostics := readSource(t, source, collectionConfig(t, source, test.filters), queryFunc(func(_ context.Context, target sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
				statements = append(statements, target.Database+": "+sql)
				return userLockoutRows(), nil
			}))
			require.False(t, diagnostics.HasError(), "%v", diagnostics)
			assert.Equal(t, []string{"admin: SHOW USER LOCKOUT"}, statements)

			var observed types.Object
			require.False(t, state.Get(context.Background(), &observed).HasError())
			assertLookupIdentity(t, observed.Attributes()["id"].(types.String), "admin", test.filters)
			items := collectionResult(source, observed).Elements()
			require.Len(t, items, len(test.users))

			for index, name := range test.users {
				item := items[index].(types.Object).Attributes()
				assert.Equal(t, types.StringValue(name), item["user_name"])

				if name == "sales_user" {
					assert.Equal(t, map[string]attr.Value{
						"user_name": types.StringValue("sales_user"), "is_locked": types.BoolValue(true), "lock_reason": types.StringValue("manual"),
						"locked_at": types.StringValue("2026-08-10 09:15:44"), "last_failed_at": types.StringNull(),
						"failed_login_attempts": types.Int64Value(0), "locked_by": types.Int64Value(100),
					}, item)
				}

				if name == "report_user" {
					assert.Equal(t, types.BoolValue(false), item["is_locked"])
					assert.True(t, item["lock_reason"].IsNull())
					assert.True(t, item["locked_by"].IsNull())
					assert.Equal(t, types.Int64Value(2), item["failed_login_attempts"])
				}
			}
		})
	}
}

// TestUserLockoutsFailures reports catalog errors, such as a missing superuser privilege, and undecodable rows.
func TestUserLockoutsFailures(t *testing.T) {
	for _, test := range []struct {
		name string
		rows []sqlclient.Row
		err  error
	}{
		{"not superuser", nil, errors.New("permission denied")},
		{"is_locked", []sqlclient.Row{{"user_name": "a", "is_locked": "maybe", "failed_login_attempts": "0"}}, nil},
		{"failed_login_attempts", []sqlclient.Row{{"user_name": "a", "is_locked": "f", "failed_login_attempts": "many"}}, nil},
		{"locked_by", []sqlclient.Row{{"user_name": "a", "is_locked": "t", "failed_login_attempts": "0", "locked_by": "admin"}}, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := newUserLockoutsDataSource()
			_, diagnostics := readSource(t, source, collectionConfig(t, source, nil), queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
				return test.rows, test.err
			}))
			assert.True(t, diagnostics.HasError())
		})
	}
}
