package provider

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/require"
)

var _ = registerFakeFamily("discovery", func() fakeFamily { return &discoveryFamily{rows: map[string][]dataapi.Row{}} })

// discoveryListing describes how the fake recognizes and filters one catalog listing.
type discoveryListing struct {
	// name keys the listing's rows.
	name string
	// source recognizes the listing's SQL by its FROM clause.
	source string
	// selects, when set, must open the SELECT list, for sources another type's read shares.
	selects string
	// equal maps a bound parameter to the row column it must equal.
	equal map[string]string
	// like maps a bound LIKE pattern to the row column it must match.
	like map[string]string
	// connected filters rows by the connection's database, for catalogs that only cover the current database.
	connected bool
}

// discoveryListings are the reads of the discovery data sources.
var discoveryListings = []discoveryListing{
	{name: "databases", source: string(databasesSource), equal: map[string]string{"database_type": "database_type"}, like: map[string]string{"name_like": "database_name"}},
	{name: "schemas", source: string(schemasSource), equal: map[string]string{"database": "database_name", "schema_type": "schema_type"}},
	{name: "tables", source: string(tablesSource), equal: map[string]string{"database": "database_name", "schema": "schema_name", "table_type": "table_type"}},
	{name: "materialized", source: "svv_mv_info", selects: "TRIM(schema_name) AS schema_name", equal: map[string]string{"database": "database_name", "schema": "schema_name"}},
	{name: "columns", source: "svv_all_columns", equal: map[string]string{"database": "database_name", "schema": "schema_name", "table": "table_name"}},
	{name: "constraints", source: string(constraintsSource), equal: map[string]string{"schema": "schema_name", "table": "table_name", "contype": "contype"}, connected: true},
}

// discoveryFamily holds the catalog rows the discovery listings read; it never handles writes.
type discoveryFamily struct {
	// rows maps a listing name to its rows, each carrying database_name for filtering.
	rows map[string][]dataapi.Row
}

// discoveryLike translates a SQL LIKE pattern into an anchored regular expression.
func discoveryLike(pattern string) *regexp.Regexp {
	var expression strings.Builder
	for _, char := range pattern {
		switch char {
		case '%':
			expression.WriteString(".*")
		case '_':
			expression.WriteString(".")
		default:
			expression.WriteString(regexp.QuoteMeta(string(char)))
		}
	}
	return regexp.MustCompile("^" + expression.String() + "$")
}

// query answers the discovery listings with the rows matching their bound filters.
func (f *discoveryFamily) query(_ *catalog, connection dataapi.Connection, sql string, parameters map[string]string) ([]dataapi.Row, bool, error) {
	for _, listing := range discoveryListings {
		if !strings.HasPrefix(sql, "SELECT "+listing.selects) || !strings.Contains(sql, " FROM "+listing.source+" ") {
			continue
		}
		var matched []dataapi.Row
	rows:
		for _, row := range f.rows[listing.name] {
			if listing.connected && row["database_name"] != connection.Database {
				continue
			}
			for parameter, column := range listing.equal {
				if value, ok := parameters[parameter]; ok && row[column] != value {
					continue rows
				}
			}
			for parameter, column := range listing.like {
				if pattern, ok := parameters[parameter]; ok && !discoveryLike(pattern).MatchString(row[column]) {
					continue rows
				}
			}
			matched = append(matched, maps.Clone(row))
		}
		return matched, true, nil
	}
	return nil, false, nil
}

// populate adds a representative warehouse: local and shared databases, local and external schemas, a table with
// a view and a materialized view over it, and key constraints.
func (f *discoveryFamily) populate() {
	f.rows = map[string][]dataapi.Row{
		"databases": {
			{"database_name": "admin", "owner": "rdsdb", "database_type": "local", "isolation_level": "Snapshot Isolation"},
			{"database_name": "analytics", "owner": "admin", "database_type": "local", "isolation_level": "Serializable"},
			{"database_name": "consumer_db", "owner": "", "database_type": "shared", "isolation_level": ""},
		},
		"schemas": {
			{"database_name": "analytics", "schema_name": "public", "owner": "rdsdb", "schema_type": "local", "source_database": ""},
			{"database_name": "analytics", "schema_name": "serving", "owner": "admin", "schema_type": "local", "source_database": ""},
			{"database_name": "analytics", "schema_name": "spectrum", "owner": "admin", "schema_type": "external", "source_database": "glue_events"},
			{"database_name": "consumer_db", "schema_name": "public", "owner": "", "schema_type": "shared", "source_database": ""},
		},
		"tables": {
			{"database_name": "analytics", "schema_name": "serving", "table_name": "orders", "table_type": "TABLE", "owner": "admin", "remarks": "Order facts."},
			{"database_name": "analytics", "schema_name": "serving", "table_name": "order_labels", "table_type": "VIEW", "owner": "admin", "remarks": ""},
			{"database_name": "analytics", "schema_name": "serving", "table_name": "order_totals", "table_type": "VIEW", "owner": "etl", "remarks": ""},
			{"database_name": "analytics", "schema_name": "spectrum", "table_name": "events", "table_type": "EXTERNAL TABLE", "owner": "", "remarks": ""},
		},
		"materialized": {
			{"database_name": "analytics", "schema_name": "serving", "name": "order_totals"},
		},
		"columns": {
			{"database_name": "analytics", "schema_name": "serving", "table_name": "orders", "column_name": "id", "ordinal_position": "1", "data_type": "integer", "character_maximum_length": "", "numeric_precision": "32", "numeric_scale": "0", "is_nullable": "NO", "column_default": "", "remarks": "Order key."},
			{"database_name": "analytics", "schema_name": "serving", "table_name": "orders", "column_name": "label", "ordinal_position": "2", "data_type": "character varying", "character_maximum_length": "64", "numeric_precision": "", "numeric_scale": "", "is_nullable": "YES", "column_default": "'none'::character varying", "remarks": ""},
			{"database_name": "analytics", "schema_name": "spectrum", "table_name": "events", "column_name": "payload", "ordinal_position": "1", "data_type": "varchar", "character_maximum_length": "256", "numeric_precision": "", "numeric_scale": "", "is_nullable": "", "column_default": "", "remarks": ""},
		},
		"constraints": {
			{"database_name": "analytics", "schema_name": "serving", "table_name": "orders", "constraint_name": "orders_pkey", "contype": "p", "constraint_type": "PRIMARY KEY", "definition": "PRIMARY KEY (id)", "referenced_schema": "", "referenced_table": ""},
			{"database_name": "analytics", "schema_name": "serving", "table_name": "orders", "constraint_name": "orders_label_key", "contype": "u", "constraint_type": "UNIQUE", "definition": `UNIQUE (label, "Region")`, "referenced_schema": "", "referenced_table": ""},
			{"database_name": "analytics", "schema_name": "serving", "table_name": "order_lines", "constraint_name": "order_lines_order_fkey", "contype": "f", "constraint_type": "FOREIGN KEY", "definition": "FOREIGN KEY (order_id) REFERENCES serving.orders(id)", "referenced_schema": "serving", "referenced_table": "orders"},
		},
	}
}

// discoveryRead reads a listing with string filters and returns its items and identity.
func discoveryRead(t *testing.T, factory func() datasource.DataSource, filters map[string]string, client dataapi.Client) ([]map[string]attr.Value, map[string]string, diag.Diagnostics) {
	t.Helper()
	source := factory()
	state, diagnostics := readSource(t, source, collectionConfig(t, source, filters), client)
	if diagnostics.HasError() {
		return nil, nil, diagnostics
	}
	var observed types.Object
	require.False(t, state.Get(context.Background(), &observed).HasError())
	var identity map[string]string
	require.NoError(t, json.Unmarshal([]byte(observed.Attributes()["id"].(types.String).ValueString()), &identity))
	list := observed.Attributes()[collectionItems].(types.List)
	require.False(t, list.IsNull(), "a listing is never null")
	var items []map[string]attr.Value
	for _, element := range list.Elements() {
		items = append(items, element.(types.Object).Attributes())
	}
	return items, identity, diagnostics
}

// discoveryTranscript reads a listing against the populated fake and pins the SQL conversation in group.
func discoveryTranscript(t *testing.T, group, name string, factory func() datasource.DataSource, filters map[string]string) []map[string]attr.Value {
	t.Helper()
	recorder := &recordingClient{client: fullCatalog()}
	items, _, diagnostics := discoveryRead(t, factory, filters, recorder)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	checkTranscript(t, group, name, recorder.take())
	return items
}

// discoveryNames returns the given attribute of every item, for compact order-sensitive assertions.
func discoveryNames(items []map[string]attr.Value, attribute string) []string {
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item[attribute].(types.String).ValueString())
	}
	return names
}

// discoverySQL renders a catalog query for checkSQL. Placeholders keep values out of the golden SQL, so the
// transcripts pin the bound values instead.
func discoverySQL(query dataapi.Query) func() (string, error) {
	return func() (string, error) {
		sql, _, err := query.Build()
		return sql, err
	}
}

// discoveryFailures checks that a listing reports catalog failures and malformed rows instead of partial results.
func discoveryFailures(t *testing.T, factory func() datasource.DataSource, filters map[string]string, malformed ...dataapi.Row) {
	t.Helper()
	_, _, diagnostics := discoveryRead(t, factory, filters, queryFunc(func(context.Context, dataapi.Connection, string, map[string]string) ([]dataapi.Row, error) {
		return nil, errors.New("catalog unavailable")
	}))
	require.True(t, diagnostics.HasError(), "catalog failures are errors")
	for _, row := range malformed {
		_, _, diagnostics = discoveryRead(t, factory, filters, queryFunc(func(context.Context, dataapi.Connection, string, map[string]string) ([]dataapi.Row, error) {
			return []dataapi.Row{row}, nil
		}))
		require.True(t, diagnostics.HasError(), "malformed row %v must be an error", row)
	}
}
