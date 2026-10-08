package provider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// privilegeCatalog emulates exact permission sets and parent existence for grant lifecycle tests.
type privilegeCatalog struct {
	// values records current explicit privileges for the selected tuple.
	values map[string]bool
	// writes records the emitted GRANT/REVOKE statements.
	writes []string
	// missing makes parent existence checks return no rows.
	missing bool
	// stuck acknowledges mutations without changing the catalog.
	stuck bool
	// admin supplies the catalog grant-option marker.
	admin string
	// grantee identifies the row's SQL recipient.
	grantee string
	// kind identifies its user/role/group/public catalog type.
	kind string
	// scope identifies the row's explicit object permission scope.
	scope string
}

// Query emulates explicit privilege catalogs and records exact SQL mutations.
func (c *privilegeCatalog) Query(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
	if strings.HasPrefix(sql, "SHOW GRANTS") || strings.Contains(sql, " AS privilege_type") || strings.HasPrefix(sql, "SELECT privilege_type") {
		rows := []sqlclient.Row{}
		for value := range c.values {
			rows = append(rows, sqlclient.Row{"privilege_type": value, "identity_name": c.grantee, "identity_type": c.kind, "privilege_scope": c.scope, "admin_option": c.admin})
		}
		return rows, nil
	}
	if strings.HasPrefix(sql, "SELECT") {
		if c.missing {
			return nil, nil
		}
		return []sqlclient.Row{{"name": "exists"}}, nil
	}
	c.writes = append(c.writes, sql)
	if c.stuck {
		return nil, nil
	}
	verb := "GRANT"
	if strings.Contains(sql, "REVOKE ") {
		verb = "REVOKE"
	}
	_, value, _ := strings.Cut(sql, verb+" ")
	if strings.HasPrefix(value, "ASSUMEROLE") {
		_, value, _ = strings.Cut(value, " FOR ")
	} else {
		value, _, _ = strings.Cut(value, " ON ")
		value, _, _ = strings.Cut(value, " TO ")
		value, _, _ = strings.Cut(value, " FROM ")
	}
	if verb == "GRANT" {
		c.values[value] = true
	} else {
		delete(c.values, value)
	}
	return nil, nil
}

// privilegeObject builds a typed permission model using its concrete resource schema.
func privilegeObject(t *testing.T, r *privilegeResource, fields map[string]string, privileges ...string) types.Object {
	t.Helper()
	var schema resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &schema)
	attributes, attributeTypes := map[string]attr.Value{}, map[string]attr.Type{}
	for name, attribute := range schema.Schema.Attributes {
		attributeTypes[name] = attribute.GetType()
		attributes[name] = types.StringNull()
		if value, ok := fields[name]; ok {
			attributes[name] = types.StringValue(value)
		}
	}
	attributes["privileges"] = types.SetValueMust(types.StringType, nil)
	return withPrivileges(types.ObjectValueMust(attributeTypes, attributes), privileges)
}

// exercisePrivilege verifies lifecycle error paths, exact-set reconciliation, binding, and imports.
func exercisePrivilege(t *testing.T, factory func() resource.Resource, fields map[string]string, privileges []string) {
	t.Helper()
	r := factory().(*privilegeResource)
	var metadata resource.MetadataResponse
	r.Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "redshift"}, &metadata)
	assert.Equal(t, "redshift_"+r.name, metadata.TypeName)
	data := privilegeObject(t, r, fields, privileges[0])
	for _, operation := range []string{"create", "read", "update", "delete"} {
		t.Run(operation, func(t *testing.T) {
			require.True(t, invoke(t, factory(), operation, data, true).HasError())
			queries := 0
			for failAt := 0; failAt <= queries; failAt++ {
				c := &privilegeCatalog{values: map[string]bool{privileges[1]: true}, grantee: fields["grantee"], kind: strings.ToLower(fields["grantee_type"]), scope: fields["object_type"]}
				calls := 0
				client := queryFunc(func(ctx context.Context, connection sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
					calls++
					if calls == failAt {
						return nil, errors.New("injected query failure")
					}
					return c.Query(ctx, connection, sql, parameters)
				})
				r := factory().(*privilegeResource)
				r.resourceClient = testResourceClient(client)
				diagnostics := invoke(t, r, operation, data, false)
				if failAt == 0 {
					require.False(t, diagnostics.HasError(), "%v", diagnostics)
					queries = calls
				} else {
					require.True(t, diagnostics.HasError(), "failure %d: %v", failAt, diagnostics)
				}
			}
			r := factory().(*privilegeResource)
			r.resourceClient = testResourceClient(&privilegeCatalog{missing: true})
			diagnostics := invoke(t, r, operation, data, false)
			assert.Equal(t, operation == "create" || operation == "update", diagnostics.HasError(), "%v", diagnostics)
		})
	}
	c := &privilegeCatalog{values: map[string]bool{privileges[0]: true}, grantee: fields["grantee"], kind: strings.ToLower(fields["grantee_type"]), scope: fields["object_type"]}
	r.resourceClient = testResourceClient(c)
	require.NoError(t, r.reconcile(context.Background(), data))
	assert.Empty(t, c.writes, "already-converged grants must not mutate")
	_, _, err := r.read(context.Background(), &data)
	require.NoError(t, err)
	for _, invalid := range []string{"admin", "catalog privilege", "desired privilege", "stuck", "binding", "configuration"} {
		t.Run(invalid, func(t *testing.T) {
			c := &privilegeCatalog{values: map[string]bool{privileges[1]: true}, grantee: fields["grantee"], kind: strings.ToLower(fields["grantee_type"]), scope: fields["object_type"]}
			r := factory().(*privilegeResource)
			r.resourceClient = testResourceClient(c)
			data := privilegeObject(t, r, fields, privileges[0])
			switch invalid {
			case "admin":
				c.admin = "t"
			case "catalog privilege":
				c.values = map[string]bool{"UNKNOWN": true}
			case "desired privilege":
				data = withPrivileges(data, []string{"UNKNOWN"})
			case "stuck":
				c.stuck = true
			case "binding":
				attributes := data.Attributes()
				attributes["id"] = types.StringValue(`{"workgroup_name":"other","database":"admin"}`)
				data = types.ObjectValueMust(data.AttributeTypes(context.Background()), attributes)
			case "configuration":
				r.warehouse.value = types.StringUnknown()
			}
			require.Error(t, r.reconcile(context.Background(), data))
		})
	}
	for _, id := range []string{"invalid JSON", `{}`, r.identity("admin", fields).ValueString()} {
		state := testState(t, r, data)
		resp := resource.ImportStateResponse{State: state}
		r.ImportState(context.Background(), resource.ImportStateRequest{ID: id}, &resp)
		assert.Equal(t, id == "invalid JSON" || id == `{}`, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
	}
}

// TestPrincipalValidation checks SQL grantee kinds and explicit PUBLIC naming.
func TestPrincipalValidation(t *testing.T) {
	r := newObjectGrantResource().(*privilegeResource)
	for _, kind := range []string{"USER", "GROUP", "PUBLIC", "invalid"} {
		fields := map[string]string{"grantee_type": kind, "grantee": "public"}
		_, _, err := principal(privilegeObject(t, r, fields))
		assert.Equal(t, kind == "invalid", err != nil)
	}
	_, _, err := principal(privilegeObject(t, r, map[string]string{"grantee_type": "PUBLIC", "grantee": "other"}))
	require.Error(t, err)
	assert.Empty(t, objectString(privilegeObject(t, r, nil), "absent"))
}

// TestPrivilegeReadHandlesPublicAndCatalogAliases checks PUBLIC, TEMP/EXFUNC normalization, and tuple filtering.
func TestPrivilegeReadHandlesPublicAndCatalogAliases(t *testing.T) {
	r := newAssumeroleGrantResource().(*privilegeResource)
	r.resourceClient = testResourceClient(&privilegeCatalog{values: map[string]bool{"EXFUNC": true}})
	data := privilegeObject(t, r, map[string]string{"iam_role_arn": "default", "grantee_type": "PUBLIC", "grantee": "public"})
	_, found, err := r.read(context.Background(), &data)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, types.SetValueMust(types.StringType, []attr.Value{types.StringValue("EXTERNAL FUNCTION")}), data.Attributes()["privileges"])
	r = newObjectGrantResource().(*privilegeResource)
	r.resourceClient = testResourceClient(&privilegeCatalog{values: map[string]bool{"TEMP": true}, grantee: "reader", kind: "user", scope: "DATABASE"})
	data = privilegeObject(t, r, map[string]string{"database_name": "warehouse", "object_type": "DATABASE", "grantee_type": "USER", "grantee": "reader"})
	_, _, err = r.read(context.Background(), &data)
	require.NoError(t, err)
	assert.Equal(t, types.SetValueMust(types.StringType, []attr.Value{types.StringValue("TEMPORARY")}), data.Attributes()["privileges"])
	r.resourceClient = testResourceClient(&privilegeCatalog{values: map[string]bool{"TEMP": true}, grantee: "other", kind: "user", scope: "DATABASE"})
	_, _, err = r.read(context.Background(), &data)
	require.NoError(t, err)
	assert.Empty(t, data.Attributes()["privileges"].(types.Set).Elements())
	data = privilegeObject(t, r, map[string]string{"object_type": "invalid", "grantee_type": "USER"})
	_, _, err = r.read(context.Background(), &data)
	require.Error(t, err)
}
