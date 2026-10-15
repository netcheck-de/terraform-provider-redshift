package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
)

var _ = registerParity(parityCase{source: newCommentDataSource, resource: newCommentResource, selectors: []string{"database_name", "schema_name", "object_type", "object_name", "column_name", "constraint_name"}})

// TestCommentLookup reads annotations and empty text without claiming or clearing them.
func TestCommentLookup(t *testing.T) {
	for _, text := range []string{"reporting objects", ""} {
		exerciseCatalogLookup(t, newCommentDataSource, map[string]string{"database_name": "warehouse", "object_type": "SCHEMA", "object_name": "serving"}, map[string]attr.Value{"text": types.StringValue(text)}, queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
			return []sqlclient.Row{{"text": text}}, nil
		}))
	}
}

// TestCommentLookupIdentitySelectors verifies object-specific identity keys and database routing.
func TestCommentLookupIdentitySelectors(t *testing.T) {
	for _, fields := range []map[string]string{
		{"database_name": "analytics", "object_type": "SCHEMA", "object_name": "serving"},
		{"database_name": "analytics", "object_type": "TABLE", "schema_name": "serving", "object_name": "events"},
		{"database_name": "analytics", "object_type": "COLUMN", "schema_name": "serving", "object_name": "events", "column_name": "time"},
		{"database_name": "analytics", "object_type": "CONSTRAINT", "schema_name": "serving", "object_name": "events", "constraint_name": "events_pkey"},
	} {
		t.Run(fields["object_type"], func(t *testing.T) {
			exerciseCatalogLookup(t, newCommentDataSource, fields, map[string]attr.Value{"text": types.StringValue("")}, queryFunc(func(_ context.Context, target sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
				if strings.Contains(sql, "FROM svv_redshift_databases") {
					assert.Equal(t, "admin", target.Database)
				} else {
					assert.Equal(t, "analytics", target.Database)
				}
				return []sqlclient.Row{{"text": ""}}, nil
			}))
		})
	}
}
