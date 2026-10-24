package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newModelsDataSource, resource: newExternalModelResource, collection: true, filters: []string{"database", "schema"}})

// TestModelsList checks unfiltered and filtered listings, the database routing, identities, and read-only execution.
func TestModelsList(t *testing.T) {
	for _, test := range []struct {
		name     string
		filters  map[string]string
		database string
		models   [][2]string
	}{
		{"all", nil, "admin", [][2]string{{"public", "customer_churn"}, {fakeModelSchema, fakeModelName}}},
		{"schema", map[string]string{"schema": fakeModelSchema}, "admin", [][2]string{{fakeModelSchema, fakeModelName}}},
		{"database", map[string]string{"database": "analytics", "schema": "public"}, "analytics", [][2]string{{"public", "customer_churn"}}},
		{"none", map[string]string{"schema": "missing"}, "admin", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, models := externalModelFake()
			models.models[fakeViewKey("public", "customer_churn")] = &fakeExternalModel{schema: "public", name: "customer_churn", owner: "admin"}

			var databases []string

			client := queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
				databases = append(databases, target.Database)
				return c.Query(ctx, target, sql, parameters)
			})
			source := newModelsDataSource()
			state, diagnostics := readSource(t, source, collectionConfig(t, source, test.filters), client)
			require.False(t, diagnostics.HasError(), "%v", diagnostics)
			assert.Empty(t, c.writes)
			assert.Equal(t, []string{test.database}, databases, "SHOW MODEL ALL runs in the listed database")

			var observed types.Object
			require.False(t, state.Get(context.Background(), &observed).HasError())
			assertLookupIdentity(t, observed.Attributes()["id"].(types.String), test.database, test.filters)
			items := collectionResult(source, observed).Elements()
			require.Len(t, items, len(test.models))

			for index, model := range test.models {
				item := items[index].(types.Object).Attributes()
				assert.Equal(t, types.StringValue(test.database), item["database"])
				assert.Equal(t, types.StringValue(model[0]), item["schema"])
				assert.Equal(t, types.StringValue(model[1]), item["name"])
			}
		})
	}
}

// TestModelsListFailures reports catalog errors and undecodable rows instead of a partial listing.
func TestModelsListFailures(t *testing.T) {
	for name, result := range map[string]struct {
		rows []sqlclient.Row
		err  error
	}{
		"no name":     {rows: []sqlclient.Row{{"Schema Name": "public"}}},
		"unavailable": {err: errors.New("catalog unavailable")},
	} {
		t.Run(name, func(t *testing.T) {
			source := newModelsDataSource()
			_, diagnostics := readSource(t, source, collectionConfig(t, source, nil), queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
				return result.rows, result.err
			}))
			assert.True(t, diagnostics.HasError())
		})
	}
}
