package provider

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/redshiftdata"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	frameworkresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/redshiftdata"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// integrationSessionAPI keeps SET SESSION AUTHORIZATION and provider reads in the same isolated Data API session,
// avoiding a Secrets Manager or IAM change just to exercise a non-superuser SQL identity.
type integrationSessionAPI struct {
	*redshiftdata.Client

	mu      sync.Mutex
	session *string
}

// ExecuteStatement reuses the session the first statement opened, clearing the connection fields a session-bound call
// must omit.
func (a *integrationSessionAPI) ExecuteStatement(ctx context.Context, input *redshiftdata.ExecuteStatementInput, options ...func(*redshiftdata.Options)) (*redshiftdata.ExecuteStatementOutput, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	copyInput := *input

	copyInput.SessionKeepAliveSeconds = aws.Int32(60)
	if a.session != nil {
		copyInput.SessionId = a.session
		copyInput.Database, copyInput.WorkgroupName, copyInput.SecretArn = nil, nil, nil
	}

	output, err := a.Client.ExecuteStatement(ctx, &copyInput, options...)
	if err == nil {
		a.session = output.SessionId
	}

	return output, err
}

// TestAccDatabaseLookupNonSuperuser verifies the permission-denied behavior of SVV_INTEGRATION without allowing
// optional integration metadata to break an ordinary local database lookup. Only its disposable SQL user is created.
func TestAccDatabaseLookupNonSuperuser(t *testing.T) {
	region, profile, workgroup, database := testAccWorkgroup(t, "REDSHIFT_ACC_DYNAMODB_DATABASE", "REDSHIFT_ACC_SECRET_ARN")
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region), config.WithSharedConfigProfile(profile))
	require.NoError(t, err)

	api := redshiftdata.NewFromConfig(cfg)
	admin := &dataapi.Client{API: api, Workgroup: workgroup, SecretARN: os.Getenv("REDSHIFT_ACC_SECRET_ARN"), Timeout: time.Minute, Poll: time.Second}
	conn := sqlclient.Connection{Database: database}
	user := "acc_zetl_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	_, err = admin.Query(ctx, conn, sqlclient.Stmt("CREATE USER").Ident(user).Kw("PASSWORD DISABLE NOCREATEDB NOCREATEUSER").String(), nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := admin.Query(context.Background(), conn, sqlclient.Stmt("REVOKE SELECT ON pg_catalog.pg_database_info FROM").Ident(user).String(), nil)
		assert.NoError(t, err)
		_, err = admin.Query(context.Background(), conn, sqlclient.Stmt("DROP USER").Ident(user).String(), nil)
		assert.NoError(t, err)
	})
	// The ordinary database metadata has its own catalog permission; grant only that read, leaving the integration
	// view inaccessible so this test isolates the optional zero-ETL metadata rather than PG_DATABASE_INFO access.
	_, err = admin.Query(ctx, conn, sqlclient.Stmt("GRANT SELECT ON pg_catalog.pg_database_info TO").Ident(user).String(), nil)
	require.NoError(t, err)

	scoped := &dataapi.Client{API: &integrationSessionAPI{Client: api}, Workgroup: workgroup, SecretARN: admin.SecretARN, Timeout: time.Minute, Poll: time.Second}

	t.Cleanup(func() {
		_, err := scoped.Query(context.Background(), conn, "SET SESSION AUTHORIZATION DEFAULT", nil)
		assert.NoError(t, err)
	})

	_, err = scoped.Query(ctx, conn, sqlclient.Stmt("SET SESSION AUTHORIZATION").Ident(user).String(), nil)
	require.NoError(t, err)
	rows, err := scoped.Query(ctx, conn, "SELECT usesuper FROM pg_user WHERE usename = current_user", nil)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "false", rows[0]["usesuper"])

	_, err = scoped.Query(ctx, conn, "SELECT integration_id FROM svv_integration", nil)
	require.ErrorContains(t, err, "permission denied for relation svv_integration")
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){
			"redshift": providerserver.NewProtocol6WithError(&redshiftProvider{version: "test", client: queryFunc(scoped.Query)}),
		},
		Steps: []resource.TestStep{{
			Config: fmt.Sprintf(`
provider "redshift" {
  region         = %q
  profile        = %q
  workgroup_name = %q
  database       = %q
}
data "redshift_database" "local" {
  name = %q
}
`, region, profile, workgroup, database, database),
			Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttr("data.redshift_database.local", "name", database),
				resource.TestCheckNoResourceAttr("data.redshift_database.local", "integration.integration_id"),
			),
		}},
	})
}

// TestAccDatabaseIntegrationDynamoDBLookup only observes an explicitly supplied destination: Terraform must not
// manage or destroy the active integration fixture. Use the SQL probe log to verify mutations and their restoration.
func TestAccDatabaseIntegrationDynamoDBLookup(t *testing.T) {
	region, profile, workgroup, database := testAccWorkgroup(t, "REDSHIFT_ACC_DYNAMODB_DATABASE", "REDSHIFT_ACC_DYNAMODB_INTEGRATION_ID", "REDSHIFT_ACC_SECRET_ARN")
	configuration := fmt.Sprintf(`
provider "redshift" {
  region         = %q
  profile        = %q
  workgroup_name = %q
  database       = %q
  secret_arn     = %q
}
data "redshift_database" "destination" {
  name = %q
}
`, region, profile, workgroup, database, os.Getenv("REDSHIFT_ACC_SECRET_ARN"), os.Getenv("REDSHIFT_ACC_DYNAMODB_DATABASE"))
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){
			"redshift": providerserver.NewProtocol6WithError(New("test")()),
		},
		Steps: []resource.TestStep{{
			Config: configuration,
			Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttr("data.redshift_database.destination", "database_type", "LOCAL"),
				resource.TestCheckResourceAttr("data.redshift_database.destination", "integration.integration_id", os.Getenv("REDSHIFT_ACC_DYNAMODB_INTEGRATION_ID")),
				resource.TestCheckNoResourceAttr("data.redshift_database.destination", "integration.source_database"),
			),
		}},
	})
}

// TestAccDatabaseIntegrationExistingUpdate exercises framework Update without importing or taking Terraform
// ownership of the active fixture. It never calls Create/Delete; cleanup restores the two settings it changes.
func TestAccDatabaseIntegrationExistingUpdate(t *testing.T) {
	region, profile, workgroup, database := testAccWorkgroup(t, "REDSHIFT_ACC_DYNAMODB_DATABASE", "REDSHIFT_ACC_DYNAMODB_INTEGRATION_ID", "REDSHIFT_ACC_SECRET_ARN", "REDSHIFT_ACC_INTEGRATION_MUTATIONS")
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region), config.WithSharedConfigProfile(profile))
	require.NoError(t, err)

	client := &dataapi.Client{API: redshiftdata.NewFromConfig(cfg), Workgroup: workgroup, SecretARN: os.Getenv("REDSHIFT_ACC_SECRET_ARN"), Timeout: time.Minute, Poll: time.Second}
	r := &databaseResource{
		client: client, database: types.StringValue(database),
		warehouse: warehouseBinding{field: "workgroup_name", value: types.StringValue(workgroup)},
	}
	prior := databaseModel{
		Name: types.StringValue(os.Getenv("REDSHIFT_ACC_DYNAMODB_DATABASE")), DatashareARN: types.StringNull(), WithPermissions: types.BoolValue(true),
		Integration: &databaseIntegrationModel{IntegrationID: types.StringValue(os.Getenv("REDSHIFT_ACC_DYNAMODB_INTEGRATION_ID"))},
	}
	prior.ID = r.identity(database, map[string]string{"name": prior.Name.ValueString(), "integration_id": prior.Integration.IntegrationID.ValueString()})
	found, err := r.read(ctx, &prior)
	require.NoError(t, err)
	require.True(t, found)
	require.NotNil(t, prior.Integration)
	planned := prior
	integration := *prior.Integration

	interval := int64(1800)
	if prior.Integration.RefreshInterval.ValueInt64() == interval {
		interval = 900
	}

	integration.RefreshInterval = types.Int64Value(interval)
	integration.AutoRemediation = types.BoolValue(!prior.Integration.AutoRemediation.ValueBool())
	planned.Integration = &integration
	restore, err := alterDatabaseStatements(planned, prior)
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		err := r.exec(cleanupCtx, database, restore...)
		assert.NoError(t, err, "restore the existing integration settings")

		observed := prior
		found, err := r.read(cleanupCtx, &observed)
		assert.NoError(t, err)
		assert.True(t, found)

		if err == nil && found {
			assert.NoError(t, r.waitDatabaseIntegration(cleanupCtx, prior, &observed), "wait for restored integration settings")
		}

		t.Log("Restored original integration settings; existing database/integration retained")
	})
	state := testState(t, r, prior)
	response := frameworkresource.UpdateResponse{State: state}
	started := time.Now()

	r.Update(ctx, frameworkresource.UpdateRequest{State: state, Plan: tfsdk.Plan(testState(t, r, planned))}, &response)
	require.False(t, response.Diagnostics.HasError(), "%v", response.Diagnostics)

	var observed databaseResourceModel
	require.False(t, response.State.Get(ctx, &observed).HasError())
	assert.Equal(t, interval, observed.Integration.RefreshInterval.ValueInt64())
	assert.Equal(t, integration.AutoRemediation.ValueBool(), observed.Integration.AutoRemediation.ValueBool())
	t.Logf("Framework Update converged after %s", time.Since(started).Round(time.Second))
}
