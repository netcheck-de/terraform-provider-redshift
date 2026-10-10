package provider

import (
	"context"
	"maps"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerReplacementPolicy("redshift_language_grant", map[string]replaceRule{
	"database_name":           replaceAlways,
	"language_name":           replaceAlways,
	"grantee":                 replaceAlways,
	"grantee_type":            replaceAlways,
	"privileges":              replaceNever,
	"grant_option_privileges": replaceNever,
})

// languageGrantTestModel mirrors the language grant schema as a struct, because the shared lifecycle tests replace
// the ID field of their models by name.
type languageGrantTestModel struct {
	// ID is the JSON import identity.
	ID types.String `tfsdk:"id"`
	// DatabaseName is the database whose language privileges are managed.
	DatabaseName types.String `tfsdk:"database_name"`
	// LanguageName is sql or plpgsql.
	LanguageName types.String `tfsdk:"language_name"`
	// Grantee is the receiving identity.
	Grantee types.String `tfsdk:"grantee"`
	// GranteeType is ROLE, USER, GROUP, or PUBLIC.
	GranteeType types.String `tfsdk:"grantee_type"`
	// Privileges is the USAGE set.
	Privileges types.Set `tfsdk:"privileges"`
	// GrantOptionPrivileges is the subset held with grant option.
	GrantOptionPrivileges types.Set `tfsdk:"grant_option_privileges"`
}

// languageGrantModel builds a typed model from tuple fields, privileges, and grant options.
func languageGrantModel(fields map[string]string, privileges, options []string) languageGrantTestModel {
	set := func(values []string) types.Set {
		elements := make([]attr.Value, 0, len(values))
		for _, value := range values {
			elements = append(elements, types.StringValue(value))
		}
		return types.SetValueMust(types.StringType, elements)
	}
	return languageGrantTestModel{
		ID: types.StringNull(), DatabaseName: types.StringValue(fields["database_name"]), LanguageName: types.StringValue(fields["language_name"]),
		Grantee: types.StringValue(fields["grantee"]), GranteeType: types.StringValue(fields["grantee_type"]),
		Privileges: set(privileges), GrantOptionPrivileges: set(options),
	}
}

var _ = registerLifecycleCase(lifecycleCase{
	name: "language grant", kind: lifecyclePermission, new: newLanguageGrantResource,
	model: languageGrantModel(languageGrantBaseFields, []string{"USAGE"}, nil),
	// The language privileges live in another local database than admin, so the database check must find it.
	setup: func(c *catalog) { c.localDB = true },
	absent: func(c *catalog) {
		clear(fakeState[*languageGrantFake](c, "language_grant").usage)
	},
})

// TestLanguageGrantTranscripts records user grant options, quoted names with literal bindings, and every grantee kind.
func TestLanguageGrantTranscripts(t *testing.T) {
	user := maps.Clone(languageGrantBaseFields)
	user["language_name"], user["grantee"], user["grantee_type"] = "sql", `Odd"O'Reilly\User`, "USER"
	group := maps.Clone(languageGrantBaseFields)
	group["grantee"], group["grantee_type"] = "readers", "GROUP"
	public := maps.Clone(languageGrantBaseFields)
	public["language_name"], public["grantee"], public["grantee_type"] = "sql", "public", "PUBLIC"
	fresh := func(option *bool) func() sqlclient.Client {
		return func() sqlclient.Client {
			c := fullCatalog()
			c.localDB = true
			fake := fakeState[*languageGrantFake](c, "language_grant")
			clear(fake.usage)
			if option != nil {
				fake.usage[languageGrantFakeKey{"sql", user["grantee"], "user"}] = *option
			}
			return columnGrantClient(c)
		}
	}
	plain, withOption := false, true
	runTranscripts(t, "language_grant/transcript", newLanguageGrantResource, []transcriptCase{
		{name: "create_user_with_grant_option", operation: "create", catalog: fresh(nil), planned: languageGrantModel(user, []string{"USAGE"}, []string{"USAGE"})},
		{name: "upgrade_user_grant_option", operation: "update", catalog: fresh(&plain), prior: languageGrantModel(user, []string{"USAGE"}, nil), planned: languageGrantModel(user, []string{"USAGE"}, []string{"USAGE"})},
		{name: "downgrade_user_grant_option", operation: "update", catalog: fresh(&withOption), prior: languageGrantModel(user, []string{"USAGE"}, []string{"USAGE"}), planned: languageGrantModel(user, []string{"USAGE"}, nil)},
		{name: "delete_user_with_grant_option", operation: "delete", catalog: fresh(&withOption), prior: languageGrantModel(user, []string{"USAGE"}, []string{"USAGE"})},
		{name: "import_user", operation: "import", catalog: fresh(nil), planned: languageGrantModel(user, []string{"USAGE"}, nil)},
		{name: "create_group", operation: "create", catalog: fresh(nil), planned: languageGrantModel(group, []string{"USAGE"}, nil)},
		{name: "create_public", operation: "create", catalog: fresh(nil), planned: languageGrantModel(public, []string{"USAGE"}, nil)},
		{name: "create_public_without_usage", operation: "create", catalog: fresh(nil), planned: languageGrantModel(public, nil, nil)},
		{name: "delete_public_without_usage", operation: "delete", catalog: fresh(nil), prior: languageGrantModel(public, nil, nil)},
	})
}

// TestLanguageGrantPublicDefault revokes PUBLIC's built-in USAGE although SVV_LANGUAGE_PRIVILEGES has no row for it,
// and sends no REVOKE for other grantees whose catalog shows nothing.
func TestLanguageGrantPublicDefault(t *testing.T) {
	public := map[string]string{"database_name": "warehouse", "language_name": "sql", "grantee": "public", "grantee_type": "PUBLIC"}
	setup := func() (*privilegeResource, *catalog, *languageGrantFake) {
		c := fullCatalog()
		c.localDB = true
		fake := fakeState[*languageGrantFake](c, "language_grant")
		clear(fake.usage)
		r := newLanguageGrantResource().(*privilegeResource)
		r.resourceClient = testResourceClient(c)
		return r, c, fake
	}
	ctx := context.Background()
	t.Run("revoked when not desired", func(t *testing.T) {
		r, c, fake := setup()
		require.True(t, fake.defaults["sql"], "PUBLIC starts with the built-in USAGE")
		require.NoError(t, r.reconcile(ctx, optionObject(t, r, public, nil, nil)))
		assert.Equal(t, []string{"REVOKE USAGE ON LANGUAGE sql FROM PUBLIC"}, c.writes)
		assert.False(t, fake.defaults["sql"], "the default is gone")
		assert.True(t, fake.defaults["plpgsql"], "other languages keep their default")
		c.writes = nil
		require.NoError(t, r.reconcile(ctx, optionObject(t, r, public, nil, nil)))
		assert.Equal(t, []string{"REVOKE USAGE ON LANGUAGE sql FROM PUBLIC"}, c.writes, "the REVOKE is repeated, because the read cannot tell")
	})
	t.Run("granted explicitly when desired", func(t *testing.T) {
		r, c, fake := setup()
		require.NoError(t, r.reconcile(ctx, optionObject(t, r, public, []string{"USAGE"}, nil)))
		assert.Equal(t, []string{"GRANT USAGE ON LANGUAGE sql TO PUBLIC"}, c.writes)
		assert.True(t, fake.defaults["sql"], "granting does not revoke the default")
	})
	t.Run("other grantees", func(t *testing.T) {
		r, c, _ := setup()
		require.NoError(t, r.reconcile(ctx, optionObject(t, r, languageGrantBaseFields, nil, nil)))
		assert.Empty(t, c.writes, "only PUBLIC holds a default")
	})
}

// TestLanguageGrantValidation rejects grant options for non-user grantees, foreign privileges, and Python.
func TestLanguageGrantValidation(t *testing.T) {
	r := newLanguageGrantResource().(*privilegeResource)
	for name, test := range map[string]struct {
		fields              map[string]string
		privileges, options []string
		valid               bool
	}{
		"role usage":            {languageGrantBaseFields, []string{"USAGE"}, nil, true},
		"user option":           {map[string]string{"database_name": "warehouse", "language_name": "sql", "grantee": "analyst", "grantee_type": "USER"}, []string{"USAGE"}, []string{"USAGE"}, true},
		"role option":           {languageGrantBaseFields, []string{"USAGE"}, []string{"USAGE"}, false},
		"option without usage":  {map[string]string{"database_name": "warehouse", "language_name": "sql", "grantee": "analyst", "grantee_type": "USER"}, nil, []string{"USAGE"}, false},
		"foreign privilege":     {languageGrantBaseFields, []string{"EXECUTE"}, nil, false},
		"python":                {map[string]string{"database_name": "warehouse", "language_name": "plpythonu", "grantee": "x", "grantee_type": "ROLE"}, []string{"USAGE"}, nil, false},
		"missing database name": {map[string]string{"language_name": "sql", "grantee": "x", "grantee_type": "ROLE"}, []string{"USAGE"}, nil, false},
	} {
		t.Run(name, func(t *testing.T) {
			err := r.validate(optionObject(t, r, test.fields, test.privileges, test.options))
			assert.Equal(t, test.valid, err == nil, "%v", err)
		})
	}
}

// TestLanguageGrantOptionReconcile grants USAGE with grant option to a user in one statement and verifies it.
func TestLanguageGrantOptionReconcile(t *testing.T) {
	fields := map[string]string{"database_name": "warehouse", "language_name": "sql", "grantee": "analyst", "grantee_type": "USER"}
	r := newLanguageGrantResource().(*privilegeResource)
	c := &privilegeCatalog{values: map[string]bool{}, grantee: "analyst", kind: "user"}
	r.resourceClient = testResourceClient(c)
	require.NoError(t, r.reconcile(context.Background(), optionObject(t, r, fields, []string{"USAGE"}, []string{"USAGE"})))
	assert.Equal(t, []string{`GRANT USAGE ON LANGUAGE sql TO "analyst" WITH GRANT OPTION`}, c.writes)
	c.writes = nil
	require.NoError(t, r.reconcile(context.Background(), optionObject(t, r, fields, []string{"USAGE"}, nil)))
	assert.Equal(t, []string{`REVOKE GRANT OPTION FOR USAGE ON LANGUAGE sql FROM "analyst"`}, c.writes)
	c.writes, c.stuck = nil, true
	require.ErrorContains(t, r.reconcile(context.Background(), optionObject(t, r, fields, nil, nil)), "did not converge")
}
