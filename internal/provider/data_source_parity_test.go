package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDataSourceReadableAttributeParity checks all lookup schemas against their paired resources.
func TestDataSourceReadableAttributeParity(t *testing.T) {
	cases := []struct {
		name      string
		source    func() datasource.DataSource
		resource  func() resource.Resource
		selectors []string
	}{
		{"database", newDatabaseDataSource, newDatabaseResource, []string{"name"}},
		{"datashare", newDatashareDataSource, newDatashareResource, []string{"database", "name"}},
		{"identity_provider", newIdentityProviderDataSource, newIdentityProviderResource, []string{"name"}},
		{"role", newRoleDataSource, newRoleResource, []string{"name"}},
		{"user", newUserDataSource, newUserResource, []string{"name"}},
		{"schema", newSchemaDataSource, newSchemaResource, []string{"database", "name"}},
		{"external_schema", newExternalSchemaDataSource, newExternalSchemaResource, []string{"database", "name"}},
		{"group", newGroupDataSource, newGroupResource, []string{"name"}},
		{"datashare_grant", newDatashareGrantDataSource, newDatashareGrantResource, []string{"database", "datashare", "account_id", "namespace_id"}},
		{"datashare_schema", newDatashareSchemaDataSource, newDatashareSchemaResource, []string{"database", "datashare", "schema"}},
		{"datashare_table", newDatashareTableDataSource, newDatashareTableResource, []string{"database", "datashare", "schema", "table"}},
		{"group_membership", newGroupMembershipDataSource, newGroupMembershipResource, []string{"group", "user"}},
		{"role_grant", newRoleGrantDataSource, newRoleGrantResource, []string{"role", "to_user", "to_role"}},
		{"grant", newGrantDataSource, newGrantResource, []string{"database_name", "schema_name", "role", "datashare", "scope"}},
		{"comment", newCommentDataSource, newCommentResource, []string{"database_name", "schema_name", "object_type", "object_name", "column_name"}},
		{"object_grant", newObjectGrantDataSource, newObjectGrantResource, []string{"database_name", "schema_name", "object_name", "object_type", "grantee", "grantee_type"}},
		{"default_privileges", newDefaultPrivilegesDataSource, newDefaultPrivilegesResource, []string{"database_name", "owner", "schema_name", "object_type", "grantee", "grantee_type"}},
		{"system_grant", newSystemGrantDataSource, newSystemGrantResource, []string{"role"}},
		{"assumerole_grant", newAssumeroleGrantDataSource, newAssumeroleGrantResource, []string{"iam_role_arn", "grantee", "grantee_type"}},
	}
	p := New("test")()
	assert.Len(t, cases, len(p.Resources(context.Background())), "every registered resource needs a parity case")
	assert.Len(t, cases, len(p.DataSources(context.Background())), "every registered data source needs a parity case")
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var source datasource.SchemaResponse
			var paired resource.SchemaResponse
			test.source().Schema(context.Background(), datasource.SchemaRequest{}, &source)
			test.resource().Schema(context.Background(), resource.SchemaRequest{}, &paired)
			for _, excluded := range []string{"password_wo", "password_wo_version", "refresh_revision"} {
				assert.NotContains(t, source.Schema.Attributes, excluded)
				delete(paired.Schema.Attributes, excluded)
			}
			for name, expected := range paired.Schema.Attributes {
				actual, ok := source.Schema.Attributes[name]
				require.True(t, ok, "missing readable attribute %s", name)
				assert.Equal(t, expected.GetType(), actual.GetType(), name)
				// Observed values are computed even when their resource counterpart is configurable.
				selector := false
				for _, key := range test.selectors {
					selector = selector || key == name
				}
				if selector {
					assert.Equal(t, expected.IsRequired(), actual.IsRequired(), name)
					assert.Equal(t, expected.IsOptional(), actual.IsOptional(), name)
					assert.False(t, actual.IsComputed(), name)
				} else {
					assert.True(t, actual.IsComputed(), name)
					assert.False(t, actual.IsRequired(), name)
					assert.False(t, actual.IsOptional(), name)
				}
			}
			for name := range source.Schema.Attributes {
				if name != "exists" {
					assert.Contains(t, paired.Schema.Attributes, name, "unpaired data-source attribute")
				}
			}
		})
	}
}
