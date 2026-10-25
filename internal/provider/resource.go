package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// resourceClient supplies transport-neutral execution to SQL resources.
type resourceClient struct {
	// client executes statements within its configured warehouse.
	client sqlclient.Client
	// warehouse validates connection configuration and legacy state ownership before SQL execution.
	warehouse warehouseBinding
	// database is the default administration database; resources may select another local database.
	database types.String
	// datashareARN performs read-only AWS discovery when a lookup needs a complete producer ARN.
	datashareARN func(context.Context, shareSource) (string, error)
}

// Configure receives the shared SQL client and configured connection selectors.
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
		Computed: true, MarkdownDescription: "JSON import identity within the configured warehouse; independent of connection method and Data API execution history.",
		PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
	}
}

// connection selects a database only after the provider connection selectors are known.
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

// identity encodes the object's complete provider-relative tuple without adding connection context.
func identity(fields map[string]string) types.String {
	encoded, _ := json.Marshal(fields) // String maps are always JSON-serializable.

	return types.StringValue(string(encoded))
}

// bound preserves the ownership check for legacy IDs before refresh rewrites them without connection selectors.
// New IDs are provider-relative; their containing database, when present, must still match the object being read.
func (r *resourceClient) bound(id types.String, database string) error {
	if id.IsNull() || id.IsUnknown() {
		return nil
	}

	var fields map[string]string
	if err := json.Unmarshal([]byte(id.ValueString()), &fields); err != nil {
		return fmt.Errorf("invalid resource identity: %w", err)
	}

	if len(fields) == 0 {
		return fmt.Errorf("invalid resource identity: expected a nonempty JSON object")
	}

	bindings := 0

	for _, field := range warehouseIdentityFields {
		if _, present := fields[field]; present {
			bindings++

			if field != r.warehouse.field || fields[field] != r.warehouse.value.ValueString() {
				return fmt.Errorf("provider warehouse or database differs from the resource identity; migrate explicitly")
			}
		}
	}

	if bindings > 1 || (bindings == 1 && fields["database"] != database) ||
		(bindings == 0 && fields["database"] != "" && fields["database"] != database) {
		return fmt.Errorf("provider warehouse or database differs from the resource identity; migrate explicitly")
	}

	return nil
}

// warehouseIdentityFields recognizes the connection selectors carried by historical IDs.
var warehouseIdentityFields = []string{"workgroup_name", "cluster_identifier", "endpoint"}

// normalizeLegacyIdentity removes connection context from a validated ID, retaining its containing database only
// for database-scoped objects. New IDs are constructed independently by identity.
func normalizeLegacyIdentity(id types.String, databaseScoped bool) types.String {
	if id.IsNull() || id.IsUnknown() {
		return id
	}

	var fields map[string]string
	if err := json.Unmarshal([]byte(id.ValueString()), &fields); err != nil {
		return id
	}

	for _, field := range warehouseIdentityFields {
		delete(fields, field)
	}

	if !databaseScoped {
		delete(fields, "database")
	}

	return identity(fields)
}

// importIdentity accepts only provider-relative tuples; legacy connection context is supported only in stored state.
func importIdentity(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse, fields ...string) {
	var values map[string]string
	if err := json.Unmarshal([]byte(req.ID), &values); err != nil {
		resp.Diagnostics.AddError("Invalid import identity", "Expected a JSON object: "+err.Error())
		return
	}

	for _, field := range warehouseIdentityFields {
		if _, present := values[field]; present {
			resp.Diagnostics.AddError("Invalid import identity", "Remove connection selector "+field+" from the import identity.")
			return
		}
	}

	if _, present := values["database"]; present && !slices.Contains(fields, "database") {
		resp.Diagnostics.AddError("Invalid import identity", "The administration database is not part of this resource's import identity; remove database.")
		return
	}

	for _, field := range fields {
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

// privateState reads provider private state, which plan modifier, import, and update requests and responses share.
type privateState interface {
	GetKey(ctx context.Context, key string) ([]byte, diag.Diagnostics)
}
