package provider

import (
	"context"
	"encoding/json"
	"maps"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLegacyIdentityRefresh exercises stored state for every resource family, including tuples whose
// catalogs cannot reconstruct their original identity. Only connection context may disappear from the ID.
func TestLegacyIdentityRefresh(t *testing.T) {
	ctx := context.Background()

	for _, test := range lifecycleCases() {
		t.Run(test.name, func(t *testing.T) {
			c := test.catalog()
			if test.absent != nil {
				test.absent(c)
			}

			test.applyPrepare(c, "create")
			r := test.new()
			configureTestResource(t, r, c)
			state, diagnostics := applyOperation(t, r, "create", nil, test.model, nil)
			require.False(t, diagnostics.HasError(), "%v", diagnostics)

			writes := len(c.writes)

			var id types.String
			require.False(t, state.GetAttribute(ctx, path.Root("id"), &id).HasError())

			var fields map[string]string
			require.NoError(t, json.Unmarshal([]byte(id.ValueString()), &fields))

			for _, field := range warehouseIdentityFields {
				assert.NotContains(t, fields, field)
			}

			for _, field := range warehouseIdentityFields {
				t.Run(field, func(t *testing.T) {
					legacy := maps.Clone(fields)

					legacy[field] = "warehouse"
					if legacy["database"] == "" {
						legacy["database"] = "admin"
					}

					encoded, err := json.Marshal(legacy)
					require.NoError(t, err)

					configured := resource.ConfigureResponse{}
					r.(resource.ResourceWithConfigure).Configure(ctx, resource.ConfigureRequest{ProviderData: providerData{
						client: c, warehouse: warehouseBinding{field: field, value: types.StringValue("warehouse")}, database: types.StringValue("admin"),
					}}, &configured)
					require.False(t, configured.Diagnostics.HasError(), "%v", configured.Diagnostics)

					prior := state
					require.False(t, prior.SetAttribute(ctx, path.Root("id"), string(encoded)).HasError())
					refreshed := resource.ReadResponse{State: prior}
					r.Read(ctx, resource.ReadRequest{State: prior}, &refreshed)
					require.False(t, refreshed.Diagnostics.HasError(), "%v", refreshed.Diagnostics)

					var normalized types.String
					require.False(t, refreshed.State.GetAttribute(ctx, path.Root("id"), &normalized).HasError())
					assert.Equal(t, id, normalized)

					imported := resource.ImportStateResponse{State: emptyState(t, r)}
					r.(resource.ResourceWithImportState).ImportState(ctx, resource.ImportStateRequest{ID: string(encoded)}, &imported)
					require.True(t, imported.Diagnostics.HasError(), "legacy IDs are supported only in existing state")
					assert.Contains(t, imported.Diagnostics.Errors()[0].Detail(), field)
				})
			}

			// Once normalized, the same tuple is usable through any configured connection method.
			refreshed := resource.ReadResponse{State: state}
			r.Read(ctx, resource.ReadRequest{State: state}, &refreshed)
			require.False(t, refreshed.Diagnostics.HasError(), "%v", refreshed.Diagnostics)

			var normalized types.String
			require.False(t, refreshed.State.GetAttribute(ctx, path.Root("id"), &normalized).HasError())
			assert.Equal(t, id, normalized)
			assert.Len(t, c.writes, writes, "refresh and rejected imports must not mutate SQL objects")
		})
	}
}
