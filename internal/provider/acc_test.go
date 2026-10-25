package provider

import (
	"fmt"
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

// testAccSSLMode returns REDSHIFT_ACC_SSLMODE for direct tests; empty keeps the provider default verify-full.
func testAccSSLMode() string {
	return os.Getenv("REDSHIFT_ACC_SSLMODE")
}

// testAccSSLModeHCL renders optional TLS settings for a direct_connection block.
func testAccSSLModeHCL() string {
	var settings string
	if mode := testAccSSLMode(); mode != "" {
		settings += fmt.Sprintf("sslmode = %q\n", mode)
	}

	if bundle := testAccCACertFile(); bundle != "" {
		settings += fmt.Sprintf("ca_cert_file = %q\n", bundle)
	}

	return settings
}

// testAccCACertFile allows explicit trust without weakening hostname or certificate verification.
func testAccCACertFile() string { return os.Getenv("REDSHIFT_ACC_CA_CERT_FILE") }

// TestAcceptanceTLSOverrides checks that the acceptance provider block sets sslmode and ca_cert_file only when their
// environment variables are set, so unset variables keep the provider defaults.
func TestAcceptanceTLSOverrides(t *testing.T) {
	for _, test := range []struct{ name, mode, bundle, want string }{
		{"defaults", "", "", ""},
		{"verified root", "", "/fixtures/amazon.pem", "ca_cert_file = \"/fixtures/amazon.pem\"\n"},
		{"explicit mode", "require", "", "sslmode = \"require\"\n"},
		{"both", "verify-full", "/fixtures/amazon.pem", "sslmode = \"verify-full\"\nca_cert_file = \"/fixtures/amazon.pem\"\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("REDSHIFT_ACC_SSLMODE", test.mode)
			t.Setenv("REDSHIFT_ACC_CA_CERT_FILE", test.bundle)
			require.Equal(t, test.want, testAccSSLModeHCL())
		})
	}
}
