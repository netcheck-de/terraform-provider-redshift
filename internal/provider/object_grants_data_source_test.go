package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newObjectGrantsDataSource, resource: newObjectGrantResource, collection: true, filters: []string{"database_name", "object_type", "schema_name", "object_name", "grantee", "grantee_type"}})

// listingItems reads a listing and returns its elements' attributes, failing the test on diagnostics.
func listingItems(t *testing.T, source datasource.DataSource, filters map[string]string, client sqlclient.Client) []map[string]attr.Value {
	t.Helper()

	state, diagnostics := readSource(t, source, collectionConfig(t, source, filters), client)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)

	var observed types.Object
	require.False(t, state.Get(context.Background(), &observed).HasError())

	list := collectionResult(source, observed)
	require.False(t, list.IsNull(), "an empty listing is an empty list, not null")

	items := make([]map[string]attr.Value, 0, len(list.Elements()))
	for _, element := range list.Elements() {
		items = append(items, element.(types.Object).Attributes())
	}

	return items
}

// assertListingFails checks that a catalog failure, or an invalid selection, is reported as an error.
func assertListingFails(t *testing.T, source datasource.DataSource, filters map[string]string, client sqlclient.Client) {
	t.Helper()

	if client == nil {
		client = queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
			return nil, errors.New("catalog unavailable")
		})
	}

	_, diagnostics := readSource(t, source, collectionConfig(t, source, filters), client)
	assert.True(t, diagnostics.HasError(), "catalog failures are errors")
}

// stringSet builds a Terraform string set for element assertions.
func stringSet(values ...string) types.Set {
	return types.SetValueMust(types.StringType, grantStringValues(values))
}

// TestObjectGrantsListing reads SHOW GRANTS in the selected database, groups each object's privileges per grantee,
// and reports invalid selections as errors.
func TestObjectGrantsListing(t *testing.T) {
	var database, statement string

	client := queryFunc(func(_ context.Context, connection sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		database, statement = connection.Database, sql

		assert.Empty(t, parameters, "SHOW GRANTS takes no bindings")

		row := func(privilege, name, kind, option string) sqlclient.Row {
			return sqlclient.Row{"schema_name": "serving", "object_name": "events", "object_type": "TABLE", "privilege_type": privilege, "identity_name": name, "identity_type": kind, "admin_option": option, "privilege_scope": "TABLE", "database_name": "warehouse", "grantor_name": "admin"}
		}

		return []sqlclient.Row{row("SELECT", "readers", "group", "f"), row("SELECT", "analyst", "user", "t"), row("INSERT", "analyst", "user", "f")}, nil
	})

	for _, test := range []struct {
		name, database, statement string
		filters                   map[string]string
		grantees                  []string
		options                   types.Set
	}{
		{"table", "warehouse", `SHOW GRANTS ON TABLE "warehouse"."serving"."events"`, map[string]string{"database_name": "warehouse", "object_type": "TABLE", "schema_name": "serving", "object_name": "events"}, []string{"GROUP:readers", "USER:analyst"}, stringSet("SELECT")},
		{"table group", "warehouse", `SHOW GRANTS ON TABLE "warehouse"."serving"."events"`, map[string]string{"database_name": "warehouse", "object_type": "TABLE", "schema_name": "serving", "object_name": "events", "grantee_type": "GROUP"}, []string{"GROUP:readers"}, stringSet()},
		{"user", "admin", `SHOW GRANTS FOR "analyst" FROM DATABASE "admin"`, map[string]string{"grantee": "analyst", "grantee_type": "USER"}, []string{"USER:analyst"}, types.SetNull(types.StringType)},
	} {
		t.Run(test.name, func(t *testing.T) {
			items := listingItems(t, newObjectGrantsDataSource(), test.filters, client)
			assert.Equal(t, test.database, database)
			assert.Equal(t, test.statement, statement)

			var grantees []string

			for _, item := range items {
				grantees = append(grantees, item["grantee_type"].(types.String).ValueString()+":"+item["grantee"].(types.String).ValueString())
				assert.Equal(t, types.StringValue("warehouse"), item["database_name"])
				assert.Equal(t, types.StringValue("serving"), item["schema_name"])
				assert.Equal(t, types.StringValue("TABLE"), item["object_type"])
				assert.Equal(t, types.StringValue("events"), item["object_name"])

				if item["grantee"].Equal(types.StringValue("analyst")) {
					assert.Equal(t, stringSet("INSERT", "SELECT"), item["privileges"], "one element carries every privilege of the grantee")
					assert.Equal(t, test.options, item["grant_option_privileges"], "SHOW GRANTS FOR reports no grant options")
				}
			}

			assert.Equal(t, test.grantees, grantees)
		})
	}

	for name, filters := range map[string]map[string]string{
		"nothing selected": nil,
		"group identity":   {"grantee": "readers", "grantee_type": "GROUP"},
	} {
		t.Run(name, func(t *testing.T) {
			statement = ""

			assertListingFails(t, newObjectGrantsDataSource(), filters, client)
			assert.Empty(t, statement, "invalid selections never reach SQL")
		})
	}

	assertListingFails(t, newObjectGrantsDataSource(), map[string]string{"object_type": "DATABASE"}, nil)
}
