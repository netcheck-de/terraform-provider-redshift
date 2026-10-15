package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rlsPolicyAttachmentLifecycleModel attaches the fake's policy to its relation for a role.
func rlsPolicyAttachmentLifecycleModel() rlsPolicyAttachmentModel {
	data := rlsPolicyAttachmentSample()
	data.Database = types.StringValue("admin")
	return data
}

var (
	_ = registerLifecycleCase(lifecycleCase{
		name: "rls_policy_attachment", kind: lifecyclePermission, new: newRlsPolicyAttachmentResource, model: rlsPolicyAttachmentLifecycleModel(),
		absent: func(c *catalog) { rlsFakeOf(c).attached = false },
	})
	_ = registerReplacementPolicy("redshift_rls_policy_attachment", map[string]replaceRule{
		"policy":       replaceAlways,
		"database":     replaceAlways,
		"schema":       replaceAlways,
		"relation":     replaceAlways,
		"grantee":      replaceAlways,
		"grantee_type": replaceAlways,
	})
	_ = registerValidateConfigCase("rls_policy_attachment", validateConfigCase{
		new:   newRlsPolicyAttachmentResource,
		valid: rlsPolicyAttachmentLifecycleModel(),
		invalid: func() rlsPolicyAttachmentModel {
			data := rlsPolicyAttachmentLifecycleModel()
			data.GranteeType = types.StringValue("PUBLIC")
			return data
		}(),
		unknown: func() rlsPolicyAttachmentModel {
			data := rlsPolicyAttachmentLifecycleModel()
			data.GranteeType, data.Grantee = types.StringValue("PUBLIC"), types.StringUnknown()
			return data
		}(),
	})
)

// TestRlsPolicyAttachmentTranscripts records the user and PUBLIC recipient forms.
func TestRlsPolicyAttachmentTranscripts(t *testing.T) {
	user := rlsPolicyAttachmentLifecycleModel()
	user.Grantee, user.GranteeType = types.StringValue("loader"), types.StringValue("USER")
	public := rlsPolicyAttachmentLifecycleModel()
	public.Grantee, public.GranteeType = types.StringValue("public"), types.StringValue("PUBLIC")
	detached := catalogWith(func(c *catalog) { rlsFakeOf(c).attached = false })
	attached := catalogWith()
	runTranscripts(t, "rls_transcripts/rls_policy_attachment", newRlsPolicyAttachmentResource, []transcriptCase{
		{name: "create_user", operation: "create", catalog: detached, planned: user},
		{name: "delete_user", operation: "delete", catalog: attached, prior: user},
		{name: "create_public", operation: "create", catalog: detached, planned: public},
		{name: "read_public", operation: "read", catalog: attached, prior: public},
		{name: "delete_public", operation: "delete", catalog: attached, prior: public},
		{name: "import_public", operation: "import", catalog: detached, planned: public},
	})
}

// TestRlsPolicyAttachmentNeverEnablesRls keeps the relation's switch with redshift_table_security.
func TestRlsPolicyAttachmentNeverEnablesRls(t *testing.T) {
	c := fullCatalog()
	f := rlsFakeOf(c)
	f.attached, f.rlsOn = false, false
	r := newRlsPolicyAttachmentResource()
	configureTestResource(t, r, c)
	_, diagnostics := applyOperation(t, r, "create", nil, rlsPolicyAttachmentLifecycleModel(), nil)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.True(t, f.attached)
	assert.False(t, f.rlsOn, "attaching must not turn row-level security on")
	f.rlsOn = true
	_, diagnostics = applyOperation(t, r, "delete", rlsPolicyAttachmentLifecycleModel(), nil, nil)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.False(t, f.attached)
	assert.True(t, f.rlsOn, "detaching must not turn row-level security off")
	assert.True(t, f.policy)
}

// TestRlsPolicyAttachmentRejectsInvalidTuplesBeforeSQL keeps invalid recipients out of SQL and state.
func TestRlsPolicyAttachmentRejectsInvalidTuplesBeforeSQL(t *testing.T) {
	r := newRlsPolicyAttachmentResource()
	configureTestResource(t, r, queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
		return nil, errors.New("no SQL expected")
	}))
	invalid := rlsPolicyAttachmentLifecycleModel()
	invalid.GranteeType = types.StringValue("GROUP")
	state, diagnostics := applyOperation(t, r, "create", nil, invalid, nil)
	require.True(t, diagnostics.HasError())
	assert.True(t, state.Raw.IsNull())
}

// TestRlsPolicyAttachmentIdentity pins the JSON identity Create records and import accepts.
func TestRlsPolicyAttachmentIdentity(t *testing.T) {
	c := fullCatalog()
	rlsFakeOf(c).attached = false
	r := newRlsPolicyAttachmentResource()
	configureTestResource(t, r, c)
	state, diagnostics := applyOperation(t, r, "create", nil, rlsPolicyAttachmentLifecycleModel(), nil)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	var data rlsPolicyAttachmentModel
	require.False(t, state.Get(context.Background(), &data).HasError())
	assertLookupIdentity(t, data.ID, "admin", map[string]string{"policy": "region_filter", "schema": "public", "relation": "events", "grantee": "analysts", "grantee_type": "ROLE"})
}
