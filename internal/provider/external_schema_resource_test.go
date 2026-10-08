package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExternalSchemaRegionRoundTrips verifies explicit cross-region SQL and both catalog key spellings.
func TestExternalSchemaRegionRoundTrips(t *testing.T) {
	for _, key := range []string{"REGION", "region"} {
		t.Run(key, func(t *testing.T) {
			r := &externalSchemaResource{testResourceClient(queryFunc(func(_ context.Context, _ dataapi.Connection, sql string, _ map[string]string) ([]dataapi.Row, error) {
				if strings.HasPrefix(sql, "CREATE EXTERNAL SCHEMA") {
					assert.Contains(t, sql, " REGION 'eu-central-1'")
					return nil, nil
				}
				return []dataapi.Row{{"schemaname": "example_external", "eskind": "1", "databasename": "glue", "esoptions": `{"IAM_ROLE":"role","` + key + `":"eu-central-1"}`}}, nil
			}))}
			data := externalSchemaModel{Database: types.StringValue("admin"), Name: types.StringValue("example_external"), GlueDatabase: types.StringValue("glue"), IAMRoleARN: types.StringValue("role"), Region: types.StringValue("eu-central-1")}
			require.False(t, invoke(t, r, "create", data, false).HasError())
			found, err := r.read(context.Background(), &data)
			require.NoError(t, err)
			require.True(t, found)
			assert.Equal(t, "eu-central-1", data.Region.ValueString())
		})
	}
}

// TestExternalSchemaReadsGlueBinding checks external schema option decoding.
func TestExternalSchemaReadsGlueBinding(t *testing.T) {
	c := &catalog{external: true}
	r := &externalSchemaResource{testResourceClient(c)}
	data := externalSchemaModel{Database: types.StringValue("admin"), Name: types.StringValue("example_external")}
	found, err := r.read(context.Background(), &data)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "example_glue", data.GlueDatabase.ValueString())
	assert.Equal(t, "arn:aws:iam::123456789012:role/spectrum", data.IAMRoleARN.ValueString())
}

// TestExternalSchemaRejectsIncompatibleCatalog rejects unsupported kinds and malformed options.
func TestExternalSchemaRejectsIncompatibleCatalog(t *testing.T) {
	for _, row := range []dataapi.Row{
		{"schemaname": "example_external", "eskind": "2"},
		{"schemaname": "example_external", "eskind": "1", "esoptions": "broken"},
		{"schemaname": "example_external", "eskind": "1", "esoptions": "{}"},
		{"schemaname": "example_external", "eskind": "1", "databasename": "different", "esoptions": `{"IAM_ROLE":"role"}`},
		{"schemaname": "example_external", "eskind": "1", "databasename": "glue", "esoptions": `{"IAM_ROLE":"different"}`},
	} {
		r := &externalSchemaResource{testResourceClient(queryFunc(func(context.Context, dataapi.Connection, string, map[string]string) ([]dataapi.Row, error) {
			return []dataapi.Row{row}, nil
		}))}
		data := externalSchemaModel{Database: types.StringValue("admin"), Name: types.StringValue("example_external"), GlueDatabase: types.StringValue("glue"), IAMRoleARN: types.StringValue("role")}
		_, err := r.read(context.Background(), &data)
		require.Error(t, err)
	}
}
