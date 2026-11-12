package provider

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccListingsMissingDatabase exercises the real database existence lookup without connecting to a missing database.
func TestAccListingsMissingDatabase(t *testing.T) {
	testAccListingDatabaseBoundary(t, "acc_absent_"+strconv.FormatInt(time.Now().UnixNano(), 36))
}

// TestAccListingsSharedDatabase keeps local-only listings out of the fixture's consumer database.
func TestAccListingsSharedDatabase(t *testing.T) {
	testAccPreCheck(t, "REDSHIFT_ACC_SHARED_DATABASE")
	testAccListingDatabaseBoundary(t, os.Getenv("REDSHIFT_ACC_SHARED_DATABASE"))
}

// testAccListingDatabaseBoundary verifies empty, identified results for every local-only listing at a boundary.
func testAccListingDatabaseBoundary(t *testing.T, selected string) {
	t.Helper()
	region, workgroup, database := testAccWorkgroup(t)
	configuration := grantAcceptanceProvider(region, workgroup, database)

	var checks []resource.TestCheckFunc

	names := make([]string, 0, len(localListings))
	for name := range localListings {
		names = append(names, name)
	}

	sort.Strings(names)

	for _, name := range names {
		field := localListings[name].field
		configuration += fmt.Sprintf("\ndata %q %q { %s = %q }\n", "redshift_"+name, "boundary", field, selected)
		address := "data.redshift_" + name + ".boundary"
		checks = append(checks, resource.TestCheckResourceAttr(address, name+".#", "0"),
			resource.TestCheckResourceAttr(address, "id", fmt.Sprintf(`{%q:%q}`, field, selected)))
	}

	if os.Getenv("REDSHIFT_ACC_SHARED_DATABASE") != selected {
		// Grant listings support shared databases, but a missing database must also list nothing.
		for _, name := range []string{"grants", "debug_grants", "object_grants"} {
			field := "database_name"
			if name == "debug_grants" {
				field = "database"
			}

			extra := ""
			if name == "object_grants" {
				extra = `object_type = "DATABASE"`
			}

			configuration += fmt.Sprintf("\ndata %q %q {\n %s = %q\n %s\n}\n", "redshift_"+name, "boundary", field, selected, extra)
			checks = append(checks, resource.TestCheckResourceAttr("data.redshift_"+name+".boundary", name+".#", "0"))
		}
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(New("test")())},
		Steps:                    []resource.TestStep{{Config: configuration, Check: resource.ComposeAggregateTestCheckFunc(checks...)}, {Config: configuration, PlanOnly: true}},
	})
}

// TestAccSharedListings checks actual inbound and consumer rows, not an empty result or Lake Formation prerequisite.
func TestAccSharedListings(t *testing.T) {
	region, workgroup, database := testAccWorkgroup(t, "REDSHIFT_ACC_SHARED_DATABASE", "REDSHIFT_ACC_SHARED_DATASHARE", "REDSHIFT_ACC_SHARED_PRODUCER_CLUSTER", "REDSHIFT_ACC_SHARED_PRODUCER_SECRET_ARN", "REDSHIFT_ACC_SHARED_CONSUMER_NAMESPACE", "REDSHIFT_ACC_SHARED_PRODUCER_NAMESPACE")
	share := os.Getenv("REDSHIFT_ACC_SHARED_DATASHARE")
	configuration := grantAcceptanceProvider(region, workgroup, database) + fmt.Sprintf(`
provider "redshift" {
 alias = "producer"
 region = %q
 database = %q
 cluster_identifier = %q
 secret_arn = %q
}
data "redshift_inbound_datashares" "fixture" { name = %q }
data "redshift_datashare_grants" "fixture" {
 provider = redshift.producer
 datashare = %q
}
`, region, database, os.Getenv("REDSHIFT_ACC_SHARED_PRODUCER_CLUSTER"), os.Getenv("REDSHIFT_ACC_SHARED_PRODUCER_SECRET_ARN"), share, share)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(New("test")())},
		Steps: []resource.TestStep{{Config: configuration, Check: resource.ComposeAggregateTestCheckFunc(
			resource.TestCheckResourceAttr("data.redshift_inbound_datashares.fixture", "inbound_datashares.#", "1"),
			resource.TestCheckResourceAttr("data.redshift_inbound_datashares.fixture", "inbound_datashares.0.producer_namespace", os.Getenv("REDSHIFT_ACC_SHARED_PRODUCER_NAMESPACE")),
			resource.TestCheckResourceAttr("data.redshift_inbound_datashares.fixture", "inbound_datashares.0.consumer_database", os.Getenv("REDSHIFT_ACC_SHARED_DATABASE")),
			resource.TestCheckResourceAttr("data.redshift_datashare_grants.fixture", "datashare_grants.#", "1"),
			resource.TestCheckResourceAttr("data.redshift_datashare_grants.fixture", "datashare_grants.0.database", database),
			resource.TestCheckResourceAttr("data.redshift_datashare_grants.fixture", "datashare_grants.0.namespace_id", os.Getenv("REDSHIFT_ACC_SHARED_CONSUMER_NAMESPACE")),
			resource.TestCheckNoResourceAttr("data.redshift_datashare_grants.fixture", "datashare_grants.0.account_id"),
		)}, {Config: configuration, PlanOnly: true}},
	})
}
