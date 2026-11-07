package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newExternalSchemasDataSource, resource: newExternalSchemaResource, collection: true, filters: []string{"database"}})

// TestExternalSchemasDataSource lists the external schemas SVV_EXTERNAL_SCHEMAS shows, decoded as the lookup does.
func TestExternalSchemasDataSource(t *testing.T) {
	items := discoveryTranscript(t, "discovery/external_schemas", "default_database", newExternalSchemasDataSource, nil)
	require.Len(t, items, 1)
	assert.Equal(t, types.StringValue("admin"), items[0]["database"])
	assert.Equal(t, types.StringValue("example_external"), items[0]["name"])
	assert.Equal(t, types.StringValue("DATA_CATALOG"), items[0]["source_type"])
	assert.Equal(t, types.StringValue("example_glue"), items[0]["glue_database"])
	assert.True(t, items[0]["source_database"].IsNull())
	assert.Equal(t, types.StringValue("arn:aws:iam::123456789012:role/spectrum"), items[0]["iam_role_arn"])
	assert.True(t, items[0]["region"].IsNull(), "an unrecorded region is null")
	assert.True(t, items[0]["port"].IsNull())
	assert.Equal(t, types.StringValue("admin"), items[0]["owner"])

	hidden := catalogWith(func(c *catalog) { c.family("databases").(*databasesFamily).externalHidden = true })()
	items, _, diagnostics := discoveryRead(t, newExternalSchemasDataSource, nil, hidden)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.Empty(t, items, "an external schema SVV_EXTERNAL_SCHEMAS hides is not listed")
}

// TestExternalSchemasDataSourceSources decodes the options of every source form in the selected database and skips
// source kinds the resource cannot manage.
func TestExternalSchemasDataSourceSources(t *testing.T) {
	var target sqlclient.Connection

	client := queryFunc(func(_ context.Context, connection sqlclient.Connection, _ string, _ map[string]string) ([]sqlclient.Row, error) {
		target = connection

		return []sqlclient.Row{
			{"schemaname": "federated", "eskind": "3", "databasename": "orders", "esoptions": `{"IAM_ROLE":"arn:role","SECRET_ARN":"arn:secret","URI":"db.example.com","PORT":5432,"SCHEMA":"sales"}`, "owner": "etl"},
			{"schemaname": "stream", "eskind": "10", "databasename": "", "esoptions": `{"AUTHENTICATION":"iam","IAM_ROLE":"arn:role","URI":"b-1:9098","REGION":"eu-central-1"}`, "owner": "etl"},
			{"schemaname": "system_tables", "eskind": "6", "databasename": "", "esoptions": `{}`, "owner": "rdsdb"},
		}, nil
	})
	items, identity, diagnostics := discoveryRead(t, newExternalSchemasDataSource, map[string]string{"database": "analytics"}, client)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.Equal(t, "analytics", target.Database, "the listing reads the selected database")
	assert.Equal(t, map[string]string{"database": "analytics"}, identity)
	require.Len(t, items, 2, "the system-table schema is skipped")

	federated, stream := items[0], items[1]
	assert.Equal(t, types.StringValue("POSTGRES"), federated["source_type"])
	assert.Equal(t, types.StringValue("orders"), federated["source_database"])
	assert.True(t, federated["glue_database"].IsNull())
	assert.Equal(t, types.StringValue("sales"), federated["source_schema"])
	assert.Equal(t, types.Int64Value(5432), federated["port"])
	assert.Equal(t, types.StringValue("arn:secret"), federated["secret_arn"])
	assert.Equal(t, types.StringValue("db.example.com"), federated["uri"])
	assert.True(t, federated["authentication"].IsNull())

	assert.Equal(t, types.StringValue("MSK"), stream["source_type"])
	assert.Equal(t, types.StringValue("IAM"), stream["authentication"])
	assert.Equal(t, types.StringValue("eu-central-1"), stream["region"])
	assert.True(t, stream["authentication_arn"].IsNull())

	discoveryFailures(t, newExternalSchemasDataSource, nil,
		sqlclient.Row{"schemaname": "broken", "eskind": "1", "esoptions": "not json", "owner": "etl"},
		sqlclient.Row{"schemaname": "roleless", "eskind": "1", "esoptions": "{}", "owner": "etl"},
		sqlclient.Row{"schemaname": "orphan", "eskind": "4", "esoptions": "{}", "owner": ""},
	)
}
