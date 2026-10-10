package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
)

var _ = registerParity(parityCase{source: newColumnGrantDataSource, resource: newColumnGrantResource, selectors: columnGrantFields})

// TestColumnGrantLookup observes one role's column privileges on the fixture relation without reconciling them.
func TestColumnGrantLookup(t *testing.T) {
	c := fullCatalog()
	c.localDB = true
	exerciseCatalogLookup(t, newColumnGrantDataSource, columnGrantBaseFields, map[string]attr.Value{"privileges": columnGrantValue(columnGrantLifecycleColumns)}, c)
}
