package provider

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// TestContractDatabaseCollationInPlacePlans plans a collation change as an in-place update, not a replacement of the
// database.
func TestContractDatabaseCollationInPlacePlans(t *testing.T) {
	t.Parallel()
	databasesPlanCase(t, &catalog{}, "redshift_database.warehouse", `
resource "redshift_database" "warehouse" {
  name      = "warehouse"
  collation = "CASE_SENSITIVE"
}`, `
resource "redshift_database" "warehouse" {
  name      = "warehouse"
  collation = "CASE_INSENSITIVE"
}`)
}

// A failed multi-option apply must not make reverted configuration look clean, even when CONNECTION LIMIT 0
// prevents the refresh session from recovering a stale collation: the verified partial value must already be saved.
func TestContractDatabaseCollationRevertedConfigPlans(t *testing.T) {
	for _, mode := range []string{"owner failure", "verification failure with zero limit"} {
		t.Run(mode, func(t *testing.T) {
			c := &catalog{}
			verificationFailed := false
			client := queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
				f := c.family("databases").(*databasesFamily)
				if mode == "owner failure" && strings.Contains(sql, `OWNER TO "etl"`) {
					return nil, errors.New("owner change refused")
				}
				if f.connectionLimit == "0" {
					if strings.HasPrefix(sql, "SELECT db_collation()") {
						return nil, errors.New("too many connections for database")
					}
					if strings.HasPrefix(sql, "SHOW DATABASES") && !verificationFailed {
						verificationFailed = true
						return nil, errors.New("catalog temporarily unavailable")
					}
				}
				return c.Query(ctx, target, sql, parameters)
			})
			configuration := func(collation, owner string, limit int) string {
				return databasesPlanProvider + fmt.Sprintf(`
resource "redshift_database" "warehouse" {
  name             = "warehouse"
  collation        = %q
  owner            = %q
  connection_limit = %d
}`, collation, owner, limit)
			}
			before := configuration("CASE_SENSITIVE", "admin", -1)
			after := configuration("CASE_INSENSITIVE", "etl", -1)
			reverted := before
			if mode != "owner failure" {
				after = configuration("CASE_INSENSITIVE", "admin", 0)
				reverted = configuration("CASE_SENSITIVE", "admin", 0)
			}
			t.Parallel()
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){
					"redshift": providerserver.NewProtocol6WithError(&redshiftProvider{version: "test", client: client}),
				},
				Steps: []resource.TestStep{
					{Config: before},
					{Config: after, ExpectError: regexp.MustCompile("owner change refused|catalog temporarily unavailable")},
					{Config: reverted, PlanOnly: true, ExpectNonEmptyPlan: true},
				},
			})
		})
	}
}
