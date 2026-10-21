package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCoveragePoliciesUnavailableDatabase(t *testing.T) {
	for _, failure := range []bool{false, true} {
		checkError := func(err error) {
			t.Helper()
			if failure {
				require.ErrorContains(t, err, "database catalog unavailable")
			} else {
				require.NoError(t, err)
			}
		}
		client := testResourceClient(queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
			if failure {
				return nil, errors.New("database catalog unavailable")
			}
			return nil, nil
		}))
		ctx := context.Background()
		masking := maskingLifecyclePolicy
		masking.Database = types.StringValue("analytics")
		_, found, err := (&maskingPolicyResource{resourceClient: client}).read(ctx, &masking)
		assert.False(t, found)
		checkError(err)
		rls := rlsPolicyLifecycleModel()
		rls.Database = types.StringValue("analytics")
		found, _, err = (&rlsPolicyResource{resourceClient: client}).read(ctx, &rls)
		assert.False(t, found)
		checkError(err)
		attachment := maskingTestAttachment(nil)
		attachment.Database = types.StringValue("analytics")
		found, err = (&maskingPolicyAttachmentResource{resourceClient: client}).read(ctx, &attachment)
		assert.False(t, found)
		checkError(err)
		rlsAttachment := rlsPolicyAttachmentLifecycleModel()
		rlsAttachment.Database = types.StringValue("analytics")
		found, err = (&rlsPolicyAttachmentResource{resourceClient: client}).read(ctx, rlsAttachment)
		assert.False(t, found)
		checkError(err)
	}
}

func TestCoveragePoliciesKnownAttachmentOptions(t *testing.T) {
	for _, test := range []struct {
		name    string
		change  func(*maskingPolicyAttachmentModel)
		message string
	}{
		{"database", func(m *maskingPolicyAttachmentModel) { m.Database = types.StringValue("") }, "database must not be empty"},
		{"recipient", func(m *maskingPolicyAttachmentModel) { m.GranteeType = types.StringValue("GROUP") }, "grantee_type"},
		{"missing columns", func(m *maskingPolicyAttachmentModel) { m.Columns = types.ListNull(types.StringType) }, "columns needs"},
		{"empty columns", func(m *maskingPolicyAttachmentModel) { m.Columns = types.ListValueMust(types.StringType, nil) }, "columns needs"},
		{"priority", func(m *maskingPolicyAttachmentModel) { m.Priority = types.Int64Value(-1) }, "priority must not be negative"},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.ErrorContains(t, maskingAttachmentKnownConfig(maskingTestAttachment(test.change)), test.message)
		})
	}
	rls := rlsPolicyAttachmentLifecycleModel()
	rls.Policy = types.StringValue("")
	require.ErrorContains(t, rlsPolicyAttachmentKnownConfig(rls), "nonempty")
	rls = rlsPolicyAttachmentLifecycleModel()
	rls.GranteeType = types.StringUnknown()
	require.NoError(t, rlsPolicyAttachmentKnownConfig(rls))
	rls.GranteeType = types.StringValue("GROUP")
	require.ErrorContains(t, rlsPolicyAttachmentKnownConfig(rls), "not one of")
}

func TestCoveragePoliciesPartialInputs(t *testing.T) {
	masking := maskingLifecyclePolicy
	masking.Expression = types.StringUnknown()
	masking.InputColumn = maskingPolicyColumnList([]maskingPolicyColumn{{Name: "value", Type: "anyelement"}})
	rls := rlsPolicyLifecycleModel()
	rls.Predicate = types.StringUnknown()
	rls.Column = types.ListValueMust(rlsPolicyColumnType, []attr.Value{types.ObjectValueMust(rlsPolicyColumnType.AttrTypes, map[string]attr.Value{"name": types.StringValue("value"), "type": types.StringValue("money")})})
	for _, test := range []struct {
		r     resource.Resource
		model any
	}{
		{newMaskingPolicyResource(), masking}, {newRlsPolicyResource(), rls},
	} {
		var response resource.ValidateConfigResponse
		test.r.(resource.ResourceWithValidateConfig).ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config(testState(t, test.r, test.model))}, &response)
		require.True(t, response.Diagnostics.HasError(), "%v", response.Diagnostics)
	}
	unknown := types.ListValueMust(maskingPolicyColumnType, []attr.Value{types.ObjectUnknown(maskingPolicyColumnType.AttrTypes)})
	assert.Empty(t, maskingPolicyColumns(unknown))
	assert.Empty(t, rlsPolicyCatalogColumnsOf(types.ListUnknown(rlsPolicyColumnType)))
	assert.Empty(t, rlsPolicyCatalogColumnsOf(types.ListValueMust(rlsPolicyColumnType, []attr.Value{types.ObjectUnknown(rlsPolicyColumnType.AttrTypes)})))
	_, err := maskingPolicyInputs([]maskingPolicyColumn{{Name: "value", Type: "anyelement"}})
	require.ErrorContains(t, err, "input column")
	_, err = maskingPolicyUsing(types.StringValue("x; DROP TABLE t"))
	require.ErrorContains(t, err, "expression")
	ctx := context.Background()
	assert.Equal(t, (rlsPolicyColumnsEquivalent{}).Description(ctx), (rlsPolicyColumnsEquivalent{}).MarkdownDescription(ctx))
	assert.Equal(t, (rlsPolicyAliasEquivalent{}).Description(ctx), (rlsPolicyAliasEquivalent{}).MarkdownDescription(ctx))
}

func TestCoveragePoliciesGrantValidationBeforeOwnership(t *testing.T) {
	r := newPolicyGrantResource().(*policyGrantResource)
	configureTestResource(t, r, &policyGrantTestCatalog{})
	_, diagnostics := applyOperation(t, r, "create", nil, policyGrantObject(nil, "INSERT"), nil)
	require.True(t, diagnostics.HasError())
	invalidID := policyGrantObject(nil, "SELECT")
	lookupValue(&invalidID, "id", types.StringValue("invalid JSON"))
	_, found, err := r.parents(context.Background(), invalidID)
	require.ErrorContains(t, err, "invalid resource identity")
	assert.False(t, found)
	_, found, err = r.parents(context.Background(), policyGrantObject(map[string]string{"policy_type": "INVALID"}, "SELECT"))
	require.Error(t, err)
	assert.False(t, found)
	target, err := r.target(policyGrantObject(nil, "SELECT"))
	require.NoError(t, err)
	require.Error(t, r.revise(context.Background(), target, nil, []string{"INSERT"}))
}

func TestCoveragePoliciesUnknownInputsRetainedAfterAttach(t *testing.T) {
	r := &maskingPolicyAttachmentResource{resourceClient: creationOnlyClient(false)}
	model := maskingTestAttachment(func(m *maskingPolicyAttachmentModel) { m.InputColumns = types.ListUnknown(types.StringType) })
	state, diagnostics := applyOperation(t, r, "create", nil, model, nil)
	require.True(t, diagnostics.HasError())
	var actual maskingPolicyAttachmentModel
	require.False(t, state.Get(context.Background(), &actual).HasError())
	assert.False(t, actual.ID.IsNull())
	assert.True(t, actual.InputColumns.IsNull())
	assert.True(t, state.Raw.IsFullyKnown())
}

func TestCoveragePoliciesAttachmentLookupFailure(t *testing.T) {
	client := queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
		return nil, errors.New("attachment catalog unavailable")
	})
	_, diagnostics := readSource(t, newMaskingPolicyAttachmentDataSource(), maskingAttachmentLookupConfig(t, maskingTestAttachment(nil)), client)
	require.True(t, diagnostics.HasError())
	assert.Contains(t, diagnostics[0].Detail(), "attachment catalog unavailable")
}
