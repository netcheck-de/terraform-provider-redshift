package provider

import (
	"context"
	"maps"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// templateGrantFields is a role's grant on the fake's template.
var templateGrantFields = map[string]string{"database_name": "admin", "schema_name": templateFakeSchema, "object_name": "csv_load", "object_type": "TEMPLATE", "grantee_type": "ROLE", "grantee": "loaders"}

// templateGrantCatalog returns a full fake whose admin database passes the local-database check.
func templateGrantCatalog(changes ...func(*catalog)) func() *catalog {
	return func() *catalog {
		c := fullCatalog()
		c.localDB = true
		for _, change := range changes {
			change(c)
		}
		return c
	}
}

// TestObjectGrantTemplateSQL pins the checks, SHOW GRANTS ON TEMPLATE read, and GRANT/REVOKE ON TEMPLATE against
// r_GRANT, r_REVOKE, and r_SHOW_GRANTS.
func TestObjectGrantTemplateSQL(t *testing.T) {
	render := func(changes map[string]string, privilege sqlclient.Keyword) func() ([]string, error) {
		fields := maps.Clone(templateGrantFields)
		maps.Copy(fields, changes)
		return privilegeTargetCase(t, newObjectGrantResource, fields, privilege)
	}
	checkSQL(t, "object_grant_template", []sqlCase{
		{"role_usage", render(nil, "USAGE")},
		{"user_alter", render(map[string]string{"grantee_type": "USER", "grantee": "loader"}, "ALTER")},
		{"public_drop", render(map[string]string{"grantee_type": "PUBLIC", "grantee": "public"}, "DROP")},
		{"quoted", render(map[string]string{"database_name": `Odd"Database`, "schema_name": `Odd"Schema`, "object_name": `odd\name_%`, "grantee": `Odd"Role`}, "USAGE")},
		{"without_object_name", render(map[string]string{"object_name": ""}, "USAGE")},
		{"without_schema_name", render(map[string]string{"schema_name": ""}, "USAGE")},
		{"with_arguments", render(map[string]string{"arguments": "integer"}, "USAGE")},
	})
}

// TestObjectGrantTemplateTranscripts grants, narrows, and revokes template privileges through the fake catalog,
// records the conversation, and removes the grant from state once the template is gone.
func TestObjectGrantTemplateTranscripts(t *testing.T) {
	r := newObjectGrantResource().(*privilegeResource)
	model := func(privileges []string, options ...string) types.Object {
		fields := maps.Clone(templateGrantFields)
		if len(options) > 0 {
			fields["grantee_type"], fields["grantee"] = "USER", templateFakeUser
		}
		return withGrantOptions(privilegeObject(t, r, fields, privileges...), options)
	}
	held := templateGrantCatalog(templateFakeChange(func(f *templateFake) {
		f.grants["USAGE"] = templateFakeGrant{identity: "loaders", kind: "role"}
	}))
	runTranscripts(t, "object_grant_template/flows", newObjectGrantResource, []transcriptCase{
		{name: "create", operation: "create", catalog: func() sqlclient.Client { return templateGrantCatalog()() }, planned: model([]string{"USAGE", "ALTER"})},
		{name: "update", operation: "update", catalog: func() sqlclient.Client { return held() }, prior: model([]string{"USAGE"}), planned: model([]string{"DROP"})},
		{name: "user_grant_option", operation: "create", catalog: func() sqlclient.Client { return templateGrantCatalog()() }, planned: model([]string{"USAGE"}, "USAGE")},
		{name: "delete", operation: "delete", catalog: func() sqlclient.Client { return held() }, prior: model([]string{"USAGE"})},
		{name: "import", operation: "import", catalog: func() sqlclient.Client { return templateGrantCatalog()() }, planned: model([]string{"USAGE"})},
	})

	c := templateGrantCatalog(templateFakeChange(func(f *templateFake) { f.exists, f.withoutDecoy = false, true }))()
	configureTestResource(t, r, c)
	state, diagnostics := applyOperation(t, r, "read", model([]string{"USAGE"}), nil, nil)
	require.True(t, diagnostics.HasError(), "the transcript helper reports a removed resource")
	assert.True(t, state.Raw.IsNull(), "a dropped template removes its grant from state")
	assert.Empty(t, c.writes)
	// SHOW TEMPLATES cannot select a name exactly, so a dropped template whose LIKE pattern matches another template
	// reaches SHOW GRANTS, which fails instead of removing the grant from state.
	configureTestResource(t, r, templateGrantCatalog(templateFakeChange(func(f *templateFake) { f.exists = false }))())
	state, diagnostics = applyOperation(t, r, "read", model([]string{"USAGE"}), nil, nil)
	require.True(t, diagnostics.HasError())
	assert.False(t, state.Raw.IsNull())

	stuck := templateGrantCatalog()()
	configureTestResource(t, r, queryFunc(func(ctx context.Context, connection sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		if strings.HasPrefix(sql, "GRANT ") {
			return nil, nil
		}
		return stuck.Query(ctx, connection, sql, parameters)
	}))
	_, diagnostics = applyOperation(t, r, "create", nil, model([]string{"USAGE"}), nil)
	assert.True(t, diagnostics.HasError(), "a grant that does not converge fails")
}
