package provider

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerLifecycleCase(lifecycleCase{
	name: "masking policy attachment", kind: lifecyclePermission, new: newMaskingPolicyAttachmentResource, model: maskingTestAttachment(nil),
	absent: func(c *catalog) { fakeState[*maskingFake](c, maskingFakeFamily).attached = false },
	prepare: func(c *catalog, operation string) {
		if operation == "update" {
			// A priority changed outside Terraform shows the DETACH-then-ATTACH repair.
			fakeState[*maskingFake](c, maskingFakeFamily).priority = 5
		}
	},
})

var _ = registerReplacementPolicy("redshift_masking_policy_attachment", map[string]replaceRule{
	"database":      replaceAlways,
	"policy":        replaceAlways,
	"schema":        replaceAlways,
	"relation":      replaceAlways,
	"columns":       replaceAlways,
	"input_columns": replaceAlways,
	"grantee":       replaceAlways,
	"grantee_type":  replaceAlways,
	"priority":      replaceNever,
})

var _ = registerValidateConfigCase("masking_policy_attachment", validateConfigCase{
	new:   newMaskingPolicyAttachmentResource,
	valid: maskingTestAttachment(nil),
	invalid: maskingTestAttachment(func(m *maskingPolicyAttachmentModel) {
		m.GranteeType = types.StringValue("PUBLIC")
	}),
	unknown: maskingTestAttachment(func(m *maskingPolicyAttachmentModel) {
		m.Grantee, m.GranteeType = types.StringUnknown(), types.StringValue("PUBLIC")
	}),
})

// TestMaskingPolicyAttachmentTranscripts records the recipient forms, an explicit input mapping, and import.
func TestMaskingPolicyAttachmentTranscripts(t *testing.T) {
	detached := func(c *catalog) { fakeState[*maskingFake](c, maskingFakeFamily).attached = false }
	using := maskingTestAttachment(func(m *maskingPolicyAttachmentModel) { m.InputColumns = maskingAttachmentNameList([]string{"email"}) })
	unchanged := maskingTestAttachment(nil)
	runTranscripts(t, "masking/attachment_flows", newMaskingPolicyAttachmentResource, []transcriptCase{
		{name: "create_using", operation: "create", catalog: catalogWith(detached), planned: using},
		{name: "update_unchanged", operation: "update", catalog: catalogWith(), prior: unchanged, planned: unchanged},
		{name: "delete_missing", operation: "delete", catalog: catalogWith(detached), prior: unchanged},
	})
}

// maskingAttachmentClient answers the attachment read with rows and accepts ATTACH and DETACH.
func maskingAttachmentClient(rows ...sqlclient.Row) sqlclient.Client {
	return queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
		switch {
		case strings.Contains(sql, "FROM svv_attached_masking_policy"):
			return rows, nil
		case strings.HasPrefix(sql, "ATTACH "), strings.HasPrefix(sql, "DETACH "):
			return nil, nil
		}
		return nil, fmt.Errorf("unexpected SQL %q", sql)
	})
}

// maskingAttachmentRowWith is the catalog row of the test attachment with the given priority and inputs.
func maskingAttachmentRowWith(priority, inputs string) sqlclient.Row {
	return sqlclient.Row{"policy_name": "mask_email", "schema_name": "public", "table_name": "customers", "grantee": "example_readers", "grantee_type": "role", "priority": priority, "input_columns": inputs, "output_columns": `["email"]`}
}

// TestMaskingPolicyAttachmentConvergence fails Create and Update when the catalog does not hold the planned
// attachment, and keeps the configured spelling of folded input names.
func TestMaskingPolicyAttachmentConvergence(t *testing.T) {
	using := maskingTestAttachment(func(m *maskingPolicyAttachmentModel) { m.InputColumns = maskingAttachmentNameList([]string{"Email"}) })
	for name, test := range map[string]struct {
		row       sqlclient.Row
		operation string
		model     maskingPolicyAttachmentModel
		fails     bool
	}{
		"stuck priority":       {maskingAttachmentRowWith("5", `["email"]`), "update", maskingTestAttachment(nil), true},
		"wrong priority":       {maskingAttachmentRowWith("3", `["email"]`), "create", maskingTestAttachment(nil), true},
		"diverging inputs":     {maskingAttachmentRowWith("10", `["phone"]`), "create", using, true},
		"malformed inputs":     {maskingAttachmentRowWith("10", `email`), "read", using, true},
		"malformed priority":   {maskingAttachmentRowWith("high", `["email"]`), "read", using, true},
		"folded input names":   {maskingAttachmentRowWith("10", `["email"]`), "create", using, false},
		"inputs from columns":  {maskingAttachmentRowWith("10", `["email"]`), "create", maskingTestAttachment(nil), false},
		"converged unchanged":  {maskingAttachmentRowWith("10", `["email"]`), "update", using, false},
		"refresh reads values": {maskingAttachmentRowWith("10", `["email"]`), "read", maskingTestAttachment(nil), false},
	} {
		t.Run(name, func(t *testing.T) {
			r := newMaskingPolicyAttachmentResource()
			configureTestResource(t, r, maskingAttachmentClient(test.row))
			state, diagnostics := applyOperation(t, r, test.operation, test.model, test.model, nil)
			require.Equal(t, test.fails, diagnostics.HasError(), "%v", diagnostics)
			if test.fails {
				return
			}
			var observed maskingPolicyAttachmentModel
			require.False(t, state.Get(context.Background(), &observed).HasError())
			assert.Equal(t, int64(10), observed.Priority.ValueInt64())
			expected := []string{"email"}
			if !test.model.InputColumns.IsNull() {
				expected = maskingAttachmentNames(test.model.InputColumns)
			}
			assert.Equal(t, expected, maskingAttachmentNames(observed.InputColumns))
		})
	}
}

// TestMaskingPolicyAttachmentCreateRejectsInvalidBeforeState keeps invalid tuples out of state and SQL.
func TestMaskingPolicyAttachmentCreateRejectsInvalidBeforeState(t *testing.T) {
	r := newMaskingPolicyAttachmentResource()
	configureTestResource(t, r, queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
		return nil, fmt.Errorf("unexpected SQL %q", sql)
	}))
	model := maskingTestAttachment(func(m *maskingPolicyAttachmentModel) { m.GranteeType = types.StringValue("PUBLIC") })
	resp := resource.CreateResponse{State: emptyState(t, r)}
	r.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan(testState(t, r, model))}, &resp)
	require.True(t, resp.Diagnostics.HasError())
	assert.True(t, resp.State.Raw.IsNull(), "an invalid attachment must not be recorded in state")
}

// TestMaskingPolicyAttachmentImport restores the column list from the JSON identity and rejects malformed ones.
func TestMaskingPolicyAttachmentImport(t *testing.T) {
	r := newMaskingPolicyAttachmentResource()
	configureTestResource(t, r, maskingAttachmentClient())
	id := r.(*maskingPolicyAttachmentResource).identity("analytics", maskingAttachmentIdentity(maskingTestAttachment(nil))).ValueString()
	for candidate, valid := range map[string]bool{
		id: true,
		`{"workgroup_name":"warehouse","database":"analytics","policy":"p","schema":"s","relation":"r","grantee":"g","grantee_type":"ROLE","columns":"email"}`: false,
		`{"workgroup_name":"warehouse","database":"analytics","policy":"p","schema":"s","relation":"r","grantee":"g","grantee_type":"ROLE","columns":"[]"}`:    false,
		`{"workgroup_name":"warehouse","database":"analytics","policy":"p","schema":"s","relation":"r","grantee":"g","columns":"[\"email\"]"}`:                 false,
	} {
		resp := resource.ImportStateResponse{State: emptyState(t, r)}
		r.(resource.ResourceWithImportState).ImportState(context.Background(), resource.ImportStateRequest{ID: candidate}, &resp)
		require.Equal(t, valid, !resp.Diagnostics.HasError(), "%s: %v", candidate, resp.Diagnostics)
		if valid {
			var columns types.List
			require.False(t, resp.State.GetAttribute(context.Background(), path.Root("columns"), &columns).HasError())
			assert.Equal(t, []string{"email"}, maskingAttachmentNames(columns))
		}
	}
}

// TestMaskingPolicyAttachmentPriorityUpdateSafety keeps the column masked when a new priority cannot be attached: a
// priority another policy holds on the column fails before the DETACH, and a failed ATTACH re-attaches the policy
// with the priority and inputs the catalog held.
func TestMaskingPolicyAttachmentPriorityUpdateSafety(t *testing.T) {
	prior := maskingTestAttachment(func(m *maskingPolicyAttachmentModel) { m.Priority = types.Int64Value(5) })
	planned := maskingTestAttachment(func(m *maskingPolicyAttachmentModel) { m.Priority = types.Int64Value(20) })
	ownRow := maskingAttachmentRowWith("5", `["email"]`)
	competitor := sqlclient.Row{"policy_name": "mask_partial", "schema_name": "public", "table_name": "customers", "grantee": "analyst", "grantee_type": "user", "priority": "20", "input_columns": `["email"]`, "output_columns": `["email"]`}
	restore := `ATTACH MASKING POLICY "mask_email" ON "public"."customers" ("email") USING ("email") TO ROLE "example_readers" PRIORITY 5`
	for name, test := range map[string]struct {
		peers []sqlclient.Row
		// failures is how many ATTACH statements fail, starting with the first.
		failures int
		writes   []string
		message  string
	}{
		"conflicting priority": {
			peers: []sqlclient.Row{ownRow, competitor}, message: `masking policy "mask_partial" is attached to column "email" with priority 20`,
		},
		"failed attach restores": {
			peers: []sqlclient.Row{ownRow}, failures: 1, message: "the previous attachment with priority 5 was restored",
			writes: []string{
				`DETACH MASKING POLICY "mask_email" ON "public"."customers" ("email") FROM ROLE "example_readers"`,
				`ATTACH MASKING POLICY "mask_email" ON "public"."customers" ("email") TO ROLE "example_readers" PRIORITY 20`,
				restore,
			},
		},
		"failed restore": {
			peers: []sqlclient.Row{ownRow}, failures: 2, message: "restoring the previous attachment also failed",
			writes: []string{
				`DETACH MASKING POLICY "mask_email" ON "public"."customers" ("email") FROM ROLE "example_readers"`,
				`ATTACH MASKING POLICY "mask_email" ON "public"."customers" ("email") TO ROLE "example_readers" PRIORITY 20`,
				restore,
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			var writes []string
			attaches := 0
			r := newMaskingPolicyAttachmentResource()
			configureTestResource(t, r, queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
				switch {
				case strings.Contains(sql, "FROM svv_attached_masking_policy") && parameters["policy"] != "":
					return []sqlclient.Row{ownRow}, nil
				case strings.Contains(sql, "FROM svv_attached_masking_policy"):
					return test.peers, nil
				case strings.HasPrefix(sql, "ATTACH "):
					writes = append(writes, sql)
					attaches++
					if attaches <= test.failures {
						return nil, fmt.Errorf("injected ATTACH failure %d", attaches)
					}
					return nil, nil
				case strings.HasPrefix(sql, "DETACH "):
					writes = append(writes, sql)
					return nil, nil
				}
				return nil, fmt.Errorf("unexpected SQL %q", sql)
			}))
			_, diagnostics := applyOperation(t, r, "update", prior, planned, nil)
			require.True(t, diagnostics.HasError())
			assert.Contains(t, fmt.Sprint(diagnostics), test.message)
			assert.Equal(t, test.writes, writes, "a conflicting priority must not detach the policy")
		})
	}
}
