package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = registerParity(parityCase{source: newMaskingPolicyAttachmentsDataSource, resource: newMaskingPolicyAttachmentResource, collection: true, filters: []string{"database", "policy", "schema", "relation"}})

// maskingAttachmentListing reads a listing and returns its elements' attributes.
func maskingAttachmentListing(t *testing.T, filters map[string]string, client sqlclient.Client) []map[string]attr.Value {
	t.Helper()

	source := newMaskingPolicyAttachmentsDataSource()
	state, diagnostics := readSource(t, source, collectionConfig(t, source, filters), client)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)

	var data types.Object
	require.False(t, state.Get(context.Background(), &data).HasError())

	var items []map[string]attr.Value
	for _, element := range collectionResult(source, data).Elements() {
		items = append(items, element.(types.Object).Attributes())
	}

	return items
}

// TestMaskingPolicyAttachmentsListing lists the attachments of the provider database with their decoded columns,
// narrows them by policy, schema, and relation, and reports catalog failures and malformed rows.
func TestMaskingPolicyAttachmentsListing(t *testing.T) {
	expected := map[string]attr.Value{
		"database": types.StringValue("admin"), "policy": types.StringValue("mask_email"), "schema": types.StringValue("public"),
		"relation": types.StringValue("customers"), "grantee": types.StringValue("example_readers"), "grantee_type": types.StringValue("ROLE"),
		"columns": maskingAttachmentNameList([]string{"email"}), "input_columns": maskingAttachmentNameList([]string{"email"}),
		"priority": types.Int64Value(10),
	}

	for _, filters := range []map[string]string{nil, {"database": "admin", "policy": "mask_email", "schema": "public", "relation": "customers"}} {
		assert.Equal(t, []map[string]attr.Value{expected}, maskingAttachmentListing(t, filters, fullCatalog()))
	}

	for _, filters := range []map[string]string{{"policy": "other"}, {"schema": "other"}, {"relation": "other"}} {
		assert.Empty(t, maskingAttachmentListing(t, filters, fullCatalog()), "%v", filters)
	}

	var (
		target     sqlclient.Connection
		parameters map[string]string
		rows       []sqlclient.Row
	)

	client := queryFunc(func(_ context.Context, connection sqlclient.Connection, sql string, params map[string]string) ([]sqlclient.Row, error) {
		require.Contains(t, sql, "FROM svv_attached_masking_policy")

		target, parameters = connection, params

		return rows, nil
	})
	rows = []sqlclient.Row{{
		"policy_name": "mask_card", "schema_name": "sales", "table_name": "cards", "grantee": "public", "grantee_type": "public",
		"priority": "100", "input_columns": `["is_fraud", "card_number"]`, "output_columns": `["card_number"]`,
	}}
	items := maskingAttachmentListing(t, map[string]string{"database": "analytics", "policy": "mask_card"}, client)
	assert.Equal(t, "analytics", target.Database, "the view covers only the connected database")
	assert.Equal(t, map[string]string{"policy": "mask_card"}, parameters)
	require.Len(t, items, 1)
	assert.Equal(t, types.StringValue("PUBLIC"), items[0]["grantee_type"])
	assert.Equal(t, types.StringValue("public"), items[0]["grantee"])
	assert.Equal(t, maskingAttachmentNameList([]string{"is_fraud", "card_number"}), items[0]["input_columns"])
	assert.Equal(t, maskingAttachmentNameList([]string{"card_number"}), items[0]["columns"])
	assert.Equal(t, types.Int64Value(100), items[0]["priority"])

	source := newMaskingPolicyAttachmentsDataSource()
	for _, failure := range []queryFunc{
		func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
			return nil, errors.New("catalog unavailable")
		},
		func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
			return []sqlclient.Row{{"priority": "0", "input_columns": "[", "output_columns": `["email"]`}}, nil
		},
		func(context.Context, sqlclient.Connection, string, map[string]string) ([]sqlclient.Row, error) {
			return []sqlclient.Row{{"priority": "high", "input_columns": `["email"]`, "output_columns": `["email"]`}}, nil
		},
	} {
		_, diagnostics := readSource(t, source, collectionConfig(t, source, nil), failure)
		assert.True(t, diagnostics.HasError())
	}
}
