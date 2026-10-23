package provider

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/redshiftdata"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/redshiftdata"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/require"
)

// testAccRolesClient returns a Data API client for drift injection and destroy checks on the shared workgroup.
func testAccRolesClient(t *testing.T, region, profile, workgroup string) sqlclient.Client {
	t.Helper()
	cfg, err := config.LoadDefaultConfig(context.Background(), config.WithRegion(region), config.WithSharedConfigProfile(profile))
	require.NoError(t, err)
	return &dataapi.Client{API: redshiftdata.NewFromConfig(cfg), Workgroup: workgroup, Timeout: 5 * time.Minute, Poll: time.Second}
}

// TestAccRoleOwnerAndAdminOption changes a role's owner and a user's admin option in place, repairs admin option
// drift, and imports both resources with the observed values.
func TestAccRoleOwnerAndAdminOption(t *testing.T) {
	region, profile, workgroup, database := testAccWorkgroup(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	roleName, first, second := "acc_role_"+suffix, "acc_owner_a_"+suffix, "acc_owner_b_"+suffix
	configuration := func(owner string, admin bool) string {
		return fmt.Sprintf(`
provider "redshift" {
  region         = %q
  profile        = %q
  workgroup_name = %q
  database       = %q
}
resource "redshift_user" "first" {
  name        = %q
  password_wo = %q
}
resource "redshift_user" "second" {
  name        = %q
  password_wo = %q
  # Identity DDL shares catalogs: order this fixture's user creation and destruction.
  depends_on = [redshift_user.first]
}
resource "redshift_role" "this" {
  name  = %q
  owner = redshift_user.%s.name
}
resource "redshift_role_grant" "this" {
  role         = redshift_role.this.name
  to_user      = redshift_user.first.name
  admin_option = %t
}
data "redshift_role" "this" { name = redshift_role.this.name }
data "redshift_role_grant" "this" {
  role    = redshift_role_grant.this.role
  to_user = redshift_role_grant.this.to_user
}
`, region, profile, workgroup, database, first, "AccPass1"+suffix, second, "AccPass2"+suffix, roleName, owner, admin)
	}
	ctx, client := context.Background(), testAccRolesClient(t, region, profile, workgroup)
	admin := sqlclient.Connection{Database: database}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(New("test")())},
		CheckDestroy: func(*terraform.State) error {
			rows, err := client.Query(ctx, admin, "SELECT role_name FROM svv_roles WHERE role_name = :name", map[string]string{"name": roleName})
			if err == nil && len(rows) != 0 {
				err = fmt.Errorf("role %s still exists after destroy", roleName)
			}
			return err
		},
		Steps: []resource.TestStep{
			{Config: configuration("first", false), Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttr("redshift_role.this", "owner", first),
				resource.TestCheckResourceAttrSet("redshift_role.this", "role_id"),
				resource.TestCheckResourceAttr("data.redshift_role.this", "owner", first),
				resource.TestCheckResourceAttr("data.redshift_role_grant.this", "admin_option", "false"),
			)},
			{Config: configuration("first", false), PlanOnly: true},
			{Config: configuration("second", true), Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttr("redshift_role.this", "owner", second),
				resource.TestCheckResourceAttr("redshift_role_grant.this", "admin_option", "true"),
				resource.TestCheckResourceAttr("data.redshift_role_grant.this", "admin_option", "true"),
			)},
			{ResourceName: "redshift_role.this", ImportState: true, ImportStateVerify: true},
			{ResourceName: "redshift_role_grant.this", ImportState: true, ImportStateVerify: true},
			{
				Config: configuration("second", true), PlanOnly: true, ExpectNonEmptyPlan: true,
				PreConfig: func() {
					_, err := client.Query(ctx, admin, sqlclient.Stmt("REVOKE ADMIN OPTION FOR ROLE").Ident(roleName).Kw("FROM").Ident(first).String(), nil)
					require.NoError(t, err)
				},
			},
			{Config: configuration("second", true), Check: resource.TestCheckResourceAttr("redshift_role_grant.this", "admin_option", "true")},
			{Config: configuration("second", false), Check: resource.TestCheckResourceAttr("data.redshift_role_grant.this", "admin_option", "false")},
			{Config: configuration("second", false), PlanOnly: true},
		},
	})
}

// TestAccAzureIdentityProvider registers, re-parameterizes, renames, and imports a native Azure identity provider.
// Redshift rejects tenant and client IDs that Microsoft Entra ID does not know, so REDSHIFT_ACC_AZURE_TENANT_ID and
// REDSHIFT_ACC_AZURE_CLIENT_ID name a real test application. The provider is a warehouse-wide object that only a
// superuser may manage: REDSHIFT_ACC_IDENTITY_PROVIDER opts in.
func TestAccAzureIdentityProvider(t *testing.T) {
	region, profile, workgroup, database := testAccWorkgroup(t, "REDSHIFT_ACC_IDENTITY_PROVIDER", "REDSHIFT_ACC_AZURE_TENANT_ID", "REDSHIFT_ACC_AZURE_CLIENT_ID")
	tenant, clientID := os.Getenv("REDSHIFT_ACC_AZURE_TENANT_ID"), os.Getenv("REDSHIFT_ACC_AZURE_CLIENT_ID")
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	name := "acc_idp_" + suffix
	configuration := func(namespace, issuer string, version int, autoCreate bool) string {
		return fmt.Sprintf(`
provider "redshift" {
  region         = %q
  profile        = %q
  workgroup_name = %q
  database       = %q
}
resource "redshift_identity_provider" "this" {
  name                     = %q
  type                     = "AZURE"
  namespace                = %q
  issuer                   = %q
  client_id                = %q
  audience                 = ["https://analysis.windows.net/powerbi/connector/AmazonRedshift"]
  client_secret_wo         = "acc-secret-%d"
  client_secret_wo_version = %d
  auto_create_roles        = %t
}
data "redshift_identity_provider" "this" { name = redshift_identity_provider.this.name }
`, region, profile, workgroup, database, name, namespace, issuer, clientID, version, version, autoCreate)
	}
	ctx, client := context.Background(), testAccRolesClient(t, region, profile, workgroup)
	issuer, legacyIssuer := "https://login.microsoftonline.com/"+tenant+"/v2.0", "https://sts.windows.net/"+tenant+"/"
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(New("test")())},
		CheckDestroy: func(*terraform.State) error {
			rows, err := client.Query(ctx, sqlclient.Connection{Database: database}, "SELECT name FROM svv_identity_providers WHERE name = :name", map[string]string{"name": name})
			if err == nil && len(rows) != 0 {
				err = fmt.Errorf("identity provider %s still exists after destroy", name)
			}
			return err
		},
		Steps: []resource.TestStep{
			{Config: configuration("acc"+suffix, issuer, 1, true), Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttr("redshift_identity_provider.this", "type", "AZURE"),
				resource.TestCheckResourceAttrSet("redshift_identity_provider.this", "provider_id"),
				resource.TestCheckResourceAttr("data.redshift_identity_provider.this", "issuer", issuer),
				resource.TestCheckResourceAttr("data.redshift_identity_provider.this", "audience.#", "1"),
			)},
			{Config: configuration("acc"+suffix, issuer, 1, true), PlanOnly: true},
			{
				Config: configuration("acc"+suffix, legacyIssuer, 2, true),
				Check:  resource.TestCheckResourceAttr("data.redshift_identity_provider.this", "issuer", legacyIssuer),
			},
			{
				Config: configuration("accb"+suffix, legacyIssuer, 2, false),
				Check:  resource.TestCheckResourceAttr("data.redshift_identity_provider.this", "namespace", "accb"+suffix),
			},
			{Config: configuration("accb"+suffix, legacyIssuer, 2, false), PlanOnly: true},
			{
				ResourceName: "redshift_identity_provider.this", ImportState: true, ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{"client_secret_wo_version", "auto_create_roles"},
			},
		},
	})
}
