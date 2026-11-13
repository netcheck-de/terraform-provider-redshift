package provider

import (
	"fmt"
	"os"
	"strings"
	"testing"

	frameworkprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/stretchr/testify/require"
)

// testAccProviderFactories gives every test connection a distinct in-process server and every command a fresh provider.
// The reattach harness shares one server across aliases of the same provider address, so tests using multiple warehouse
// or database configurations register separate local provider names instead of aliases.
func testAccProviderFactories(names ...string) map[string]func() (tfprotov6.ProviderServer, error) {
	return testProviderFactories(func() frameworkprovider.Provider {
		return &testAccProvider{redshiftProvider: New("test")().(*redshiftProvider)}
	}, names...)
}

// testAccProviderServer constructs a fresh server for single-connection acceptance cases using the same write gate.
func testAccProviderServer() (tfprotov6.ProviderServer, error) {
	return providerserver.NewProtocol6WithError(&testAccProvider{redshiftProvider: New("test")().(*redshiftProvider)})()
}

// testProviderFactories also accepts offline provider factories so connection routing can be checked without AWS.
func testProviderFactories(factory func() frameworkprovider.Provider, names ...string) map[string]func() (tfprotov6.ProviderServer, error) {
	factories := make(map[string]func() (tfprotov6.ProviderServer, error), len(names))
	for _, name := range names {
		factories[name] = func() (tfprotov6.ProviderServer, error) {
			return providerserver.NewProtocol6WithError(factory())()
		}
	}

	return factories
}

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

// testAccWorkgroup returns the region, workgroup, and admin database of the shared Serverless fixture.
func testAccWorkgroup(t *testing.T, flags ...string) (region, workgroup, database string) {
	t.Helper()
	testAccPreCheck(t, flags...)

	region = os.Getenv("REDSHIFT_ACC_REGION")

	workgroup, database = os.Getenv("REDSHIFT_ACC_WORKGROUP"), os.Getenv("REDSHIFT_ACC_DATABASE")
	for name, value := range map[string]string{"REDSHIFT_ACC_REGION": region, "REDSHIFT_ACC_WORKGROUP": workgroup, "REDSHIFT_ACC_DATABASE": database} {
		require.NotEmpty(t, value, name)
	}

	return region, workgroup, database
}

// testAccSecretARN selects the warehouse's own secret, never falling back to another warehouse's credentials.
func testAccSecretARN(cluster bool) string {
	if cluster {
		return os.Getenv("REDSHIFT_ACC_CLUSTER_SECRET_ARN")
	}

	return os.Getenv("REDSHIFT_ACC_SECRET_ARN")
}

// TestAcceptanceSecretSelection keeps both warehouse secrets independent in one acceptance process.
func TestAcceptanceSecretSelection(t *testing.T) {
	t.Setenv("REDSHIFT_ACC_SECRET_ARN", "serverless-secret")
	t.Setenv("REDSHIFT_ACC_CLUSTER_SECRET_ARN", "cluster-secret")
	require.Equal(t, "serverless-secret", testAccSecretARN(false))
	require.Equal(t, "cluster-secret", testAccSecretARN(true))

	t.Setenv("REDSHIFT_ACC_CLUSTER_SECRET_ARN", "")
	require.Empty(t, testAccSecretARN(true), "a cluster must not use the Serverless secret")
	require.Equal(t, "serverless-secret", testAccSecretARN(false))

	t.Setenv("REDSHIFT_ACC_CLUSTER_SECRET_ARN", "cluster-secret")
	t.Setenv("REDSHIFT_ACC_SECRET_ARN", "")
	require.Empty(t, testAccSecretARN(false), "Serverless must not use the cluster secret")
	require.Equal(t, "cluster-secret", testAccSecretARN(true))
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
