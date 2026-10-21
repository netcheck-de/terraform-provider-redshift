package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCoverageExternalTablePartialValidation(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*externalTableModel)
		want   string
	}{
		{"serde delimiter conflict", func(m *externalTableModel) {
			m.Serde = types.StringValue("custom.Serde")
			m.FieldDelimiter = types.StringValue(",")
		}, "serde conflicts"},
		{"invalid delimiter", func(m *externalTableModel) { m.FieldDelimiter = types.StringValue("long delimiter") }, "delimiter"},
		{"invalid location", func(m *externalTableModel) { m.Location = types.StringValue("file:///local") }, "s3://"},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := externalTableTestModel()
			model.Columns = types.ListUnknown(externalTableColumnType)
			test.change(&model)
			r := &externalTableResource{}
			var response resource.ValidateConfigResponse
			r.ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config(testState(t, r, model))}, &response)
			require.True(t, response.Diagnostics.HasError())
			assert.Contains(t, response.Diagnostics.Errors()[0].Detail(), test.want)
		})
	}
}

func TestCoverageExternalTableCatalogEdges(t *testing.T) {
	ctx := context.Background()
	assert.Empty(t, recordExternalTableLayout(ctx, nil, nil))
	model := externalTableTestModel()
	model.SerdeProperties = externalTableTestMap("", "value")
	_, err := externalTableSpecFrom(model)
	require.ErrorContains(t, err, "nonempty names")
	layout := &externalTableLayout{Columns: []string{"missing"}}
	_, ok := layout.physical([]externalTableColumnValue{{Name: types.StringValue("id"), Type: types.StringValue("integer")}})
	assert.False(t, ok)
	prior := externalTableTestModel()
	observed := observeExternalTable(&externalTableCatalog{table: map[string]string{"tablename": "renamed", "location": "s3://bucket/path"}}, prior, externalTableLookup)
	assert.Equal(t, "renamed", observed.Name.ValueString())
	prior.StoredAs = types.StringNull()
	observed = prior
	prior.InputFormat = types.StringValue("custom.Input")
	observed.InputFormat = types.StringValue("different.Input")
	assert.Contains(t, externalTableDivergence(prior, observed), "input_format")
}
