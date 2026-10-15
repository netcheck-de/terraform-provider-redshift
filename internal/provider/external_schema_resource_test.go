package provider

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerLifecycleCase(lifecycleCase{name: "external schema", new: newExternalSchemaResource, model: externalSchemaModel{Database: types.StringValue("admin"), Name: types.StringValue("example_external"), GlueDatabase: types.StringValue("example_glue"), IAMRoleARN: types.StringValue("arn:aws:iam::123456789012:role/spectrum"), RefreshRevision: types.StringNull()}, absent: func(c *catalog) { c.external = false }})

var _ = registerReplacementPolicy("redshift_external_schema", map[string]replaceRule{
	"database":           replaceAlways,
	"name":               replaceAlways,
	"source_type":        replaceAlways,
	"glue_database":      replaceAlways,
	"source_database":    replaceAlways,
	"source_schema":      replaceAlways,
	"iam_role_arn":       replaceConditional("TestExternalSchemaConditionalReplacement"),
	"region":             replaceAlways,
	"uri":                replaceConditional("TestExternalSchemaConditionalReplacement"),
	"port":               replaceAlways,
	"secret_arn":         replaceConditional("TestExternalSchemaConditionalReplacement"),
	"authentication":     replaceNever,
	"authentication_arn": replaceNever,
	"owner":              replaceNever,
	"refresh_revision":   replaceAlways,
})

// TestExternalSchemaRegionRoundTrips verifies explicit cross-region SQL and both catalog key spellings.
func TestExternalSchemaRegionRoundTrips(t *testing.T) {
	for _, key := range []string{"REGION", "region"} {
		t.Run(key, func(t *testing.T) {
			r := &externalSchemaResource{testResourceClient(queryFunc(func(_ context.Context, _ dataapi.Connection, sql string, _ map[string]string) ([]dataapi.Row, error) {
				if strings.HasPrefix(sql, "CREATE EXTERNAL SCHEMA") {
					assert.Contains(t, sql, " REGION 'eu-central-1'")
					return nil, nil
				}
				return []dataapi.Row{{"schemaname": "example_external", "eskind": "1", "databasename": "glue", "esoptions": `{"IAM_ROLE":"role","` + key + `":"eu-central-1"}`, "owner": "admin"}}, nil
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

// TestExternalSchemaRejectsIncompatibleCatalog rejects unsupported kinds, a missing owner, and malformed options.
func TestExternalSchemaRejectsIncompatibleCatalog(t *testing.T) {
	for _, row := range []dataapi.Row{
		{"schemaname": "example_external", "eskind": "6", "owner": "admin", "esoptions": "{}"},
		{"schemaname": "example_external", "eskind": "1", "esoptions": `{"IAM_ROLE":"role"}`},
		{"schemaname": "example_external", "eskind": "1", "owner": "admin", "esoptions": "broken"},
		{"schemaname": "example_external", "eskind": "1", "owner": "admin", "esoptions": "{}"},
		{"schemaname": "example_external", "eskind": "3", "owner": "admin", "esoptions": `{"PORT":"many"}`},
	} {
		r := &externalSchemaResource{testResourceClient(queryFunc(func(context.Context, dataapi.Connection, string, map[string]string) ([]dataapi.Row, error) {
			return []dataapi.Row{row}, nil
		}))}
		data := externalSchemaModel{Database: types.StringValue("admin"), Name: types.StringValue("example_external"), GlueDatabase: types.StringValue("glue"), IAMRoleARN: types.StringValue("role")}
		_, err := r.read(context.Background(), &data)
		require.Error(t, err)
	}
}

// TestExternalSchemaCreationErrorsRetainKnownState keeps a known identity when readback fails after a successful CREATE.
func TestExternalSchemaCreationErrorsRetainKnownState(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		for _, region := range []types.String{types.StringUnknown(), types.StringValue("eu-central-1")} {
			t.Run(fmt.Sprintf("unavailable=%t/region=%s", unavailable, region), func(t *testing.T) {
				r := &externalSchemaResource{creationOnlyClient(unavailable)}
				state := testState(t, r, externalSchemaModel{Database: types.StringValue("admin"), Name: types.StringValue("raw"), GlueDatabase: types.StringValue("glue"), IAMRoleARN: types.StringValue("role"), Region: region})
				resp := resource.CreateResponse{State: tfsdk.State{Schema: state.Schema}}
				r.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan(state)}, &resp)
				require.True(t, resp.Diagnostics.HasError())
				var observed externalSchemaModel
				require.False(t, resp.State.Get(context.Background(), &observed).HasError())
				assert.False(t, observed.ID.IsNull())
				assert.True(t, resp.State.Raw.IsFullyKnown())
				if !region.IsUnknown() {
					assert.Equal(t, region, observed.Region)
				}
			})
		}
	}
}
