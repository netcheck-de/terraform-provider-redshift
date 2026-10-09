package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// resourceClient supplies transport-neutral execution and stable ownership to SQL resources.
type resourceClient struct {
	// client executes statements within its configured warehouse.
	client sqlclient.Client
	// warehouse validates state ownership before SQL execution.
	warehouse warehouseBinding
	// database is the default administration database; resources may select another local database.
	database types.String
	// datashareARN performs read-only AWS discovery when a lookup needs a complete producer ARN.
	datashareARN func(context.Context, shareSource) (string, error)
}

// Configure receives the shared SQL client and transport-independent ownership binding.
func (r *resourceClient) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	data, ok := req.ProviderData.(providerData)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider client", fmt.Sprintf("Expected provider connection data, got %T.", req.ProviderData))
		return
	}
	r.client, r.warehouse, r.database = data.client, data.warehouse, data.database
	r.datashareARN = data.datashareARN
}

// idAttribute defines a computed import identity retained across unknown plans.
func idAttribute() schema.StringAttribute {
	return schema.StringAttribute{
		Computed: true, MarkdownDescription: "JSON import identity; independent of Data API execution history.",
		PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
	}
}

// connection selects a database only after the provider ownership binding is known.
func (r *resourceClient) connection(database string) (sqlclient.Connection, error) {
	if r.warehouse.value.IsUnknown() || r.warehouse.value.IsNull() || r.warehouse.value.ValueString() == "" || database == "" {
		return sqlclient.Connection{}, fmt.Errorf("provider %s and database must be known before executing SQL", r.warehouse.field)
	}
	return sqlclient.Connection{Database: database}, nil
}

// query executes SQL in the configured local administration database.
func (r *resourceClient) query(ctx context.Context, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
	return r.queryDatabase(ctx, r.database.ValueString(), sql, parameters)
}

// queryDatabase executes SQL against an explicitly selected database in this warehouse.
func (r *resourceClient) queryDatabase(ctx context.Context, database, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
	target, err := r.connection(database)
	if err != nil {
		return nil, err
	}
	return r.client.Query(ctx, target, sql, parameters)
}

// exec runs DDL statements in order without parameters. It stops at the first failure because later
// statements assume the earlier ones applied.
func (r *resourceClient) exec(ctx context.Context, database string, statements ...string) error {
	for _, statement := range statements {
		if _, err := r.queryDatabase(ctx, database, statement, nil); err != nil {
			return err
		}
	}
	return nil
}

// selectRows runs a catalog query in database after Build has checked its placeholders against its parameters.
func (r *resourceClient) selectRows(ctx context.Context, database string, query sqlclient.Query) ([]sqlclient.Row, error) {
	sql, parameters, err := query.Build()
	if err != nil {
		return nil, err
	}
	return r.queryDatabase(ctx, database, sql, parameters)
}

// localDatabaseExists distinguishes a dropped target database from query failures, so refresh can drop orphaned state.
func (r *resourceClient) localDatabaseExists(ctx context.Context, database string) (bool, error) {
	if database == r.database.ValueString() {
		return true, nil
	}
	rows, err := r.selectRows(ctx, r.database.ValueString(), localDatabaseQuery(database))
	return len(rows) > 0, err
}

// identity adds the warehouse/database binding to resource-specific JSON import fields.
func (r *resourceClient) identity(database string, fields map[string]string) types.String {
	fields[r.warehouse.field] = r.warehouse.value.ValueString()
	fields["database"] = database
	encoded, _ := json.Marshal(fields) // String maps are always JSON-serializable.
	return types.StringValue(string(encoded))
}

// bound rejects adoption of state belonging to another warehouse or database.
func (r *resourceClient) bound(id types.String, database string) error {
	if id.IsNull() || id.IsUnknown() {
		return nil
	}
	var fields map[string]string
	if err := json.Unmarshal([]byte(id.ValueString()), &fields); err != nil {
		return fmt.Errorf("invalid resource identity: %w", err)
	}
	if fields[r.warehouse.field] != r.warehouse.value.ValueString() || fields["database"] != database {
		return fmt.Errorf("provider warehouse or database differs from the resource identity; migrate explicitly")
	}
	return nil
}

// importIdentity validates one Serverless/cluster/endpoint binding and restores resource attributes.
func importIdentity(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse, fields ...string) {
	var values map[string]string
	if err := json.Unmarshal([]byte(req.ID), &values); err != nil {
		resp.Diagnostics.AddError("Invalid import identity", "Expected a JSON object: "+err.Error())
		return
	}
	bindings := 0
	for _, field := range []string{"workgroup_name", "cluster_identifier", "endpoint"} {
		if values[field] != "" {
			bindings++
		}
	}
	if bindings != 1 {
		resp.Diagnostics.AddError("Invalid warehouse identity", "Import requires exactly one nonempty workgroup_name, cluster_identifier, or endpoint.")
		return
	}
	for _, field := range append([]string{"database"}, fields...) {
		if values[field] == "" {
			resp.Diagnostics.AddError("Invalid import identity", "Missing nonempty field "+field+".")
			return
		}
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	for _, field := range fields {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(field), values[field])...)
	}
}
