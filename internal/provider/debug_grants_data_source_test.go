package provider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newDebugGrantsDataSource, resource: newDebugGrantResource, collection: true, filters: []string{"database"}})

// TestDebugGrantsListing lists the fake's DEBUG grantees of one database from admin, skips other privileges, rejects
// remote aliases, and lists nothing for a database that no longer exists.
func TestDebugGrantsListing(t *testing.T) {
	c := fullCatalog()
	fake := fakeState[*debugGrantFakeFamily](c, "debug_grant")
	fake.grant("sales_db", debugGrantKey{identityType: "global_user", name: "IAM:sales_auditor"}, true)
	fake.grant("sales_db", debugGrantKey{identityType: "admins_of_account", name: "123456789012"}, true)
	fake.grant("sales_db", debugGrantKey{identityType: "global_group", name: "myidc:audit_team"}, true)

	var statements []string

	client := queryFunc(func(ctx context.Context, connection sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		assert.Equal(t, "admin", connection.Database)

		statements = append(statements, sql)

		return c.Query(ctx, connection, sql, parameters)
	})

	items := listingItems(t, newDebugGrantsDataSource(), map[string]string{"database": "sales_db"}, client)
	assert.Equal(t, []string{readDebugGrantStatement("sales_db")}, statements)

	var grantees []string

	for _, item := range items {
		assert.Equal(t, types.StringValue("sales_db"), item["database"])
		grantees = append(grantees, item["grantee_type"].(types.String).ValueString()+":"+item["grantee"].(types.String).ValueString())
	}

	assert.Equal(t, []string{"ACCOUNT:123456789012", "ROLE:myidc:audit_team", "USER:IAM:sales_auditor"}, grantees, "the PUBLIC TEMP row is skipped")

	statements = nil

	assertListingFails(t, newDebugGrantsDataSource(), map[string]string{"database": debugGrantTestDatabase}, client)
	assert.Empty(t, statements, "remote aliases never reach SQL")
	assertListingFails(t, newDebugGrantsDataSource(), nil, nil)

	// A failing SHOW GRANTS lists nothing only when SHOW DATABASES confirms the database is gone, as the resource reads.
	for listed, fails := range map[bool]bool{false: false, true: true} {
		statements = nil
		missing := queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
			statements = append(statements, sql)
			if strings.HasPrefix(sql, "SHOW DATABASES") {
				if listed {
					return []sqlclient.Row{{"database_name": "dropped"}}, nil
				}

				return nil, nil
			}

			return nil, errors.New("database \"dropped\" does not exist")
		})

		source := newDebugGrantsDataSource()
		state, diagnostics := readSource(t, source, collectionConfig(t, source, map[string]string{"database": "dropped"}), missing)
		assert.Equal(t, fails, diagnostics.HasError(), "listed=%t", listed)

		if !fails {
			var observed types.Object
			require.False(t, state.Get(context.Background(), &observed).HasError())
			assert.Empty(t, collectionResult(source, observed).Elements(), "a database that does not exist lists nothing")
		}

		assert.Equal(t, []string{readDebugGrantStatement("dropped"), readDatabaseStatement("dropped")}, statements)
	}
}
