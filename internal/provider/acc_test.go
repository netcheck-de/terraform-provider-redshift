package provider

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// testAccPreCheck skips unless TF_ACC=1 and every feature flag selecting an optional fixture is set.
func testAccPreCheck(t *testing.T, flags ...string) {
	t.Helper()
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("set TF_ACC=1 to run acceptance tests against a test Redshift warehouse")
	}
	for _, flag := range flags {
		if os.Getenv(flag) == "" {
			t.Skipf("set %s for this acceptance fixture", strings.Join(flags, " and "))
		}
	}
}

// testAccWorkgroup returns the region, profile, workgroup, and admin database of the shared Serverless fixture.
func testAccWorkgroup(t *testing.T, flags ...string) (region, profile, workgroup, database string) {
	t.Helper()
	testAccPreCheck(t, flags...)
	region, profile = os.Getenv("REDSHIFT_ACC_REGION"), os.Getenv("REDSHIFT_ACC_PROFILE")
	workgroup, database = os.Getenv("REDSHIFT_ACC_WORKGROUP"), os.Getenv("REDSHIFT_ACC_DATABASE")
	for name, value := range map[string]string{"REDSHIFT_ACC_REGION": region, "REDSHIFT_ACC_PROFILE": profile, "REDSHIFT_ACC_WORKGROUP": workgroup, "REDSHIFT_ACC_DATABASE": database} {
		require.NotEmpty(t, value, name)
	}
	return region, profile, workgroup, database
}
