package provider

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/redshiftdata"
	"github.com/aws/aws-sdk-go-v2/service/redshiftserverless"
	"github.com/netcheck-de/terraform-provider-redshift/internal/redshiftconn"
	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/redshiftdata"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAccDirectPasswordPrivileges verifies strict TLS and actual non-superuser read/write permissions on isolated data.
func TestAccDirectPasswordPrivileges(t *testing.T) {
	region, profile, workgroup, database := testAccWorkgroup(t, "REDSHIFT_ACC_DIRECT")
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region), config.WithSharedConfigProfile(profile))
	require.NoError(t, err)
	sdk := redshiftserverless.NewFromConfig(cfg)
	routing, err := (&redshiftconn.IAM{Workgroup: workgroup, Serverless: sdk}).Resolve(ctx, database)
	require.NoError(t, err)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	dbName, userName := "acc_password_"+suffix, "acc_reader_"+suffix
	secret := make([]byte, 16)
	_, err = rand.Read(secret)
	require.NoError(t, err)
	password := "Aa1" + hex.EncodeToString(secret)
	admin := &dataapi.Client{API: redshiftdata.NewFromConfig(cfg), Workgroup: workgroup, Timeout: 5 * time.Minute, Poll: time.Second}
	adminDB, target := sqlclient.Connection{Database: database}, sqlclient.Connection{Database: dbName}
	t.Cleanup(func() {
		rows, err := admin.Query(ctx, adminDB, "SELECT database_name FROM svv_redshift_databases WHERE database_name = :name", map[string]string{"name": dbName})
		require.NoError(t, err)
		if len(rows) != 0 {
			_, err = admin.Query(ctx, adminDB, "DROP DATABASE "+sqlclient.Identifier(dbName), nil)
			require.NoError(t, err)
		}
		rows, err = admin.Query(ctx, adminDB, "SELECT usename FROM pg_user WHERE usename = :name", map[string]string{"name": userName})
		require.NoError(t, err)
		if len(rows) != 0 {
			_, err = admin.Query(ctx, adminDB, "DROP USER "+sqlclient.Identifier(userName), nil)
			require.NoError(t, err)
		}
	})
	_, err = admin.Query(ctx, adminDB, "CREATE DATABASE "+sqlclient.Identifier(dbName), nil)
	require.NoError(t, err)
	_, err = admin.Query(ctx, adminDB, "CREATE USER "+sqlclient.Identifier(userName)+" PASSWORD "+sqlclient.Literal(password)+" NOCREATEDB NOCREATEUSER", nil)
	require.NoError(t, err)
	_, err = admin.Query(ctx, target, "CREATE TABLE public.fixture (id INTEGER)", nil)
	require.NoError(t, err)
	_, err = admin.Query(ctx, target, "INSERT INTO public.fixture VALUES (1)", nil)
	require.NoError(t, err)
	_, err = admin.Query(ctx, target, "GRANT SELECT ON TABLE public.fixture TO "+sqlclient.Identifier(userName), nil)
	require.NoError(t, err)
	reader := &redshiftconn.Client{Credentials: redshiftconn.Credentials{Host: routing.Host, Port: routing.Port, Username: userName, Password: password}, Timeout: 30 * time.Second}
	rows, err := reader.Query(ctx, target, "SELECT id FROM public.fixture WHERE id = CAST(:id AS INTEGER)", map[string]string{"id": "1"})
	require.NoError(t, err)
	assert.Equal(t, []sqlclient.Row{{"id": "1"}}, rows)
	_, err = reader.Query(ctx, target, "INSERT INTO public.fixture VALUES (2)", nil)
	require.ErrorContains(t, err, "permission denied")
}
