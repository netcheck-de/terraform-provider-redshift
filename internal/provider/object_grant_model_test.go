package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestObjectGrantModelLifecycle grants EXECUTE on a model with its grant option, reads it back from
// SVV_ML_MODEL_PRIVILEGES in the model's database, and revokes it.
func TestObjectGrantModelLifecycle(t *testing.T) {
	fields := map[string]string{"database_name": "warehouse", "schema_name": "serving", "object_name": "summarizer", "object_type": "MODEL", "grantee_type": "USER", "grantee": "analyst"}
	r := newObjectGrantResource().(*privilegeResource)
	c := &privilegeCatalog{values: map[string]bool{}, grantee: "analyst", kind: "user"}

	var reads []string

	r.resourceClient = testResourceClient(queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		if strings.HasPrefix(sql, "SELECT privilege_type") {
			reads = append(reads, target.Database)
		}

		return c.Query(ctx, target, sql, parameters)
	}))
	executeOnly := []string{"EXECUTE"}
	require.NoError(t, r.reconcile(context.Background(), withGrantOptions(privilegeObject(t, r, fields, executeOnly...), executeOnly)))
	assert.Equal(t, []string{`GRANT EXECUTE ON MODEL "serving"."summarizer" TO "analyst" WITH GRANT OPTION`}, c.writes)
	require.NoError(t, r.reconcile(context.Background(), withGrantOptions(privilegeObject(t, r, fields), nil)))
	require.Len(t, c.writes, 2)
	assert.Equal(t, `REVOKE EXECUTE ON MODEL "serving"."summarizer" FROM "analyst"`, c.writes[1])
	require.NotEmpty(t, reads)

	for _, database := range reads {
		assert.Equal(t, "warehouse", database, "SVV_ML_MODEL_PRIVILEGES has no database column")
	}
}

// TestObjectGrantModelMissing removes the grant of a dropped model from state instead of reporting an empty set.
func TestObjectGrantModelMissing(t *testing.T) {
	r := newObjectGrantResource().(*privilegeResource)
	r.resourceClient = testResourceClient(queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
		if strings.Contains(sql, "FROM svv_ml_model_info") {
			return nil, nil
		}

		return []sqlclient.Row{{"name": "exists"}}, nil
	}))
	fields := map[string]string{"database_name": "warehouse", "schema_name": "serving", "object_name": "summarizer", "object_type": "MODEL", "grantee_type": "ROLE", "grantee": "readers"}
	attributes := privilegeObject(t, r, fields).Attributes()
	attributes["id"] = identity(fields)
	state := testState(t, r, types.ObjectValueMust(privilegeObject(t, r, fields).AttributeTypes(context.Background()), attributes))
	resp := resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)
	require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
	assert.True(t, resp.State.Raw.IsNull())
}
