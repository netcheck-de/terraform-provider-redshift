package provider

import (
	"context"
	"errors"
	"maps"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerReplacementPolicy("redshift_policy_grant", map[string]replaceRule{
	"database_name": replaceAlways,
	"schema_name":   replaceAlways,
	"object_name":   replaceAlways,
	"policy_type":   replaceAlways,
	"policy_name":   replaceAlways,
	"privileges":    replaceNever,
})

var _ = registerLookupExemption("redshift_policy_grant", "Redshift documents no catalog that lists grants to RLS or masking policies, so a lookup could not observe them.")

var _ = registerValidateConfigCase("policy_grant", func() validateConfigCase {
	unknown := policyGrantObject(nil, "SELECT")
	lookupValue(&unknown, "policy_name", types.StringUnknown())
	return validateConfigCase{new: newPolicyGrantResource, valid: policyGrantObject(nil, "SELECT"), invalid: policyGrantObject(nil, "INSERT"), unknown: unknown}
}())

// policyGrantObject builds the tuple with optional field changes and the desired privileges, with a null ID.
func policyGrantObject(changes map[string]string, privileges ...string) types.Object {
	fields := maps.Clone(policyGrantTupleFields)
	maps.Copy(fields, changes)
	var response resource.SchemaResponse
	newPolicyGrantResource().Schema(context.Background(), resource.SchemaRequest{}, &response)
	attributes, attributeTypes := map[string]attr.Value{}, map[string]attr.Type{}
	for name, attribute := range response.Schema.Attributes {
		attributeTypes[name], attributes[name] = attribute.GetType(), types.StringNull()
		if value, ok := fields[name]; ok {
			attributes[name] = types.StringValue(value)
		}
	}
	attributes["privileges"] = types.SetValueMust(types.StringType, nil)
	return withPrivileges(types.ObjectValueMust(attributeTypes, attributes), privileges)
}

// policyGrantTestCatalog answers the parent checks and records GRANT and REVOKE. It fails any other SQL, so a test
// notices when the resource tries to read grants from a catalog that does not report them.
type policyGrantTestCatalog struct {
	// missing makes the parent checks find nothing, as after the table or policy was dropped.
	missing bool
	// writes records the GRANT and REVOKE statements in order.
	writes []string
}

// Query answers parent checks and records writes.
func (c *policyGrantTestCatalog) Query(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
	switch {
	case strings.HasPrefix(sql, "SELECT "):
		if c.missing {
			return nil, nil
		}
		return []sqlclient.Row{{"exists": "true"}}, nil
	case strings.HasPrefix(sql, "GRANT "), strings.HasPrefix(sql, "REVOKE "):
		c.writes = append(c.writes, sql)
		return nil, nil
	}
	return nil, errors.New("unexpected SQL: " + sql)
}

// TestPolicyGrantLifecycle injects failures at every SQL boundary and checks missing parents for each operation.
func TestPolicyGrantLifecycle(t *testing.T) {
	data := policyGrantObject(nil, "SELECT")
	for _, operation := range []string{"create", "read", "update", "delete"} {
		t.Run(operation, func(t *testing.T) {
			require.True(t, invoke(t, newPolicyGrantResource(), operation, data, true).HasError())
			queries := 0
			for failAt := 0; failAt <= queries; failAt++ {
				c := &policyGrantTestCatalog{}
				calls := 0
				r := newPolicyGrantResource()
				configureTestResource(t, r, queryFunc(func(ctx context.Context, connection sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
					calls++
					if calls == failAt {
						return nil, errors.New("injected query failure")
					}
					return c.Query(ctx, connection, sql, parameters)
				}))
				diagnostics := invoke(t, r, operation, data, false)
				if failAt == 0 {
					require.False(t, diagnostics.HasError(), "%v", diagnostics)
					queries = calls
				} else {
					require.True(t, diagnostics.HasError(), "failure %d: %v", failAt, diagnostics)
				}
			}
			r := newPolicyGrantResource()
			configureTestResource(t, r, &policyGrantTestCatalog{missing: true})
			diagnostics := invoke(t, r, operation, data, false)
			assert.Equal(t, operation == "create" || operation == "update", diagnostics.HasError(), "%v", diagnostics)
		})
	}
}

// TestPolicyGrantStatements derives GRANT and REVOKE from the recorded and planned privileges, since no catalog
// reports the policy's current grant, and keeps the recorded privileges on refresh.
func TestPolicyGrantStatements(t *testing.T) {
	grant := `GRANT SELECT ON TABLE "analytics"."public"."masking_exempt" TO MASKING POLICY "mask_email"`
	revoke := `REVOKE SELECT ON TABLE "analytics"."public"."masking_exempt" FROM MASKING POLICY "mask_email"`
	selected, none := policyGrantObject(nil, "SELECT"), policyGrantObject(nil)
	for name, test := range map[string]struct {
		operation      string
		prior, planned types.Object
		writes         []string
		privileges     []string
	}{
		"create grants":        {"create", none, selected, []string{grant}, []string{"SELECT"}},
		"create empty":         {"create", none, none, nil, nil},
		"update revokes":       {"update", selected, none, []string{revoke}, nil},
		"update grants":        {"update", none, selected, []string{grant}, []string{"SELECT"}},
		"update unchanged":     {"update", selected, selected, nil, []string{"SELECT"}},
		"read keeps recorded":  {"read", selected, selected, nil, []string{"SELECT"}},
		"delete revokes":       {"delete", selected, selected, []string{revoke}, nil},
		"delete nothing owned": {"delete", none, none, nil, nil},
	} {
		t.Run(name, func(t *testing.T) {
			c := &policyGrantTestCatalog{}
			r := newPolicyGrantResource()
			configureTestResource(t, r, c)
			state, diagnostics := applyOperation(t, r, test.operation, test.prior, test.planned, nil)
			require.False(t, diagnostics.HasError(), "%v", diagnostics)
			assert.Equal(t, test.writes, c.writes)
			if test.operation == "delete" {
				return
			}
			var privileges types.Set
			require.False(t, state.GetAttribute(context.Background(), path.Root("privileges"), &privileges).HasError())
			assert.Equal(t, test.privileges, knownStrings(privileges))
		})
	}
	for name, data := range map[string]types.Object{
		"unsupported privilege": policyGrantObject(nil, "INSERT"),
		"unsupported type":      policyGrantObject(map[string]string{"policy_type": "ROLE"}, "SELECT"),
	} {
		t.Run(name, func(t *testing.T) {
			c := &policyGrantTestCatalog{}
			r := newPolicyGrantResource()
			configureTestResource(t, r, c)
			_, diagnostics := applyOperation(t, r, "update", none, data, nil)
			require.True(t, diagnostics.HasError())
			assert.Empty(t, c.writes)
		})
	}
}

// TestPolicyGrantImport restores the tuple from the JSON identity Create records; the grant itself cannot be read,
// so the imported state records no privileges and the next apply grants the configured ones.
func TestPolicyGrantImport(t *testing.T) {
	r := newPolicyGrantResource()
	configureTestResource(t, r, &policyGrantTestCatalog{})
	data := policyGrantObject(nil, "SELECT")
	id := r.(*policyGrantResource).identity("admin", maps.Clone(policyGrantTupleFields)).ValueString()
	for _, candidate := range []string{"invalid JSON", `{}`, id} {
		resp := resource.ImportStateResponse{State: testState(t, r, data)}
		r.(resource.ResourceWithImportState).ImportState(context.Background(), resource.ImportStateRequest{ID: candidate}, &resp)
		assert.Equal(t, candidate != id, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
	}
	imported := resource.ImportStateResponse{State: emptyState(t, r)}
	r.(resource.ResourceWithImportState).ImportState(context.Background(), resource.ImportStateRequest{ID: id}, &imported)
	require.False(t, imported.Diagnostics.HasError(), "%v", imported.Diagnostics)
	refreshed := resource.ReadResponse{State: imported.State}
	r.Read(context.Background(), resource.ReadRequest{State: imported.State}, &refreshed)
	require.False(t, refreshed.Diagnostics.HasError(), "%v", refreshed.Diagnostics)
	var privileges types.Set
	require.False(t, refreshed.State.GetAttribute(context.Background(), path.Root("privileges"), &privileges).HasError())
	assert.False(t, privileges.IsNull())
	assert.Empty(t, privileges.Elements())
}

// TestPolicyGrantTranscripts records grant, revoke, refresh, and import for both policy kinds.
func TestPolicyGrantTranscripts(t *testing.T) {
	catalog := func() sqlclient.Client { return &policyGrantTestCatalog{} }
	rls := map[string]string{"policy_type": "RLS"}
	runTranscripts(t, "masking/policy_grant_flows", newPolicyGrantResource, []transcriptCase{
		{name: "create", operation: "create", catalog: catalog, planned: policyGrantObject(nil, "SELECT")},
		{name: "create_rls", operation: "create", catalog: catalog, planned: policyGrantObject(rls, "SELECT")},
		{name: "read", operation: "read", catalog: catalog, prior: policyGrantObject(nil, "SELECT")},
		{name: "update_revoke", operation: "update", catalog: catalog, prior: policyGrantObject(nil, "SELECT"), planned: policyGrantObject(nil)},
		{name: "delete", operation: "delete", catalog: catalog, prior: policyGrantObject(nil, "SELECT")},
		{name: "import", operation: "import", catalog: catalog, planned: policyGrantObject(nil, "SELECT")},
	})
}
