package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
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
	// options records privileges held with grant option, reported as admin_option = t.
	options map[string]bool
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
			admin := c.admin
			if c.options[value] {
				admin = "t"
			}
			rows = append(rows, sqlclient.Row{"privilege_type": value, "identity_name": c.grantee, "identity_type": c.kind, "privilege_scope": c.scope, "admin_option": admin})
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
	value, optionOnly := strings.CutPrefix(value, "GRANT OPTION FOR ")
	if strings.HasPrefix(value, "ASSUMEROLE") {
		_, value, _ = strings.Cut(value, " FOR ")
	} else {
		value, _, _ = strings.Cut(value, " ON ")
		value, _, _ = strings.Cut(value, " TO ")
		value, _, _ = strings.Cut(value, " FROM ")
	}
	switch {
	case verb == "GRANT":
		c.values[value] = true
		if strings.HasSuffix(sql, " WITH GRANT OPTION") {
			if c.options == nil {
				c.options = map[string]bool{}
			}
			c.options[value] = true
		}
	case optionOnly:
		delete(c.options, value)
	default:
		delete(c.values, value)
		delete(c.options, value)
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
		if set, ok := attribute.GetType().(types.SetType); ok {
			attributes[name] = types.SetValueMust(set.ElemType, nil)
		}
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

// TestPrivilegeCreateRejectsInvalidTupleBeforeState reports invalid tuples at plan time and keeps them out of state.
func TestPrivilegeCreateRejectsInvalidTupleBeforeState(t *testing.T) {
	for name, mutate := range map[string]func(map[string]attr.Value){
		"tuple": func(values map[string]attr.Value) { values["schema_name"] = types.StringValue("serving") },
		"privilege": func(values map[string]attr.Value) {
			values["privileges"] = types.SetValueMust(types.StringType, []attr.Value{types.StringValue("SELECT")})
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := newObjectGrantResource().(*privilegeResource)
			r.resourceClient = testResourceClient(queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
				return nil, fmt.Errorf("unexpected SQL %q", sql)
			}))
			var schemaResponse resource.SchemaResponse
			r.Schema(context.Background(), resource.SchemaRequest{}, &schemaResponse)
			objectType := schemaResponse.Schema.Type().(basetypes.ObjectType)
			values := map[string]attr.Value{}
			for key := range objectType.AttrTypes {
				values[key] = types.StringNull()
			}
			values["privileges"] = types.SetValueMust(types.StringType, nil)
			values["database_name"], values["object_type"] = types.StringValue("analytics"), types.StringValue("DATABASE")
			values["grantee"], values["grantee_type"] = types.StringValue("readers"), types.StringValue("ROLE")
			mutate(values)
			plan := tfsdk.Plan{Schema: schemaResponse.Schema}
			require.False(t, plan.Set(context.Background(), types.ObjectValueMust(objectType.AttrTypes, values)).HasError())
			resp := resource.CreateResponse{State: tfsdk.State{Schema: plan.Schema, Raw: tftypes.NewValue(plan.Raw.Type(), nil)}}
			r.Create(context.Background(), resource.CreateRequest{Plan: plan}, &resp)
			require.True(t, resp.Diagnostics.HasError())
			assert.True(t, resp.State.Raw.IsNull(), "invalid tuple must not be recorded in state")
			var validated resource.ValidateConfigResponse
			r.ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config(plan)}, &validated)
			assert.True(t, validated.Diagnostics.HasError(), "invalid tuple must be reported during planning")
		})
	}
}
