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

func TestCoverageGrantsMembershipValidation(t *testing.T) {
	r := &datashareMembershipResource{}
	for _, test := range []struct {
		name    string
		change  func(*datashareMembershipModel)
		invalid bool
	}{
		{"valid fully known", func(*datashareMembershipModel) {}, false},
		{"empty database", func(m *datashareMembershipModel) { m.Database = types.StringValue("") }, true},
		{"empty share", func(m *datashareMembershipModel) { m.Datashare = types.StringValue("") }, true},
		{"empty schema", func(m *datashareMembershipModel) { m.Schema = types.StringValue("") }, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := datashareMembershipFixture("TABLES")
			data.ID = types.StringValue("known")
			test.change(&data)
			var response resource.ValidateConfigResponse
			r.ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config(testState(t, r, data))}, &response)
			assert.Equal(t, test.invalid, response.Diagnostics.HasError(), "%v", response.Diagnostics)
		})
	}
}

func TestCoverageGrantsMembershipInvalidOperations(t *testing.T) {
	r := &datashareMembershipResource{}
	data := datashareMembershipFixture("TABLES")
	data.All = types.BoolValue(false)
	require.ErrorContains(t, r.apply(context.Background(), &data), "all must be true")
	data.All = types.BoolValue(true)
	data.Kind = types.StringValue("PROCEDURES")
	require.ErrorContains(t, r.apply(context.Background(), &data), "kind must be")
	_, _, _, err := r.read(context.Background(), &data)
	require.ErrorContains(t, err, "kind must be")
}

func TestCoverageGrantsMembershipNonConvergence(t *testing.T) {
	c := fullCatalog()
	c.includeNew = false
	fakeState[*fakeDatashares](c, "datashare").bulkMembers["TABLES"] = nil
	r := &datashareMembershipResource{testResourceClient(c)}
	data := datashareMembershipFixture("TABLES")
	require.ErrorContains(t, r.verify(context.Background(), &data), "did not converge")
}

func TestCoverageGrantsMembershipReadWarnsAboutIncludeNew(t *testing.T) {
	c := fullCatalog()
	c.includeNew = true
	r := &datashareMembershipResource{testResourceClient(c)}
	state := testState(t, r, datashareMembershipFixture("TABLES"))
	response := resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, &response)
	require.False(t, response.Diagnostics.HasError(), "%v", response.Diagnostics)
	require.Len(t, response.Diagnostics.Warnings(), 1)
	assert.Contains(t, response.Diagnostics.Warnings()[0].Detail(), "include_new = false")
	assert.False(t, response.State.Raw.IsNull())
}
