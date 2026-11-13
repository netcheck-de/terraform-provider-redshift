package provider

import (
	"context"
	"strings"
	"sync"
	"testing"
	"testing/synctest"

	frameworkprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/redshiftdata"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
)

// testAccMutationGates protect warehouse catalogs across independent test/provider instances, not whole lifecycles.
// Reads, Terraform planning, and different warehouses remain concurrent.
var testAccMutationGates sync.Map

// testAccProvider adds test-process-wide catalog write isolation without changing production provider behavior.
// Embed the concrete provider so optional interfaces such as configuration validation remain available.
type testAccProvider struct {
	*redshiftProvider
}

var _ frameworkprovider.ProviderWithValidateConfig = (*testAccProvider)(nil)

// Configure shares only the mutation gate; routing, credentials, administration database, and state stay independent.
func (p *testAccProvider) Configure(ctx context.Context, req frameworkprovider.ConfigureRequest, resp *frameworkprovider.ConfigureResponse) {
	p.redshiftProvider.Configure(ctx, req, resp)

	if resp.Diagnostics.HasError() || resp.ResourceData == nil {
		return
	}

	data := resp.ResourceData.(providerData)
	if data.client == nil {
		return
	}

	data.client = testAccCatalogClient(data.warehouse.field+"/"+data.warehouse.value.ValueString(), data.client)
	resp.ResourceData, resp.DataSourceData = data, data
}

// testAccClientKey carries one call's transport through the shared serializer without sharing credentials or clients.
type testAccClientKey struct{}

// testAccGatedClient dispatches each call to its own client through a warehouse-wide mutation gate.
type testAccGatedClient struct {
	client, gate sqlclient.Client
}

// Query reuses production mutation classification, cancellation, and unsettled-statement protection.
func (c testAccGatedClient) Query(ctx context.Context, connection sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
	return c.gate.Query(context.WithValue(ctx, testAccClientKey{}, c.client), connection, sql, parameters)
}

// testAccCatalogClient makes writes from provider servers and fixture/drift helpers obey the same warehouse gate.
func testAccCatalogClient(warehouse string, client sqlclient.Client) sqlclient.Client {
	gate := sqlclient.SerializeMutations(queryFunc(func(ctx context.Context, connection sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		return ctx.Value(testAccClientKey{}).(sqlclient.Client).Query(ctx, connection, sql, parameters)
	}))
	shared, _ := testAccMutationGates.LoadOrStore(warehouse, gate)

	return testAccGatedClient{client: client, gate: shared.(sqlclient.Client)}
}

// testAccDataClient applies the same gate to live Data API fixtures, drift injection, and fallback cleanup.
func testAccDataClient(client *dataapi.Client) sqlclient.Client {
	warehouse := "workgroup_name/" + client.Workgroup
	if client.Workgroup == "" {
		warehouse = "cluster_identifier/" + client.Cluster
	}

	return testAccCatalogClient(warehouse, client)
}

// TestLiveCatalogWriteGate proves that independent clients cannot overlap writes to one warehouse, while reads and
// another warehouse can make progress. Virtual time is not used to control Terraform subprocesses or network I/O.
func TestLiveCatalogWriteGate(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		entered := make(chan string, 4)
		release := make(chan struct{})
		transport := queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
			entered <- sql

			if !strings.HasPrefix(sql, "SELECT") {
				<-release
			}

			return nil, nil
		})
		first := testAccCatalogClient(t.Name(), transport)
		second := testAccCatalogClient(t.Name(), transport)
		other := testAccCatalogClient(t.Name()+"/other", transport)
		connection := sqlclient.Connection{Database: "admin"}

		query := func(client sqlclient.Client, sql string) {
			_, err := client.Query(context.Background(), connection, sql, nil)
			assert.NoError(t, err)
		}

		go query(first, sqlclient.Stmt("CREATE ROLE").Ident("first").String())

		synctest.Wait()

		go query(second, sqlclient.Stmt("CREATE ROLE").Ident("second").String())
		go query(second, "SELECT 1")
		go query(other, sqlclient.Stmt("CREATE ROLE").Ident("other").String())

		synctest.Wait()
		assert.Len(t, entered, 3, "only the second write to the same warehouse must wait")
		close(release)
		synctest.Wait()
		assert.Len(t, entered, 4, "the queued write must execute after the first write settles")
	})
}
