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

var _ = registerParity(parityCase{source: newCopyJobDataSource, resource: newCopyJobResource, selectors: []string{"database", "name"}})

var _ = registerParity(parityCase{source: newCopyJobsDataSource, resource: newCopyJobResource, collection: true, filters: []string{"database"}})

// TestCopyJobLookup observes AUTO and the role, and reports the unreadable definition as null.
func TestCopyJobLookup(t *testing.T) {
	exerciseCatalogLookup(t, newCopyJobDataSource, map[string]string{"database": "admin", "name": "events_load"}, map[string]attr.Value{
		"iam_role":   types.StringValue(copyJobFakeRole),
		"auto":       types.BoolValue(false),
		"source":     types.StringNull(),
		"parameters": types.MapNull(types.StringType),
		"table":      types.ObjectNull(copyJobTableType.AttrTypes),
	}, fullCatalog())
}

// TestCopyJobsListing lists the visible jobs of a database with blanks of the character columns trimmed.
func TestCopyJobsListing(t *testing.T) {
	for _, filters := range []map[string]string{{}, {"database": "admin"}} {
		source := newCopyJobsDataSource()
		state, diagnostics := readSource(t, source, collectionConfig(t, source, filters), fullCatalog())
		require.False(t, diagnostics.HasError(), "%v", diagnostics)
		var data types.Object
		require.False(t, state.Get(context.Background(), &data).HasError())
		items := collectionResult(source, data).Elements()
		require.Len(t, items, 1)
		item := items[0].(types.Object)
		assert.Equal(t, "events_load", objectString(item, "name"))
		assert.Equal(t, copyJobFakeRole, objectString(item, "iam_role"))
		assert.Equal(t, "admin", objectString(item, "database"))
		assert.Equal(t, types.BoolValue(false), item.Attributes()["auto"])
	}
	for name, client := range map[string]sqlclient.Client{
		"catalog error": queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
			return nil, errors.New("catalog unavailable")
		}),
		"malformed auto": queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
			return []sqlclient.Row{{"job_name": "j", "iam_role": copyJobFakeRole, "is_auto": "x"}}, nil
		}),
	} {
		t.Run(name, func(t *testing.T) {
			source := newCopyJobsDataSource()
			_, diagnostics := readSource(t, source, collectionConfig(t, source, nil), client)
			assert.True(t, diagnostics.HasError())
		})
	}
}
