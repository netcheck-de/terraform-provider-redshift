package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stretchr/testify/assert"
)

var _ = registerParity(parityCase{source: newDatashareMembershipDataSource, resource: newDatashareMembershipResource, selectors: []string{"database", "datashare", "schema", "kind"}})

func TestDatashareMembershipLookup(t *testing.T) {
	for _, kind := range []string{"TABLES", "FUNCTIONS"} {
		c := fullCatalog()
		c.includeNew = false
		exerciseCatalogLookup(t, newDatashareMembershipDataSource, map[string]string{"database": "admin", "datashare": "producer", "schema": "serving", "kind": kind}, map[string]attr.Value{"exists": types.BoolValue(true), "all": types.BoolValue(true)}, c)
	}
}

func TestDatashareMembershipRestrictedLookupIsObservational(t *testing.T) {
	c := fullCatalog()
	f := fakeState[*fakeDatashares](c, "datashare")
	f.restricted = true
	f.bulkCandidates["TABLES"], f.bulkMembers["TABLES"] = nil, nil
	exerciseCatalogLookup(t, newDatashareMembershipDataSource, map[string]string{"database": "admin", "datashare": "producer", "schema": "serving", "kind": "TABLES"}, map[string]attr.Value{"exists": types.BoolValue(true), "all": types.BoolValue(true)}, c)
}

func TestDatashareMembershipLookupDescriptionsAreObservational(t *testing.T) {
	source := dataSourceSchema(newDatashareMembershipDataSource)
	all := source.Attributes["all"].GetMarkdownDescription()
	assert.Contains(t, all, "visible")
	assert.NotContains(t, all, "Must be true")
	assert.NotContains(t, all, "next apply")
	assert.NotContains(t, source.Attributes["schema"].GetMarkdownDescription(), "include_new = false")
}
