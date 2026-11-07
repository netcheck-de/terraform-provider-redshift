package provider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newMaterializedViewsDataSource, resource: newMaterializedViewResource, collection: true, filters: []string{"database", "schema"}})

// materializedViewsRead reads the materialized view listing as a transcript step.
func materializedViewsRead(t *testing.T, filters map[string]string) func(sqlclient.Client) ([]map[string]attr.Value, map[string]string, error) {
	t.Helper()

	return func(client sqlclient.Client) ([]map[string]attr.Value, map[string]string, error) {
		items, identity, diagnostics := discoveryRead(t, newMaterializedViewsDataSource, filters, client)
		if diagnostics.HasError() {
			return nil, nil, errors.New(diagnostics.Errors()[0].Detail())
		}

		return items, identity, nil
	}
}

// TestMaterializedViewsListingTranscript lists materialized views with the lookup's values and refresh settings, leaves out
// ordinary views, and filters by schema.
func TestMaterializedViewsListingTranscript(t *testing.T) {
	c, views := viewsListingFake()
	views.relations[fakeViewKey("staging", "daily_totals")] = &fakeViewRelation{schema: "staging", name: "daily_totals", query: fakeMaterializedQuery, owner: "etl", materialized: true}

	items := listingTranscript(t, "materialized_views", "database", c, materializedViewsRead(t, nil))
	assert.Equal(t, []string{fakeMaterializedViewName, "daily_totals"}, discoveryNames(items, "name"), "ordinary views are not listed")
	definition := views.get(fakeMaterializedViewName).definition()
	assert.Equal(t, map[string]attr.Value{
		"database": types.StringValue("admin"), "schema": types.StringValue(fakeViewSchema), "name": types.StringValue(fakeMaterializedViewName),
		"query": types.StringValue(definition), "auto_refresh": types.BoolValue(true), "owner": types.StringValue(fakeViewOwner),
		"definition_fingerprint": types.StringValue(definitionFingerprint(definition)),
	}, items[0])
	assert.Equal(t, types.BoolValue(false), items[1]["auto_refresh"])

	items = listingTranscript(t, "materialized_views", "schema", c, materializedViewsRead(t, map[string]string{"schema": "staging"}))
	assert.Equal(t, []string{"daily_totals"}, discoveryNames(items, "name"))

	items = listingTranscript(t, "materialized_views", "no_materialized_views", c, materializedViewsRead(t, map[string]string{"schema": "other"}))
	assert.Empty(t, items, "without materialized views SVV_MV_INFO is not read")

	discoveryFailures(t, newMaterializedViewsDataSource, nil)
}

// TestMaterializedViewsListingHiddenRefresh reports a refresh setting SVV_MV_INFO hides as null instead of failing.
func TestMaterializedViewsListingHiddenRefresh(t *testing.T) {
	c, _ := viewFake()
	items, _, diagnostics := discoveryRead(t, newMaterializedViewsDataSource, nil, materializedViewHiddenRefresh(c))
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.Equal(t, []string{fakeMaterializedViewName}, discoveryNames(items, "name"))
	assert.True(t, items[0]["auto_refresh"].IsNull())
	assert.Equal(t, types.StringValue(fakeViewOwner), items[0]["owner"])
}

// TestMaterializedViewsListingRejectsBadRefreshRows reports failed, malformed, or ambiguous SVV_MV_INFO reads.
func TestMaterializedViewsListingRejectsBadRefreshRows(t *testing.T) {
	c, _ := viewFake()

	for name, refresh := range map[string]func() ([]sqlclient.Row, error){
		"failure": func() ([]sqlclient.Row, error) { return nil, errors.New("STV state unavailable") },
		"malformed": func() ([]sqlclient.Row, error) {
			return []sqlclient.Row{{"schema_name": fakeViewSchema, "name": fakeMaterializedViewName, "autorefresh": "x"}}, nil
		},
		"ambiguous": func() ([]sqlclient.Row, error) {
			row := sqlclient.Row{"schema_name": fakeViewSchema, "name": fakeMaterializedViewName, "autorefresh": "t"}
			return []sqlclient.Row{row, row}, nil
		},
	} {
		_, _, diagnostics := discoveryRead(t, newMaterializedViewsDataSource, nil, queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
			if strings.Contains(sql, "svv_mv_info") {
				return refresh()
			}

			return c.Query(ctx, target, sql, parameters)
		}))
		assert.True(t, diagnostics.HasError(), name)
	}
}

// TestMaterializedViewsSQL pins the refresh setting listing and its schema filter.
func TestMaterializedViewsSQL(t *testing.T) {
	checkSQL(t, "materialized_views_sql", []sqlCase{
		{"refresh", discoverySQL(listMaterializedViewRefreshQuery("analytics", ""))},
		{"refresh_schema", discoverySQL(listMaterializedViewRefreshQuery("analytics", `Odd"Schema`))},
	})
}
