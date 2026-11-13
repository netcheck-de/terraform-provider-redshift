package provider

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAccConfiguration reads an ordinary session parameter without changing warehouse-wide settings.
func TestAccConfiguration(t *testing.T) {
	t.Parallel()

	region, workgroup, database := testAccWorkgroup(t)
	client := testAccRolesClient(t, region, workgroup)
	statement, err := readConfigurationStatement("datestyle")
	require.NoError(t, err)
	rows, err := client.Query(context.Background(), sqlclient.Connection{Database: database}, statement, nil)
	require.NoError(t, err)
	wanted, err := configurationValue("datestyle", rows)
	require.NoError(t, err)
	require.NotEmpty(t, wanted)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": testAccProviderServer},
		Steps: []resource.TestStep{{
			Config: configurationAcceptanceHCL(region, workgroup, database, "datestyle"),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.redshift_configuration.current", "name", "datestyle"),
				resource.TestCheckResourceAttr("data.redshift_configuration.current", "value", wanted),
				resource.TestCheckResourceAttr("data.redshift_configuration.current", "id", `{"name":"datestyle"}`),
			),
		}},
	})
}

// configurationAcceptanceHCL reads one parameter through the live Serverless provider session.
func configurationAcceptanceHCL(region, workgroup, database, parameter string) string {
	return fmt.Sprintf(`
provider "redshift" {
  region = %q
  workgroup_name = %q
  database = %q
}
data "redshift_configuration" "current" { name = %q }
`, region, workgroup, database, parameter)
}

// This probe changes a warehouse-wide setting, so its separate flag must only select a disposable warehouse.
// It verifies an immediately effective session value, not readback of every persisted ALTER SYSTEM setting.
func TestAccSystemConfigurationSessionValue(t *testing.T) {
	f := newLiveValidationFixture(t, "REDSHIFT_ACC_SYSTEM_SETTINGS")
	show, err := readConfigurationStatement("default_mv_auto_refresh_interval")
	require.NoError(t, err)

	rows, err := f.client.Query(context.Background(), f.conn, show, nil)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "unrecognized configuration parameter") {
		t.Skipf("this engine has no session readback for the system setting; no mutation attempted: %v", err)
	}

	require.NoError(t, err)
	initial, err := configurationValue("default_mv_auto_refresh_interval", rows)
	require.NoError(t, err)

	read := func() string {
		rows, queryErr := f.client.Query(context.Background(), f.conn, show, nil)
		require.NoError(t, queryErr)

		value, parseErr := configurationValue("default_mv_auto_refresh_interval", rows)
		require.NoError(t, parseErr)

		return value
	}
	original, err := strconv.ParseInt(initial, 10, 64)
	require.NoError(t, err)
	require.GreaterOrEqual(t, original, int64(60))
	require.LessOrEqual(t, original, int64(86400))
	t.Cleanup(func() {
		statement := sqlclient.Stmt("ALTER SYSTEM SET").KwInt("default_mv_auto_refresh_interval =", original)
		_, err := f.client.Query(context.Background(), f.conn, statement.String(), nil)
		assert.NoError(t, err, "restore the disposable warehouse's original effective refresh interval")
		assert.Equal(t, initial, read(), "verify the original effective refresh interval was restored")
	})

	wanted := int64(600)
	if original == wanted {
		wanted = 300
	}

	region, workgroup, database := testAccWorkgroup(t, "REDSHIFT_ACC_SYSTEM_SETTINGS")
	configuration := configurationAcceptanceHCL(region, workgroup, database, "default_mv_auto_refresh_interval")
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": testAccProviderServer},
		Steps: []resource.TestStep{
			{Config: configuration, Check: resource.TestCheckResourceAttr("data.redshift_configuration.current", "value", initial)},
			{Config: configuration, PreConfig: func() {
				f.exec(t, sqlclient.Stmt("ALTER SYSTEM SET").KwInt("default_mv_auto_refresh_interval =", wanted))
				assert.Equal(t, strconv.FormatInt(wanted, 10), read(), "a fresh session should observe the immediate system setting")
			}, Check: resource.TestCheckResourceAttr("data.redshift_configuration.current", "value", strconv.FormatInt(wanted, 10))},
		},
	})
}
