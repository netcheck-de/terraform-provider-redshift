package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// rlsPolicyAttachmentSample is a representative role attachment.
func rlsPolicyAttachmentSample() rlsPolicyAttachmentModel {
	return rlsPolicyAttachmentModel{
		Policy: types.StringValue("region_filter"), Database: types.StringValue("analytics"), Schema: types.StringValue("public"),
		Relation: types.StringValue("events"), Grantee: types.StringValue("analysts"), GranteeType: types.StringValue("ROLE"),
	}
}

// TestRlsPolicyAttachmentSQL pins ATTACH/DETACH for every recipient form and the catalog read.
func TestRlsPolicyAttachmentSQL(t *testing.T) {
	with := func(change func(*rlsPolicyAttachmentModel)) rlsPolicyAttachmentModel {
		data := rlsPolicyAttachmentSample()
		change(&data)
		return data
	}
	user := with(func(d *rlsPolicyAttachmentModel) {
		d.Grantee, d.GranteeType = types.StringValue("loader"), types.StringValue("USER")
	})
	public := with(func(d *rlsPolicyAttachmentModel) {
		d.Grantee, d.GranteeType = types.StringValue("public"), types.StringValue("PUBLIC")
	})
	quoted := with(func(d *rlsPolicyAttachmentModel) {
		d.Policy, d.Schema, d.Relation, d.Grantee = types.StringValue(`Odd"Policy`), types.StringValue(`Odd"Schema`), types.StringValue(`Odd"Events`), types.StringValue(`Odd"Role`)
	})
	attach := func(data rlsPolicyAttachmentModel) func() (string, error) {
		return func() (string, error) { return createRlsPolicyAttachmentStatement(data) }
	}
	detach := func(data rlsPolicyAttachmentModel) func() (string, error) {
		return func() (string, error) { return dropRlsPolicyAttachmentStatement(data) }
	}
	read := func(data rlsPolicyAttachmentModel) func() (string, error) {
		return func() (string, error) {
			sql, _, err := readRlsPolicyAttachmentQuery(data).Build()
			return sql, err
		}
	}
	checkSQL(t, "rls_policy_attachment", []sqlCase{
		{"attach_role", attach(rlsPolicyAttachmentSample())},
		{"attach_user", attach(user)},
		{"attach_public", attach(public)},
		{"attach_quoted", attach(quoted)},
		{"attach_public_named", attach(with(func(d *rlsPolicyAttachmentModel) { d.GranteeType = types.StringValue("PUBLIC") }))},
		{"attach_group", attach(with(func(d *rlsPolicyAttachmentModel) { d.GranteeType = types.StringValue("GROUP") }))},
		{"attach_empty_grantee", attach(with(func(d *rlsPolicyAttachmentModel) { d.Grantee = types.StringValue("") }))},
		{"attach_empty_schema", attach(with(func(d *rlsPolicyAttachmentModel) { d.Schema = types.StringValue("") }))},
		{"detach_role", detach(rlsPolicyAttachmentSample())},
		{"detach_user", detach(user)},
		{"detach_public", detach(public)},
		{"detach_quoted", detach(quoted)},
		{"detach_empty_relation", detach(with(func(d *rlsPolicyAttachmentModel) { d.Relation = types.StringValue("") }))},
		{"read_role", read(quoted)},
		{"read_user", read(user)},
		{"read_public", read(public)},
	})
}
