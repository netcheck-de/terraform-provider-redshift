package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// maskingTestAttachment builds an attachment of mask_email to public.customers(email) for a role at priority 10.
func maskingTestAttachment(change func(*maskingPolicyAttachmentModel)) maskingPolicyAttachmentModel {
	model := maskingPolicyAttachmentModel{
		ID: types.StringNull(), Database: types.StringValue("admin"), Policy: types.StringValue("mask_email"),
		Schema: types.StringValue("public"), Relation: types.StringValue("customers"), Columns: maskingAttachmentNameList([]string{"email"}),
		InputColumns: types.ListNull(types.StringType), Grantee: types.StringValue("example_readers"), GranteeType: types.StringValue("ROLE"),
		Priority: types.Int64Value(10),
	}
	if change != nil {
		change(&model)
	}
	return model
}

// TestMaskingPolicyAttachmentSQL pins ATTACH and DETACH MASKING POLICY for every recipient form, the priority
// change, and the catalog read against r_ATTACH_MASKING_POLICY, r_DETACH_MASKING_POLICY, and
// r_SVV_ATTACHED_MASKING_POLICY.
func TestMaskingPolicyAttachmentSQL(t *testing.T) {
	role := maskingTestAttachment(nil)
	user := maskingTestAttachment(func(m *maskingPolicyAttachmentModel) {
		m.Policy, m.Relation = types.StringValue("card_number_conditional_mask"), types.StringValue("credit_cards")
		m.Columns, m.InputColumns = maskingAttachmentNameList([]string{"credit_card_number"}), maskingAttachmentNameList([]string{"is_fraud", "credit_card_number"})
		m.Grantee, m.GranteeType, m.Priority = types.StringValue("analyst"), types.StringValue("USER"), types.Int64Value(100)
	})
	public := maskingTestAttachment(func(m *maskingPolicyAttachmentModel) {
		m.Grantee, m.GranteeType, m.Priority = types.StringValue("public"), types.StringValue("PUBLIC"), types.Int64Value(0)
	})
	quoted := maskingTestAttachment(func(m *maskingPolicyAttachmentModel) {
		m.Policy, m.Schema, m.Relation = types.StringValue(`Odd"Policy`), types.StringValue(`Odd"Schema`), types.StringValue(`Odd"Table`)
		m.Columns, m.InputColumns = maskingAttachmentNameList([]string{`Odd"Column`, "second"}), maskingAttachmentNameList([]string{`It's\Input`, "second"})
		m.Grantee = types.StringValue(`Odd"Role`)
	})
	unknownPriority := maskingTestAttachment(func(m *maskingPolicyAttachmentModel) { m.Priority = types.Int64Unknown() })
	reprioritized := maskingTestAttachment(func(m *maskingPolicyAttachmentModel) { m.Priority = types.Int64Value(20) })
	invalid := func(change func(*maskingPolicyAttachmentModel)) func() (string, error) {
		return func() (string, error) { return attachMaskingPolicyStatement(maskingTestAttachment(change)) }
	}
	checkSQL(t, "masking_policy_attachment", []sqlCase{
		{"attach_role", func() (string, error) { return attachMaskingPolicyStatement(role) }},
		{"attach_user_using", func() (string, error) { return attachMaskingPolicyStatement(user) }},
		{"attach_public", func() (string, error) { return attachMaskingPolicyStatement(public) }},
		{"attach_quoted", func() (string, error) { return attachMaskingPolicyStatement(quoted) }},
		{"attach_unknown_priority", func() (string, error) { return attachMaskingPolicyStatement(unknownPriority) }},
		{"detach_role", func() (string, error) { return detachMaskingPolicyStatement(role) }},
		{"detach_user", func() (string, error) { return detachMaskingPolicyStatement(user) }},
		{"detach_public", func() (string, error) { return detachMaskingPolicyStatement(public) }},
		{"detach_quoted", func() (string, error) { return detachMaskingPolicyStatement(quoted) }},
		{"alter_priority", func() ([]string, error) { return alterMaskingAttachmentStatements(role, reprioritized) }},
		{"alter_unchanged", func() ([]string, error) { return alterMaskingAttachmentStatements(role, role) }},
		{"alter_invalid_plan", func() ([]string, error) {
			return alterMaskingAttachmentStatements(role, maskingTestAttachment(func(m *maskingPolicyAttachmentModel) { m.Priority = types.Int64Value(-1) }))
		}},
		{"alter_invalid_current", func() ([]string, error) {
			return alterMaskingAttachmentStatements(maskingTestAttachment(func(m *maskingPolicyAttachmentModel) { m.GranteeType = types.StringValue("GROUP") }), reprioritized)
		}},
		{"read", maskingQuerySQL(readMaskingAttachmentQuery(quoted))},
		{"read_peers", maskingQuerySQL(maskingAttachmentPeersQuery(quoted))},
		// A failed re-attach restores the attachment Update read from the catalog, which reports explicit inputs.
		{"restore_after_failed_attach", func() (string, error) {
			return attachMaskingPolicyStatement(maskingTestAttachment(func(m *maskingPolicyAttachmentModel) {
				m.InputColumns, m.Priority = maskingAttachmentNameList([]string{"email"}), types.Int64Value(5)
			}))
		}},
		{"error_public_name", invalid(func(m *maskingPolicyAttachmentModel) { m.GranteeType = types.StringValue("PUBLIC") })},
		{"error_group", invalid(func(m *maskingPolicyAttachmentModel) { m.GranteeType = types.StringValue("GROUP") })},
		{"error_no_columns", invalid(func(m *maskingPolicyAttachmentModel) { m.Columns = maskingAttachmentNameList(nil) })},
		{"error_null_columns", invalid(func(m *maskingPolicyAttachmentModel) { m.Columns = types.ListNull(types.StringType) })},
		{"error_duplicate_columns", invalid(func(m *maskingPolicyAttachmentModel) {
			m.Columns = maskingAttachmentNameList([]string{"email", "EMAIL"})
		})},
		{"error_empty_input", invalid(func(m *maskingPolicyAttachmentModel) { m.InputColumns = maskingAttachmentNameList([]string{""}) })},
		{"error_negative_priority", invalid(func(m *maskingPolicyAttachmentModel) { m.Priority = types.Int64Value(-1) })},
		{"error_empty_relation", invalid(func(m *maskingPolicyAttachmentModel) { m.Relation = types.StringValue("") })},
		{"error_detach_group", func() (string, error) {
			return detachMaskingPolicyStatement(maskingTestAttachment(func(m *maskingPolicyAttachmentModel) { m.GranteeType = types.StringValue("GROUP") }))
		}},
	})
}

// TestMaskingAttachmentRowSelection matches the recipient and output columns of catalog rows.
func TestMaskingAttachmentRowSelection(t *testing.T) {
	rows := []sqlclient.Row{
		{"grantee": "other", "grantee_type": "role", "output_columns": `["email"]`, "priority": "5"},
		{"grantee": "example_readers", "grantee_type": "role", "output_columns": `["phone"]`, "priority": "6"},
		{"grantee": "Example_Readers", "grantee_type": "role", "output_columns": `["EMAIL"]`, "priority": "7"},
		{"grantee": "public", "grantee_type": "public", "output_columns": `["email"]`, "priority": "8"},
	}
	row, err := maskingAttachmentRow(maskingTestAttachment(nil), rows)
	require.NoError(t, err)
	assert.Equal(t, "7", row["priority"])
	row, err = maskingAttachmentRow(maskingTestAttachment(func(m *maskingPolicyAttachmentModel) {
		m.Grantee, m.GranteeType = types.StringValue("public"), types.StringValue("PUBLIC")
	}), rows)
	require.NoError(t, err)
	assert.Equal(t, "8", row["priority"])
	row, err = maskingAttachmentRow(maskingTestAttachment(func(m *maskingPolicyAttachmentModel) { m.GranteeType = types.StringValue("USER") }), rows)
	require.NoError(t, err)
	assert.Nil(t, row)
	_, err = maskingAttachmentRow(maskingTestAttachment(nil), []sqlclient.Row{{"grantee": "example_readers", "grantee_type": "role", "output_columns": "email"}})
	require.Error(t, err)
	_, err = maskingAttachmentPriority("high")
	require.Error(t, err)
	assert.JSONEq(t, `["email"]`, maskingAttachmentIdentity(maskingTestAttachment(nil))["columns"])
}

// TestMaskingAttachmentPriorityConflict finds another policy holding the planned priority on a planned column, which
// ATTACH would reject after the update had already detached the policy.
func TestMaskingAttachmentPriorityConflict(t *testing.T) {
	peer := func(policy, priority, outputs string) sqlclient.Row {
		return sqlclient.Row{"policy_name": policy, "grantee": "analyst", "grantee_type": "user", "priority": priority, "output_columns": outputs}
	}
	plan := maskingTestAttachment(func(m *maskingPolicyAttachmentModel) {
		m.Columns, m.Priority = maskingAttachmentNameList([]string{"email", "phone"}), types.Int64Value(20)
	})
	for name, test := range map[string]struct {
		rows     []sqlclient.Row
		conflict string
	}{
		"no peers":                      {nil, ""},
		"same policy, other recipient":  {[]sqlclient.Row{peer("MASK_EMAIL", "20", `["email"]`)}, ""},
		"other priority":                {[]sqlclient.Row{peer("mask_phone", "30", `["phone"]`)}, ""},
		"other column":                  {[]sqlclient.Row{peer("mask_name", "20", `["name"]`)}, ""},
		"equal priority, shared column": {[]sqlclient.Row{peer("mask_phone", "30", `["phone"]`), peer("mask_phone", "20", `["name","PHONE"]`)}, `masking policy "mask_phone" is attached to column "PHONE" with priority 20 for user "analyst"`},
		"malformed priority":            {[]sqlclient.Row{peer("mask_phone", "high", `["phone"]`)}, "unexpected masking policy priority"},
		"malformed columns":             {[]sqlclient.Row{peer("mask_phone", "20", "phone")}, "unexpected attached masking policy columns"},
	} {
		t.Run(name, func(t *testing.T) {
			err := maskingAttachmentPriorityConflict(plan, test.rows)
			if test.conflict == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), test.conflict)
		})
	}
	unknown := plan
	unknown.Priority = types.Int64Unknown()
	require.NoError(t, maskingAttachmentPriorityConflict(unknown, []sqlclient.Row{peer("mask_phone", "high", `["phone"]`)}), "an unknown priority is checked at apply time")
}
