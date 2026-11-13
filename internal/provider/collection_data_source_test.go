package provider

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMultiQueryListingsReportSQLFailures fails each read after the preceding catalog queries have succeeded.
func TestMultiQueryListingsReportSQLFailures(t *testing.T) {
	for _, test := range []struct {
		name    string
		new     func() datasource.DataSource
		filters map[string]string
	}{
		{"external tables", newExternalTablesDataSource, nil},
		{"scoped grants", newGrantsDataSource, map[string]string{"database_name": "analytics"}},
		{"object grants", newObjectGrantsDataSource, map[string]string{"database_name": "analytics", "object_type": "DATABASE"}},
		{"role grants", newRoleGrantsDataSource, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			queries := 0
			for failAt := 0; failAt <= queries; failAt++ {
				c := fullCatalog()
				calls := 0
				client := queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
					require.True(t, fakeRead(sql), "a listing must not mutate: %s", sql)

					calls++
					if calls == failAt {
						return nil, errors.New("listing query unavailable")
					}

					return c.Query(ctx, target, sql, parameters)
				})
				source := test.new()

				state, diagnostics := readSource(t, source, collectionConfig(t, source, test.filters), client)
				if failAt == 0 {
					require.False(t, diagnostics.HasError(), "%v", diagnostics)
					require.Greater(t, calls, 1, "the fixture must exercise several queries")
					queries = calls

					continue
				}

				require.True(t, diagnostics.HasError(), "query %d: %v", failAt, diagnostics)
				assert.Contains(t, diagnostics.Errors()[0].Detail(), "listing query unavailable")
				assert.Equal(t, failAt, calls, "a failed query must stop the listing")
				assert.True(t, state.Raw.IsNull(), "a failure must not publish a partial or empty listing")
			}
		})
	}
}

// testSchemaListing is a tiny collection over schema names, owners, and a numeric column, so the helper is
// tested without a production listing.
func testSchemaListing() datasource.DataSource {
	element := collectionElement(newSchemaResource, "database", "name", "owner")
	element["tables"] = schema.Int64Attribute{Computed: true, MarkdownDescription: "Number of tables."}

	return newCollectionDataSource(collectionSpec{
		name:          "test_schemas",
		databaseField: "database",
		description:   "Lists schemas.",
		filters: map[string]schema.Attribute{
			"database": schema.StringAttribute{Optional: true, MarkdownDescription: "Database to list; defaults to the provider database."},
			"owner":    schema.StringAttribute{Optional: true, MarkdownDescription: "Only schemas owned by this user."},
		},
		element: element,
		list: func(ctx context.Context, client *resourceClient, filters types.Object) ([]map[string]attr.Value, error) {
			database := client.database.ValueString()
			if selected := objectString(filters, "database"); selected != "" {
				database = selected
			}

			query := sqlclient.Select("nspname", "usename").From("pg_namespace").OptEq("usename", "owner", objectString(filters, "owner"))

			rows, err := client.selectRows(ctx, database, query)
			if err != nil {
				return nil, err
			}

			var items []map[string]attr.Value
			for _, row := range rows {
				items = append(items, map[string]attr.Value{"database": types.StringValue(database), "name": types.StringValue(row["nspname"]), "owner": types.StringValue(row["usename"]), "tables": types.Int64Value(2)})
			}

			return items, nil
		},
	})
}

// collectionConfig builds a listing configuration with the given string filters and null computed values.
func collectionConfig(t *testing.T, source datasource.DataSource, filters map[string]string) types.Object {
	t.Helper()
	return catalogLookupObject(t, source, filters)
}

// TestCollectionDataSourceSchema checks the filter, identity, and element shape shared by all listings.
func TestCollectionDataSourceSchema(t *testing.T) {
	source := testSchemaListing()

	var metadata datasource.MetadataResponse
	source.Metadata(context.Background(), datasource.MetadataRequest{ProviderTypeName: "redshift"}, &metadata)
	assert.Equal(t, "redshift_test_schemas", metadata.TypeName)
	assertCollectionParity(t, parityCase{source: testSchemaListing, filters: []string{"database", "owner"}, collection: true})

	resourceShaped := func() datasource.DataSource {
		return newCollectionDataSource(collectionSpec{name: "test_schemas", element: collectionElement(newSchemaResource, "database", "name", "owner"), filters: map[string]schema.Attribute{"owner": schema.StringAttribute{Optional: true}}})
	}
	assertCollectionParity(t, parityCase{source: resourceShaped, resource: newSchemaResource, filters: []string{"owner"}, collection: true})
	assert.Panics(t, func() { collectionElement(newSchemaResource, "missing") })

	var response datasource.SchemaResponse
	source.Schema(context.Background(), datasource.SchemaRequest{}, &response)
	assert.Contains(t, response.Schema.Attributes, "test_schemas", "the result is named after the data source")
	assert.NotContains(t, response.Schema.Attributes, "items")

	for _, reserved := range []string{"test_schemas", "id"} {
		assert.Panics(t, func() {
			newCollectionDataSource(collectionSpec{name: "test_schemas", filters: map[string]schema.Attribute{reserved: schema.StringAttribute{Optional: true}}})
		}, "a filter named %s would be overwritten by the read", reserved)
	}
}

// collectionResult returns a listing's result list, which is named after the data source.
func collectionResult(source datasource.DataSource, observed types.Object) types.List {
	return observed.Attributes()[source.(*collectionDataSource).spec.name].(types.List)
}

// TestCollectionElementFromBlocks lists resource blocks as computed nested attributes of the same shape.
func TestCollectionElementFromBlocks(t *testing.T) {
	element := collectionElement(newBlockTestResource, "database", "name", "column", "unique", "distribution")
	listing := func() datasource.DataSource {
		return newCollectionDataSource(collectionSpec{name: "block_tests", element: element})
	}
	assertCollectionParity(t, parityCase{source: listing, resource: newBlockTestResource, collection: true})

	column := element["column"].(schema.ListNestedAttribute)
	assert.True(t, column.Computed)
	assert.IsType(t, schema.SingleNestedAttribute{}, column.NestedObject.Attributes["identity"])
}

// TestCollectionID ignores unset filters and keeps JSON identities stable regardless of map iteration order.
func TestCollectionID(t *testing.T) {
	source := testSchemaListing().(*collectionDataSource)
	source.resourceClient = testResourceClient(fullCatalog())
	data := collectionConfig(t, source, map[string]string{"database": "analytics", "owner": "etl"})
	id := source.collectionID(data)
	assertLookupIdentity(t, id, map[string]string{"database": "analytics", "owner": "etl"})

	for range 20 {
		assert.Equal(t, id, source.collectionID(data))
	}

	lookupValue(&data, "database", types.StringUnknown())
	lookupValue(&data, "owner", types.StringNull())
	assertLookupIdentity(t, source.collectionID(data), map[string]string{"database": "admin"})
}

// TestCollectionDataSourceRead checks filtered and unfiltered listings, empty results, and failures.
func TestCollectionDataSourceRead(t *testing.T) {
	var (
		target     sqlclient.Connection
		parameters map[string]string
		rows       []sqlclient.Row
	)

	client := queryFunc(func(_ context.Context, connection sqlclient.Connection, sql string, params map[string]string) ([]sqlclient.Row, error) {
		require.Contains(t, sql, "SELECT nspname, usename FROM pg_namespace")

		target, parameters = connection, params

		return rows, nil
	})

	for _, test := range []struct {
		name     string
		filters  map[string]string
		rows     []sqlclient.Row
		database string
		identity map[string]string
	}{
		{"unfiltered", nil, []sqlclient.Row{{"nspname": "public", "usename": "admin"}, {"nspname": "serving", "usename": "etl"}}, "admin", map[string]string{"database": "admin"}},
		{"filtered", map[string]string{"database": "analytics", "owner": "etl"}, []sqlclient.Row{{"nspname": "serving", "usename": "etl"}}, "analytics", map[string]string{"database": "analytics", "owner": "etl"}},
		{"empty", map[string]string{"owner": "nobody"}, nil, "admin", map[string]string{"database": "admin", "owner": "nobody"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			rows = test.rows
			source := testSchemaListing()
			state, diagnostics := readSource(t, source, collectionConfig(t, source, test.filters), client)
			require.False(t, diagnostics.HasError(), "%v", diagnostics)
			assert.Equal(t, test.database, target.Database)

			if owner, ok := test.filters["owner"]; ok {
				assert.Equal(t, map[string]string{"owner": owner}, parameters)
			}

			var observed types.Object
			require.False(t, state.Get(context.Background(), &observed).HasError())

			var identity map[string]string
			require.NoError(t, json.Unmarshal([]byte(observed.Attributes()["id"].(types.String).ValueString()), &identity))
			assert.Equal(t, test.identity, identity)

			items := collectionResult(source, observed)
			require.False(t, items.IsNull(), "an empty listing is an empty list, not null")
			require.Len(t, items.Elements(), len(test.rows))

			for index, row := range test.rows {
				item := items.Elements()[index].(types.Object).Attributes()
				assert.Equal(t, types.StringValue(row["nspname"]), item["name"])
				assert.Equal(t, types.StringValue(row["usename"]), item["owner"])
				assert.Equal(t, types.StringValue(test.database), item["database"])
				assert.Equal(t, types.Int64Value(2), item["tables"])
			}
		})
	}

	source := testSchemaListing()
	_, diagnostics := readSource(t, source, collectionConfig(t, source, nil), queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
		return nil, errors.New("catalog unavailable")
	}))
	assert.True(t, diagnostics.HasError(), "catalog failures are errors")

	malformed := newCollectionDataSource(collectionSpec{name: "test_schemas", element: map[string]schema.Attribute{"name": schema.StringAttribute{Computed: true}}, list: func(context.Context, *resourceClient, types.Object) ([]map[string]attr.Value, error) {
		return []map[string]attr.Value{{"unknown": types.StringValue("x")}}, nil
	}})
	_, diagnostics = readSource(t, malformed, collectionConfig(t, malformed, nil), client)
	assert.True(t, diagnostics.HasError(), "elements must match the element type")
}

// TestCollectionIdentityIncludesBooleanFilter records a boolean filter in the listing identity as text.
func TestCollectionIdentityIncludesBooleanFilter(t *testing.T) {
	d := newCollectionDataSource(collectionSpec{name: "items", filters: map[string]schema.Attribute{"enabled": schema.BoolAttribute{Optional: true}}}).(*collectionDataSource)
	d.resourceClient = testResourceClient(fullCatalog())
	data := types.ObjectValueMust(map[string]attr.Type{"enabled": types.BoolType}, map[string]attr.Value{"enabled": types.BoolValue(true)})
	assertLookupIdentity(t, d.collectionID(data), map[string]string{"enabled": "true"})
}

// failingCatalog fails every query, for the error paths of the listings.
var failingCatalog = queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
	return nil, errors.New("catalog unavailable")
})

// localListings are the database-scoped listings whose objects exist only in local databases, with their database
// filter.
var localListings = map[string]struct {
	factory func() datasource.DataSource
	field   string
}{
	"column_grants":              {newColumnGrantsDataSource, "database_name"},
	"comments":                   {newCommentsDataSource, "database_name"},
	"constraints":                {newConstraintsDataSource, "database"},
	"copy_jobs":                  {newCopyJobsDataSource, "database"},
	"datashare_memberships":      {newDatashareMembershipsDataSource, "database"},
	"datashare_schemas":          {newDatashareSchemasDataSource, "database"},
	"datashare_tables":           {newDatashareTablesDataSource, "database"},
	"default_privileges":         {newDefaultPrivilegesDataSource, "database_name"},
	"external_functions":         {newExternalFunctionsDataSource, "database"},
	"external_partitions":        {newExternalPartitionsDataSource, "database"},
	"external_schemas":           {newExternalSchemasDataSource, "database"},
	"external_tables":            {newExternalTablesDataSource, "database"},
	"functions":                  {newFunctionsDataSource, "database"},
	"language_grants":            {newLanguageGrantsDataSource, "database_name"},
	"masking_policy_attachments": {newMaskingPolicyAttachmentsDataSource, "database"},
	"materialized_views":         {newMaterializedViewsDataSource, "database"},
	"models":                     {newModelsDataSource, "database"},
	"procedures":                 {newProceduresDataSource, "database"},
	"rls_policies":               {newRlsPoliciesDataSource, "database"},
	"rls_policy_attachments":     {newRlsPolicyAttachmentsDataSource, "database"},
	"routine_parameters":         {newRoutineParametersDataSource, "database"},
	"schemas":                    {newSchemasDataSource, "database"},
	"table_securities":           {newTableSecuritiesDataSource, "database"},
	"tables":                     {newTablesDataSource, "database"},
	"templates":                  {newTemplatesDataSource, "database"},
	"views":                      {newViewsDataSource, "database"},
}

// TestLocalListingsSkipNonLocalDatabases lists nothing for a missing or shared database without connecting to it,
// records the selected database in the identity, documents that policy, and reports a failing database lookup.
func TestLocalListingsSkipNonLocalDatabases(t *testing.T) {
	for name, listing := range localListings {
		t.Run(name, func(t *testing.T) {
			var response datasource.SchemaResponse
			listing.factory().Schema(context.Background(), datasource.SchemaRequest{}, &response)
			assert.Contains(t, response.Schema.MarkdownDescription, discoveryLocalScope)

			for _, database := range []string{"consumer_db", "dropped"} {
				recorder := &recordingClient{client: fullCatalog()}
				items, identity, diagnostics := discoveryRead(t, listing.factory, map[string]string{listing.field: database}, recorder)
				require.False(t, diagnostics.HasError(), "%v", diagnostics)
				assert.Empty(t, items, "%s lists nothing", database)
				assert.Equal(t, database, identity[listing.field])

				for _, entry := range recorder.take() {
					assert.Equal(t, "admin", entry.database, "%s is never connected to: %s", database, entry.sql)
				}
			}

			_, _, diagnostics := discoveryRead(t, listing.factory, map[string]string{listing.field: "analytics"}, failingCatalog)
			assert.True(t, diagnostics.HasError(), "a failing database lookup is an error, not an empty listing")
		})
	}
}

// TestSharedListingsDocumentRouting documents, for the listings that read shared databases from the provider
// database, that policy.
func TestSharedListingsDocumentRouting(t *testing.T) {
	for _, factory := range []func() datasource.DataSource{newColumnsDataSource, newDebugGrantsDataSource, newGrantsDataSource, newObjectGrantsDataSource} {
		var response datasource.SchemaResponse
		factory().Schema(context.Background(), datasource.SchemaRequest{}, &response)
		assert.Contains(t, response.Schema.MarkdownDescription, discoverySharedScope)
	}
}

// TestCollectionFiltersRejectEmptyStrings requires every string filter of every listing to reject an empty string,
// which would otherwise read as an unset filter.
func TestCollectionFiltersRejectEmptyStrings(t *testing.T) {
	ctx := context.Background()
	minimum := stringvalidator.LengthAtLeast(1).Description(ctx)

	for _, factory := range registeredDataSources() {
		listing, ok := factory().(*collectionDataSource)
		if !ok {
			continue
		}

		for name, filter := range listing.spec.filters {
			text, ok := filter.(schema.StringAttribute)
			if !ok {
				continue
			}

			var descriptions []string
			for _, validator := range text.Validators {
				descriptions = append(descriptions, validator.Description(ctx))
			}

			assert.Contains(t, descriptions, minimum, "%s.%s", listing.spec.name, name)
		}
	}
}
