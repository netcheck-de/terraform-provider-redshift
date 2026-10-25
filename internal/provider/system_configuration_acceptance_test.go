package provider

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	})

	wanted := int64(600)
	if original == wanted {
		wanted = 300
	}

	f.exec(t, sqlclient.Stmt("ALTER SYSTEM SET").KwInt("default_mv_auto_refresh_interval =", wanted))
	assert.Equal(t, strconv.FormatInt(wanted, 10), read(), "a fresh session should observe the immediate system setting")
}
