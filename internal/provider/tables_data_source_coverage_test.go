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

func TestCoverageTablesDatabaseLookupFailure(t *testing.T) {
	client := testResourceClient(queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
		if strings.Contains(sql, "svv_all_tables") {
			return []sqlclient.Row{{"schema_name": "public", "table_name": "view", "table_type": "VIEW"}}, nil
		}
		return nil, errors.New("database metadata unavailable")
	}))
	filters := types.ObjectValueMust(map[string]attr.Type{"database": types.StringType, "schema": types.StringType, "table_type": types.StringType}, map[string]attr.Value{"database": types.StringValue("other"), "schema": types.StringNull(), "table_type": types.StringNull()})
	items, err := tablesList(context.Background(), &client, filters)
	require.ErrorContains(t, err, "database metadata unavailable")
	assert.Nil(t, items)
}
