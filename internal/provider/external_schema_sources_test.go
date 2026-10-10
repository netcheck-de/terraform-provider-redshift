package provider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerValidateConfigCase("external_schema", validateConfigCase{
	new:   newExternalSchemaResource,
	valid: externalSchemaSourceModel("POSTGRES"),
	invalid: func() externalSchemaModel {
		data := externalSchemaSourceModel("POSTGRES")
		data.SecretARN = types.StringNull()
		return data
	}(),
	unknown: func() externalSchemaModel {
		data := externalSchemaSourceModel("POSTGRES")
		data.SecretARN, data.URI = types.StringUnknown(), types.StringUnknown()
		return data
	}(),
})

// externalSchemaSourceModel returns a complete "example_external" schema of each source form, as the fake
// catalog names every external schema.
func externalSchemaSourceModel(sourceType string) externalSchemaModel {
	data := externalSchemaFixture(sourceType, "example_external")
	data.Database = types.StringValue("admin")
	switch sourceType {
	case "DATA_CATALOG":
		data.GlueDatabase, data.IAMRoleARN = types.StringValue("example_glue"), types.StringValue("arn:aws:iam::123456789012:role/spectrum")
	case "HIVE_METASTORE":
		data.SourceDatabase, data.URI, data.Port = types.StringValue("hive_db"), types.StringValue("172.10.10.10"), types.Int64Value(9083)
		data.IAMRoleARN = types.StringValue("arn:aws:iam::123456789012:role/hive")
	case "POSTGRES", "MYSQL":
		data.SourceDatabase, data.URI, data.Port = types.StringValue("orders"), types.StringValue("aurora.example.internal"), types.Int64Value(5432)
		data.IAMRoleARN = types.StringValue("arn:aws:iam::123456789012:role/federated")
		data.SecretARN = types.StringValue("arn:aws:secretsmanager:eu-central-1:123456789012:secret:aurora-AbCdEf")
		if sourceType == "POSTGRES" {
			data.SourceSchema = types.StringValue("public")
		}
	case "REDSHIFT":
		data.SourceDatabase, data.SourceSchema = types.StringValue("analytics"), types.StringValue("public")
	case "KINESIS":
		data.IAMRoleARN, data.Region = types.StringValue("arn:aws:iam::123456789012:role/kinesis"), types.StringValue("us-west-2")
	case "MSK":
		data = externalSchemaMSK("iam")
		data.Name, data.Database = types.StringValue("example_external"), types.StringValue("admin")
	}
	return data
}

// externalSchemaCatalog returns a full fake whose external schema was created from data, without recorded writes.
func externalSchemaCatalog(t *testing.T, data externalSchemaModel) func() sqlclient.Client {
	t.Helper()
	statement, err := createExternalSchemaStatement(data)
	require.NoError(t, err)
	return func() sqlclient.Client {
		c := fullCatalog()
		c.external = false
		_, err := c.Query(context.Background(), sqlclient.Connection{Database: "admin"}, statement, nil)
		require.NoError(t, err)
		c.writes = nil
		return c
	}
}

// TestExternalSchemaAlterCoverage checks that every in-place external schema option has an alter step.
func TestExternalSchemaAlterCoverage(t *testing.T) {
	assertAlterCoverage(t, newExternalSchemaResource(), externalSchemaAlterSteps)
}

// TestExternalSchemaSourceTranscripts records every source form's creation and the supported in-place changes.
func TestExternalSchemaSourceTranscripts(t *testing.T) {
	absent := catalogWith(func(c *catalog) { c.external = false })
	var cases []transcriptCase
	for _, sourceType := range []string{"HIVE_METASTORE", "POSTGRES", "MYSQL", "REDSHIFT", "KINESIS", "MSK"} {
		cases = append(cases, transcriptCase{name: "create_" + strings.ToLower(sourceType), operation: "create", catalog: absent, planned: externalSchemaSourceModel(sourceType)})
	}
	glue := externalSchemaSourceModel("DATA_CATALOG")
	owned := glue
	owned.Owner = types.StringValue(`Etl"Owner`)
	rotated := owned
	rotated.IAMRoleARN = types.StringValue("arn:aws:iam::123456789012:role/rotated")
	msk := externalSchemaSourceModel("MSK")
	certificate := msk
	certificate.Authentication = types.StringValue("mtls")
	certificate.AuthenticationARN = types.StringValue("arn:aws:acm:eu-central-1:123456789012:certificate/example")
	certificate.URI = types.StringValue("b-1.example.kafka.eu-central-1.amazonaws.com:9094")
	cases = append(cases,
		transcriptCase{name: "create_owner", operation: "create", catalog: absent, planned: owned},
		transcriptCase{name: "update_glue_role_and_owner", operation: "update", catalog: externalSchemaCatalog(t, glue), prior: glue, planned: rotated},
		transcriptCase{name: "update_msk_mtls", operation: "update", catalog: externalSchemaCatalog(t, msk), prior: msk, planned: certificate},
		transcriptCase{name: "read_redshift", operation: "read", catalog: externalSchemaCatalog(t, externalSchemaSourceModel("REDSHIFT")), prior: externalSchemaSourceModel("REDSHIFT")},
		transcriptCase{name: "import_postgres", operation: "import", catalog: absent, planned: externalSchemaSourceModel("POSTGRES")},
	)
	runTranscripts(t, "external_schema_sources", newExternalSchemaResource, cases)
}

// TestExternalSchemaConditionalReplacement covers both branches of the attributes ALTER EXTERNAL SCHEMA changes for
// some source forms only.
func TestExternalSchemaConditionalReplacement(t *testing.T) {
	ctx := context.Background()
	r := newExternalSchemaResource()
	var response resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &response)
	value := types.StringValue("after")
	for _, test := range []struct {
		attribute, source string
		planned           types.String
		replace           bool
	}{
		{"iam_role_arn", "DATA_CATALOG", value, false},
		{"iam_role_arn", "MSK", value, false},
		{"iam_role_arn", "MSK", types.StringNull(), true},
		{"iam_role_arn", "POSTGRES", value, true},
		{"iam_role_arn", "KINESIS", value, true},
		{"uri", "MSK", value, false},
		{"uri", "HIVE_METASTORE", value, true},
		{"uri", "MYSQL", value, true},
		{"secret_arn", "MSK", value, false},
		{"secret_arn", "MSK", types.StringNull(), false},
		{"secret_arn", "POSTGRES", value, true},
	} {
		t.Run(test.attribute+"/"+test.source+"/"+test.planned.String(), func(t *testing.T) {
			state := testState(t, r, externalSchemaSourceModel(test.source))
			request := planmodifier.StringRequest{
				Path: path.Root(test.attribute), Plan: tfsdk.Plan(state), State: state, Config: tfsdk.Config(state),
				PlanValue: test.planned, ConfigValue: test.planned, StateValue: types.StringValue("before"),
			}
			replace := false
			for _, modifier := range response.Schema.Attributes[test.attribute].(schema.StringAttribute).PlanModifiers {
				result := planmodifier.StringResponse{PlanValue: request.PlanValue}
				modifier.PlanModifyString(ctx, request, &result)
				require.False(t, result.Diagnostics.HasError(), "%v", result.Diagnostics)
				replace = replace || result.RequiresReplace
			}
			assert.Equal(t, test.replace, replace)
		})
	}
	// A source type that is not known yet cannot rule out replacement.
	unknown := externalSchemaSourceModel("MSK")
	unknown.SourceType = types.StringUnknown()
	state := testState(t, r, unknown)
	request := planmodifier.StringRequest{Path: path.Root("uri"), Plan: tfsdk.Plan(state), State: state, PlanValue: value, StateValue: types.StringValue("before")}
	var result stringplanmodifier.RequiresReplaceIfFuncResponse
	externalSchemaInPlace("uri")(ctx, request, &result)
	assert.True(t, result.RequiresReplace)
}

// TestValidateExternalSchemaRules pins the per-source requirements, including the streaming certificate rule.
func TestValidateExternalSchemaRules(t *testing.T) {
	with := func(sourceType string, change func(*externalSchemaModel)) externalSchemaModel {
		data := externalSchemaSourceModel(sourceType)
		change(&data)
		return data
	}
	for name, test := range map[string]struct {
		data    externalSchemaModel
		problem string
	}{
		"glue":                 {data: externalSchemaSourceModel("DATA_CATALOG")},
		"legacy default":       {data: with("DATA_CATALOG", func(d *externalSchemaModel) { d.SourceType = types.StringNull() })},
		"unknown source":       {data: with("DATA_CATALOG", func(d *externalSchemaModel) { d.SourceType = types.StringUnknown(); d.URI = types.StringValue("x") })},
		"glue source database": {data: with("DATA_CATALOG", func(d *externalSchemaModel) { d.SourceDatabase = types.StringValue("x") }), problem: "DATA_CATALOG does not accept source_database"},
		"hive":                 {data: externalSchemaSourceModel("HIVE_METASTORE")},
		"hive region":          {data: with("HIVE_METASTORE", func(d *externalSchemaModel) { d.Region = types.StringValue("us-east-1") }), problem: "does not accept region"},
		"redshift":             {data: externalSchemaSourceModel("REDSHIFT")},
		"redshift no database": {data: with("REDSHIFT", func(d *externalSchemaModel) { d.SourceDatabase = types.StringNull() }), problem: "REDSHIFT requires source_database"},
		"kinesis":              {data: externalSchemaSourceModel("KINESIS")},
		"kinesis uri":          {data: with("KINESIS", func(d *externalSchemaModel) { d.URI = types.StringValue("x") }), problem: "KINESIS does not accept uri"},
		"msk":                  {data: externalSchemaSourceModel("MSK")},
		"msk without auth":     {data: with("MSK", func(d *externalSchemaModel) { d.Authentication = types.StringNull() }), problem: "MSK requires authentication"},
		"msk mtls no source":   {data: with("MSK", func(d *externalSchemaModel) { d.Authentication = types.StringValue("mtls") }), problem: "exactly one"},
		"msk mtls unknown": {data: with("MSK", func(d *externalSchemaModel) {
			d.Authentication = types.StringValue("mtls")
			d.SecretARN = types.StringUnknown()
		})},
		"msk iam secret": {data: with("MSK", func(d *externalSchemaModel) { d.SecretARN = types.StringValue("x") }), problem: "apply only to AUTHENTICATION mtls"},
		"msk unknown auth": {data: with("MSK", func(d *externalSchemaModel) {
			d.Authentication = types.StringUnknown()
			d.IAMRoleARN = types.StringNull()
		})},
		"unsupported": {data: with("DATA_CATALOG", func(d *externalSchemaModel) { d.SourceType = types.StringValue("KAFKA") }), problem: "unsupported"},
	} {
		t.Run(name, func(t *testing.T) {
			err := validateExternalSchema(test.data, false)
			if test.problem == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, test.problem)
			}
		})
	}
	// At apply time an unknown option is a computed one left unconfigured, so a required option it stands for is missing.
	unconfigured := externalSchemaSourceModel("POSTGRES")
	unconfigured.URI = types.StringUnknown()
	require.NoError(t, validateExternalSchema(unconfigured, false))
	require.ErrorContains(t, validateExternalSchema(unconfigured, true), "POSTGRES requires uri")
}

// TestExternalSchemaRefreshesDrift reports a Glue database or role changed outside Terraform as a diff, so the
// plan replaces the schema or alters the role, instead of failing the refresh.
func TestExternalSchemaRefreshesDrift(t *testing.T) {
	c := fullCatalog()
	f := fakeState[*databasesFamily](c, "databases")
	f.externalDatabase = "moved_glue"
	f.externalOptions["IAM_ROLE"] = "arn:aws:iam::123456789012:role/other"
	r := &externalSchemaResource{testResourceClient(c)}
	data := externalSchemaSourceModel("DATA_CATALOG")
	found, err := r.read(context.Background(), &data)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "moved_glue", data.GlueDatabase.ValueString())
	assert.Equal(t, "arn:aws:iam::123456789012:role/other", data.IAMRoleARN.ValueString())
	assert.Equal(t, "admin", data.Owner.ValueString())
}

// TestExternalSchemaKeepsUnrecordedOptions keeps configured options the catalog does not record and decodes
// numeric JSON options.
func TestExternalSchemaKeepsUnrecordedOptions(t *testing.T) {
	r := &externalSchemaResource{testResourceClient(queryFunc(func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
		return []sqlclient.Row{{"schemaname": "example_external", "eskind": "3", "databasename": "orders", "owner": "etl", "esoptions": `{"iam_role":"role","port":6543,"ssl":true}`}}, nil
	}))}
	data := externalSchemaSourceModel("POSTGRES")
	found, err := r.read(context.Background(), &data)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "role", data.IAMRoleARN.ValueString())
	assert.Equal(t, int64(6543), data.Port.ValueInt64())
	assert.Equal(t, "aurora.example.internal", data.URI.ValueString(), "URI is not recorded, so the configured value stays")
	assert.Equal(t, "public", data.SourceSchema.ValueString())
	assert.True(t, data.Region.IsNull())
	imported := externalSchemaModel{Database: types.StringValue("admin"), Name: types.StringValue("example_external"), Port: types.Int64Unknown(), SourceSchema: types.StringUnknown()}
	_, err = r.read(context.Background(), &imported)
	require.NoError(t, err)
	assert.Equal(t, "POSTGRES", imported.SourceType.ValueString())
	assert.Equal(t, "orders", imported.SourceDatabase.ValueString())
	assert.True(t, imported.URI.IsNull())
	assert.True(t, imported.SourceSchema.IsNull())
}

// TestExternalSchemaUpdateFailures rejects changes ALTER EXTERNAL SCHEMA cannot make and unconverged changes.
func TestExternalSchemaUpdateFailures(t *testing.T) {
	postgres := externalSchemaSourceModel("POSTGRES")
	rotated := postgres
	rotated.SecretARN = types.StringValue("arn:aws:secretsmanager:eu-central-1:123456789012:secret:rotated")
	c := externalSchemaCatalog(t, postgres)().(*catalog)
	_, diagnostics := applyOperation(t, &externalSchemaResource{testResourceClient(c)}, "update", postgres, rotated, nil)
	require.True(t, diagnostics.HasError())
	assert.Contains(t, diagnostics.Errors()[0].Detail(), "cannot change secret_arn in place")
	assert.Empty(t, c.writes)

	glue := externalSchemaSourceModel("DATA_CATALOG")
	moved := glue
	moved.IAMRoleARN = types.StringValue("arn:aws:iam::123456789012:role/rotated")
	ignored := fullCatalog()
	client := queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		if strings.HasPrefix(sql, "ALTER EXTERNAL SCHEMA") {
			return nil, nil
		}
		return ignored.Query(ctx, target, sql, parameters)
	})
	_, diagnostics = applyOperation(t, &externalSchemaResource{testResourceClient(client)}, "update", glue, moved, nil)
	require.True(t, diagnostics.HasError())
	assert.Contains(t, diagnostics.Errors()[0].Detail(), "iam_role_arn is")

	_, diagnostics = applyOperation(t, &externalSchemaResource{testResourceClient(catalogWith(func(c *catalog) { c.external = false })())}, "update", glue, moved, nil)
	require.True(t, diagnostics.HasError())
	assert.Contains(t, diagnostics.Errors()[0].Detail(), "disappeared")
}

// TestExternalSchemaCreateFailures keeps known state when assigning the owner fails after creation, and rejects
// invalid forms before any SQL.
func TestExternalSchemaCreateFailures(t *testing.T) {
	c := catalogWith(func(c *catalog) { c.external = false })().(*catalog)
	client := queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		if strings.HasPrefix(sql, "ALTER SCHEMA") {
			return nil, errors.New("permission denied")
		}
		return c.Query(ctx, target, sql, parameters)
	})
	owned := externalSchemaSourceModel("REDSHIFT")
	owned.Owner = types.StringValue("etl")
	owned.SourceSchema, owned.Port = types.StringUnknown(), types.Int64Unknown()
	state, diagnostics := applyOperation(t, &externalSchemaResource{testResourceClient(client)}, "create", nil, owned, nil)
	require.True(t, diagnostics.HasError())
	assert.Contains(t, diagnostics.Errors()[0].Detail(), "permission denied")
	assert.True(t, state.Raw.IsFullyKnown())
	var observed externalSchemaModel
	require.False(t, state.Get(context.Background(), &observed).HasError())
	assert.False(t, observed.ID.IsNull(), "the created schema stays in state so it is not orphaned")

	invalid := externalSchemaSourceModel("REDSHIFT")
	invalid.IAMRoleARN = types.StringValue("role")
	empty := fullCatalog()
	empty.external = false
	_, diagnostics = applyOperation(t, &externalSchemaResource{testResourceClient(empty)}, "create", nil, invalid, nil)
	require.True(t, diagnostics.HasError())
	assert.Empty(t, empty.writes)
}

// TestExternalSchemaHiddenFromRegularUser reports a schema that SVV_EXTERNAL_SCHEMAS hides from a non-superuser
// after an ownership transfer as an error, so Read never removes a live schema and Create never calls it absent.
func TestExternalSchemaHiddenFromRegularUser(t *testing.T) {
	glue := externalSchemaSourceModel("DATA_CATALOG")
	identity := testResourceClient(fullCatalog())
	glue.ID = identity.identity("admin", map[string]string{"name": "example_external"})
	hidden := func() sqlclient.Client {
		c := externalSchemaCatalog(t, glue)().(*catalog)
		fakeState[*databasesFamily](c, "databases").externalHidden = true
		return c
	}
	state, diagnostics := applyOperation(t, &externalSchemaResource{testResourceClient(hidden())}, "read", glue, nil, nil)
	require.True(t, diagnostics.HasError())
	assert.Contains(t, diagnostics.Errors()[0].Detail(), "only a superuser can read")
	assert.False(t, state.Raw.IsNull(), "a hidden schema stays in state")

	c := catalogWith(func(c *catalog) { c.external = false })().(*catalog)
	client := queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
		rows, err := c.Query(ctx, target, sql, parameters)
		if strings.HasPrefix(sql, "ALTER SCHEMA") && err == nil {
			// The provider's regular user no longer owns the schema it just handed over.
			fakeState[*databasesFamily](c, "databases").externalHidden = true
		}
		return rows, err
	})
	owned := glue
	owned.ID, owned.Owner = types.StringNull(), types.StringValue("etl")
	_, diagnostics = applyOperation(t, &externalSchemaResource{testResourceClient(client)}, "create", nil, owned, nil)
	require.True(t, diagnostics.HasError())
	assert.Contains(t, diagnostics.Errors()[0].Detail(), "only a superuser can read")
	assert.NotContains(t, diagnostics.Errors()[0].Detail(), "absent")
}
