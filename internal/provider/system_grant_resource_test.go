package provider

import "testing"

var _ = registerLifecycleExemption("redshift_system_grant", "TestSystemGrantLifecycle uses exercisePrivilege and privilegeCatalog.")

var _ = registerReplacementPolicy("redshift_system_grant", map[string]replaceRule{
	"role":       replaceAlways,
	"privileges": replaceNever,
})

// TestSystemGrantLifecycle exercises authoritative role capability management.
func TestSystemGrantLifecycle(t *testing.T) {
	exercisePrivilege(t, newSystemGrantResource, map[string]string{"role": "operators"}, []string{"CREATE USER", "CREATE ROLE"})
}
