package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestObjectGrantLocalChecks keeps the template and model existence checks in the object's database: SHOW TEMPLATES
// rejects a cross-database schema, and the model catalog is read where the model lives.
func TestObjectGrantLocalChecks(t *testing.T) {
	for kind, want := range map[string]string{
		"TEMPLATE": `SHOW TEMPLATES FROM SCHEMA "serving" LIKE 'csv_load'`,
		"MODEL":    "SELECT model_name FROM svv_ml_model_info",
	} {
		t.Run(kind, func(t *testing.T) {
			r := newObjectGrantResource().(*privilegeResource)
			target, err := r.prepare(privilegeObject(t, r, map[string]string{
				"database_name": "warehouse", "schema_name": "serving", "object_name": "csv_load", "object_type": kind,
				"grantee_type": "USER", "grantee": "loader",
			}))
			require.NoError(t, err)

			var local []catalogCheck
			for _, check := range target.checks {
				if check.local {
					local = append(local, check)
				}
			}

			require.Len(t, local, 1)
			assert.Contains(t, local[0].sql, want)
			assert.Equal(t, "warehouse", r.targetDatabase(target))
		})
	}
}
