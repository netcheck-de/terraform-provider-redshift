package provider

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSchemaCreationErrorsRetainKnownState covers failed readback after successful local and external schema creation.
func TestSchemaCreationErrorsRetainKnownState(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		t.Run(fmt.Sprintf("unavailable=%t", unavailable), func(t *testing.T) {
			client := testResourceClient(queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
				if strings.HasPrefix(sql, "CREATE ") {
					return nil, nil
				}
				if unavailable {
					return nil, fmt.Errorf("catalog unavailable")
				}
				return nil, nil
			}))
			r := &schemaResource{client}
			state := testState(t, r, schemaModel{Database: types.StringValue("admin"), Name: types.StringValue("serving"), Owner: types.StringUnknown()})
			resp := resource.CreateResponse{State: tfsdk.State{Schema: state.Schema}}
			r.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan(state)}, &resp)
			require.True(t, resp.Diagnostics.HasError())
			var observed schemaModel
			require.False(t, resp.State.Get(context.Background(), &observed).HasError())
			assert.False(t, observed.ID.IsNull())
			assert.True(t, observed.Owner.IsNull())
			assert.True(t, resp.State.Raw.IsFullyKnown())

			for _, region := range []types.String{types.StringUnknown(), types.StringValue("eu-central-1")} {
				external := &externalSchemaResource{client}
				state := testState(t, external, externalSchemaModel{Database: types.StringValue("admin"), Name: types.StringValue("raw"), GlueDatabase: types.StringValue("glue"), IAMRoleARN: types.StringValue("role"), Region: region})
				resp := resource.CreateResponse{State: tfsdk.State{Schema: state.Schema}}
				external.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan(state)}, &resp)
				require.True(t, resp.Diagnostics.HasError())
				var observed externalSchemaModel
				require.False(t, resp.State.Get(context.Background(), &observed).HasError())
				assert.False(t, observed.ID.IsNull())
				assert.True(t, resp.State.Raw.IsFullyKnown())
				if !region.IsUnknown() {
					assert.Equal(t, region, observed.Region)
				}
			}
		})
	}
}
