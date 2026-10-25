package provider

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/redshiftdata"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/redshiftdata"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/require"
)

// TestAccDebugGrantFederated needs a dedicated federated database and recipients without an existing DEBUG grant.
// The warehouse and catalog registration stay with the fixture owner; the test owns only the explicit grants.
func TestAccDebugGrantFederated(t *testing.T) {
	region, profile, workgroup, database := testAccWorkgroup(t, "REDSHIFT_ACC_FEDERATED_DATABASE")
	target := os.Getenv("REDSHIFT_ACC_FEDERATED_DATABASE")
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region), config.WithSharedConfigProfile(profile))
	require.NoError(t, err)

	client := &dataapi.Client{API: redshiftdata.NewFromConfig(cfg), Workgroup: workgroup, Timeout: 5 * time.Minute, Poll: time.Second}

	for _, test := range []struct{ kind, flag string }{
		{"USER", "REDSHIFT_ACC_DEBUG_USER"},
		{"ROLE", "REDSHIFT_ACC_DEBUG_ROLE"},
		{"ACCOUNT", "REDSHIFT_ACC_DEBUG_ACCOUNT"},
	} {
		t.Run(test.kind, func(t *testing.T) {
			grantee := os.Getenv(test.flag)
			if grantee == "" {
				t.Skipf("set %s to a dedicated federated recipient", test.flag)
			}

			model := debugGrantFixture(target, grantee, test.kind)
			if err := model.validateRemote(); err != nil {
				t.Skip(err.Error())
			}

			recipient, _, err := debugRecipient(grantee, test.kind)
			require.NoError(t, err)

			reader := &debugGrantResource{testResourceClient(client)}
			reader.database = types.StringValue(database)
			found, err := reader.read(ctx, model)
			require.NoError(t, err)

			if found {
				t.Skip("recipient already has DEBUG; refusing to take ownership of an existing grant")
			}

			t.Cleanup(func() {
				statement := sqlclient.Stmt("REVOKE DEBUG ON DATABASE").Ident(target).Kw("FROM").Append(recipient).String()
				_, err := client.Query(ctx, sqlclient.Connection{Database: database}, statement, nil)
				require.NoError(t, err, "remove the test-owned DEBUG grant")
			})

			configuration := fmt.Sprintf(`
provider "redshift" {
  region = %q
  profile = %q
  workgroup_name = %q
  database = %q
}
resource "redshift_debug_grant" "test" {
  database = %q
  grantee = %q
  grantee_type = %q
}
data "redshift_debug_grant" "test" {
  database = redshift_debug_grant.test.database
  grantee = redshift_debug_grant.test.grantee
  grantee_type = redshift_debug_grant.test.grantee_type
}`, region, profile, workgroup, database, target, grantee, test.kind)
			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(New("test")())},
				Steps: []resource.TestStep{
					{Config: configuration, Check: resource.TestCheckResourceAttr("data.redshift_debug_grant.test", "exists", "true")},
					{ResourceName: "redshift_debug_grant.test", ImportState: true, ImportStateVerify: true},
					{Config: configuration, PlanOnly: true},
				},
			})
		})
	}
}
