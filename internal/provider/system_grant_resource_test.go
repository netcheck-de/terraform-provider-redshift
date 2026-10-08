package provider

import "testing"

// TestSystemGrantLifecycle exercises authoritative role capability management.
func TestSystemGrantLifecycle(t *testing.T) {
	exercisePrivilege(t, newSystemGrantResource, map[string]string{"role": "operators"}, []string{"CREATE USER", "CREATE ROLE"})
}
