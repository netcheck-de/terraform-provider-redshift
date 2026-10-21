package provider

import (
	"context"
	"errors"
	"maps"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCoverageGrantsObjectSelectors(t *testing.T) {
	r := newObjectGrantResource().(*privilegeResource)
	base := map[string]string{"database_name": "shared_db", "database_type": "shared", "object_type": "TABLE", "schema_name": "serving", "object_name": "orders", "grantee_type": "USER", "grantee": "reader"}
	for _, test := range []struct {
		name    string
		fields  map[string]string
		message string
	}{
		{"unsupported kind", map[string]string{"object_type": "VIEW"}, "unsupported object_type"},
		{"arguments on table", map[string]string{"database_type": "", "arguments": "integer"}, "arguments is only valid"},
		{"missing object", map[string]string{"object_name": ""}, "object_name is required"},
		{"unexpected object", map[string]string{"object_type": "SCHEMA"}, "does not accept object_name"},
		{"invalid principal", map[string]string{"grantee_type": "GROUP", "grantee": ""}, "parameter :name is empty"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fields := maps.Clone(base)
			maps.Copy(fields, test.fields)
			data := privilegeObject(t, r, fields)
			_, err := objectGrantConfigAllowed(data)
			if test.name != "invalid principal" {
				require.ErrorContains(t, err, test.message)
			} else {
				require.NoError(t, err)
			}
			_, err = sharedObjectGrantTarget(data)
			require.ErrorContains(t, err, test.message)
		})
	}
	for _, kind := range []string{"DATABASE", "SCHEMA"} {
		t.Run(kind, func(t *testing.T) {
			fields := maps.Clone(base)
			fields["object_type"], fields["object_name"] = kind, ""
			if kind == "DATABASE" {
				fields["schema_name"] = ""
			}
			data := privilegeObject(t, r, fields, "USAGE")
			allowed, err := objectGrantConfigAllowed(data)
			require.NoError(t, err)
			assert.Equal(t, []sqlclient.Keyword{"USAGE"}, allowed)
			_, err = sharedObjectGrantTarget(withGrantOptions(data, []string{"USAGE"}))
			require.ErrorContains(t, err, "do not support grant options")
		})
	}
}

func TestCoverageGrantsDefaultConfigAllowlist(t *testing.T) {
	r := newDefaultPrivilegesResource().(*privilegeResource)
	for kind, count := range map[string]int{"TABLES": 7, "FUNCTIONS": 1, "PROCEDURES": 1, "VIEWS": 0} {
		t.Run(kind, func(t *testing.T) {
			data := privilegeObject(t, r, map[string]string{"object_type": kind})
			allowed, err := defaultPrivilegesConfigAllowed(data)
			if count == 0 {
				require.ErrorContains(t, err, "unsupported default object_type")
				return
			}
			require.NoError(t, err)
			assert.Len(t, allowed, count)
		})
	}
	data := privilegeObject(t, r, nil)
	attributes := data.Attributes()
	attributes["object_type"] = types.StringUnknown()
	allowed, err := defaultPrivilegesConfigAllowed(types.ObjectValueMust(data.AttributeTypes(context.Background()), attributes))
	require.NoError(t, err)
	assert.Nil(t, allowed)
}

func TestCoverageGrantsMalformedConfig(t *testing.T) {
	for _, factory := range []func() resource.Resource{newDatashareMembershipResource, newDebugGrantResource, newColumnGrantResource, newGrantResource, newObjectGrantResource} {
		r := factory()
		var schema resource.SchemaResponse
		r.Schema(context.Background(), resource.SchemaRequest{}, &schema)
		config := tfsdk.Config{Schema: schema.Schema, Raw: tftypes.NewValue(tftypes.String, "not an object")}
		var response resource.ValidateConfigResponse
		r.(resource.ResourceWithValidateConfig).ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: config}, &response)
		require.True(t, response.Diagnostics.HasError(), "%T: %v", r, response.Diagnostics)
	}
}

func TestCoverageGrantsRendererInvalidTuples(t *testing.T) {
	_, err := createDatashareGrantStatement(datashareGrantModel{})
	require.Error(t, err)
	_, err = dropDatashareGrantStatement(datashareGrantModel{})
	require.Error(t, err)
	_, err = dropDatashareMembershipStatement(datashareMembershipModel{})
	require.ErrorContains(t, err, "must be nonempty")
	_, err = datashareMembershipKind(datashareMembershipFixture("PROCEDURES"))
	require.ErrorContains(t, err, "kind must be")
	_, err = readDebugGrantRemoteStatement(debugGrantModel{})
	require.ErrorContains(t, err, "database must not be empty")
	_, err = dropDebugGrantStatement(debugGrantModel{})
	require.ErrorContains(t, err, "database must not be empty")
	assert.False(t, debugGrantRemoteMatches(sqlclient.Row{}, debugGrantFixture("db@catalog", "123456789012", "ACCOUNT")))
	_, err = (grantModel{Scope: types.StringValue("INVALID")}).spec()
	require.Error(t, err)
}

func TestCoverageGrantsColumnPartialPrincipal(t *testing.T) {
	m := columnGrantTestModel(columnGrantBaseFields, columnGrantLifecycleColumns)
	m.DatabaseName = types.StringUnknown()
	m.GranteeType = types.StringValue("INVALID")
	require.Error(t, validateColumnGrant(m))
	m.GranteeType = types.StringValue("USER")
	m.Privileges = types.MapValueMust(types.SetType{ElemType: types.StringType}, map[string]attr.Value{"SELECT": types.SetUnknown(types.StringType)})
	require.NoError(t, validateColumnGrant(m))
	m.Privileges = types.MapUnknown(types.SetType{ElemType: types.StringType})
	require.NoError(t, validateColumnGrant(m))
	r := &columnGrantResource{}
	m.GranteeType = types.StringValue("INVALID")
	require.Error(t, r.reconcile(context.Background(), m))
}

func TestCoverageGrantsEmptyDatabaseChecks(t *testing.T) {
	for _, kind := range []string{"DATABASE", "ALL TABLES"} {
		t.Run("shared "+kind, func(t *testing.T) {
			r := newObjectGrantResource().(*privilegeResource)
			fields := map[string]string{"database_name": "", "database_type": "shared", "object_type": kind, "schema_name": "serving", "grantee": "reader", "grantee_type": "USER"}
			if kind == "DATABASE" {
				fields["schema_name"] = ""
			}
			_, err := sharedObjectGrantTarget(privilegeObject(t, r, fields))
			require.ErrorContains(t, err, "parameter :database is empty")
		})
	}
	r := newDefaultPrivilegesResource().(*privilegeResource)
	_, err := defaultPrivilegesTarget(privilegeObject(t, r, map[string]string{"database_name": "", "object_type": "TABLES", "grantee": "reader", "grantee_type": "USER"}))
	require.ErrorContains(t, err, "parameter :database is empty")
	r = newObjectGrantResource().(*privilegeResource)
	_, err = objectGrantTarget(privilegeObject(t, r, map[string]string{"database_name": "", "schema_name": "serving", "object_name": "predict", "object_type": "MODEL", "grantee": "reader", "grantee_type": "USER"}))
	require.ErrorContains(t, err, "parameter :database is empty")
}

func TestCoverageGrantsRestoreValidationAndDestroy(t *testing.T) {
	r := newDefaultPrivilegesResource().(*privilegeResource)
	r.resourceClient = testResourceClient(&privilegeCatalog{values: map[string]bool{}, grantee: "reader", kind: "user"})
	data := privilegeObject(t, r, map[string]string{"database_name": "warehouse", "grantee": "reader", "grantee_type": "USER", "object_type": "TABLES"})
	attributes := data.Attributes()
	attributes["restore_default_on_destroy"] = types.BoolValue(true)
	data = types.ObjectValueMust(data.AttributeTypes(context.Background()), attributes)
	require.ErrorContains(t, r.validateConfig(data), "restore_default_on_destroy requires")
	state := testState(t, r, data)
	var response resource.DeleteResponse
	r.Delete(context.Background(), resource.DeleteRequest{State: state}, &response)
	require.True(t, response.Diagnostics.HasError(), "%v", response.Diagnostics)
	assert.Contains(t, response.Diagnostics.Errors()[0].Detail(), "restore_default_on_destroy requires")
	allowed, err := fixedPrivilegeAllowlist([]sqlclient.Keyword{"USAGE"})(data)
	require.NoError(t, err)
	assert.Equal(t, []sqlclient.Keyword{"USAGE"}, allowed)
}

func TestCoverageGrantsSnapshotIgnoresOtherRecipients(t *testing.T) {
	r := newObjectGrantResource().(*privilegeResource)
	r.resourceClient = testResourceClient(queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
		if strings.Contains(sql, "DISTINCT table_name AS object_name") {
			return []sqlclient.Row{{"object_name": "orders"}}, nil
		}
		assert.Equal(t, `SHOW GRANTS ON TABLE "shared_db"."serving"."orders"`, sql)
		return []sqlclient.Row{{"identity_name": "other", "identity_type": "user", "privilege_scope": "TABLE", "privilege_type": "SELECT"}, {"identity_name": "reader", "identity_type": "user", "privilege_scope": "TABLE", "privilege_type": "SELECT"}}, nil
	}))
	data := privilegeObject(t, r, map[string]string{"database_name": "shared_db", "database_type": "shared", "schema_name": "serving", "object_type": "ALL TABLES", "grantee": "reader", "grantee_type": "USER"})
	target, err := sharedObjectGrantTarget(data)
	require.NoError(t, err)
	rows, found, err := r.privilegeRows(context.Background(), target)
	require.NoError(t, err)
	assert.True(t, found)
	require.Len(t, rows, 1)
	assert.Equal(t, "reader", rows[0]["identity_name"])
}

func TestCoverageGrantsCompleteCatalogQueryFailure(t *testing.T) {
	r := newObjectGrantResource().(*privilegeResource)
	r.resourceClient = testResourceClient(queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
		assert.Contains(t, sql, "usesuper")
		return nil, errors.New("visibility unavailable")
	}))
	data := privilegeObject(t, r, map[string]string{"database_name": "shared_db", "database_type": "shared", "schema_name": "serving", "object_type": "ALL TABLES", "grantee": "reader", "grantee_type": "USER"})
	_, found, err := r.read(context.Background(), &data)
	require.ErrorContains(t, err, "visibility unavailable")
	assert.False(t, found)
}

func TestCoverageGrantsRemoteDatabaseVisibilityError(t *testing.T) {
	r := &debugGrantResource{testResourceClient(queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
		if strings.Contains(sql, "sys_connection_log") {
			return []sqlclient.Row{{"auth_method": "IAM AssumeUser"}}, nil
		}
		assert.Contains(t, sql, "SHOW DATABASES")
		return nil, errors.New("listing unavailable")
	}))}
	require.ErrorContains(t, r.remoteVisible(context.Background(), "db@catalog"), "cannot verify remote DEBUG database visibility")
	data := debugGrantFixture("", "IAM:reader", "USER")
	var response resource.ValidateConfigResponse
	r.ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config(testState(t, r, data))}, &response)
	require.True(t, response.Diagnostics.HasError())
	assert.Contains(t, response.Diagnostics.Errors()[0].Detail(), "database must not be empty")
}
