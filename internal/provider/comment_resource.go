package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// commentResource owns annotation text independently of its target object's definition.
type commentResource struct {
	// resourceClient provides SQL execution and warehouse ownership checks.
	resourceClient
}

// commentModel is the Terraform state for an annotation on one local SQL object.
type commentModel struct {
	// ID records the administration binding and annotated object identity.
	ID types.String `tfsdk:"id"`
	// DatabaseName is the local database where COMMENT executes.
	DatabaseName types.String `tfsdk:"database_name"`
	// ObjectType selects DATABASE, SCHEMA, TABLE, VIEW, or COLUMN.
	ObjectType types.String `tfsdk:"object_type"`
	// ObjectName identifies the database, schema, or parent relation.
	ObjectName types.String `tfsdk:"object_name"`
	// SchemaName qualifies table, view, and column targets.
	SchemaName types.String `tfsdk:"schema_name"`
	// ColumnName selects a column annotation within ObjectName.
	ColumnName types.String `tfsdk:"column_name"`
	// Text is the annotation; empty text clears it with IS NULL.
	Text types.String `tfsdk:"text"`
}

// newCommentResource constructs an annotation handler that never owns its target object.
func newCommentResource() resource.Resource { return &commentResource{} }

// Metadata identifies the comment resource to Terraform.
func (r *commentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_comment"
}

// Schema defines the target object's immutable identity and mutable annotation text.
func (r *commentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Manages an object's comment independently of its definition. Destroy clears only the annotation.", Attributes: map[string]schema.Attribute{
		"id":            idAttribute(),
		"database_name": privilegeString("Local database containing the target object.", false),
		"object_type":   privilegeString("DATABASE, SCHEMA, TABLE, VIEW, or COLUMN.", false, "DATABASE", "SCHEMA", "TABLE", "VIEW", "COLUMN"),
		"object_name":   privilegeString("Database/schema/relation name according to object_type.", false),
		"schema_name":   privilegeString("Required for TABLE, VIEW, COLUMN; omit for DATABASE and SCHEMA.", true),
		"column_name":   privilegeString("Required only for COLUMN.", true),
		"text":          schema.StringAttribute{Required: true, MarkdownDescription: "Comment text. An empty string represents no annotation."},
	}}
}

// target builds quoted COMMENT syntax and an object-specific catalog query.
func (data commentModel) target() (string, catalogCheck, error) {
	kind, name, schemaName, column := data.ObjectType.ValueString(), data.ObjectName.ValueString(), data.SchemaName.ValueString(), data.ColumnName.ValueString()
	parameters := map[string]string{"name": name}
	object := sqlclient.Identifier(name)
	var query string
	switch kind {
	case "DATABASE", "SCHEMA":
		if schemaName != "" || column != "" {
			return "", catalogCheck{}, fmt.Errorf("%s does not accept schema_name or column_name", kind)
		}
		catalog, key := "pg_namespace", "nspname"
		if kind == "DATABASE" {
			if name != data.DatabaseName.ValueString() {
				return "", catalogCheck{}, fmt.Errorf("DATABASE object_name must equal database_name")
			}
			catalog, key = "pg_database", "datname"
		}
		query = "SELECT COALESCE(d.description, '') AS text FROM " + catalog + " o LEFT JOIN pg_description d ON d.objoid = o.oid AND d.classoid = '" + catalog + "'::regclass AND d.objsubid = 0 WHERE o." + key + " = :name"
	case "TABLE", "VIEW", "COLUMN":
		if schemaName == "" {
			return "", catalogCheck{}, fmt.Errorf("schema_name is required for %s", kind)
		}
		if (kind == "COLUMN") != (column != "") {
			return "", catalogCheck{}, fmt.Errorf("column_name is required only for COLUMN")
		}
		object = sqlclient.Identifier(schemaName) + "." + object
		parameters["schema"] = schemaName
		query = "SELECT COALESCE(d.description, '') AS text FROM pg_class o JOIN pg_namespace n ON n.oid = o.relnamespace "
		if kind == "COLUMN" {
			object += "." + sqlclient.Identifier(column)
			parameters["column"] = column
			query += "JOIN pg_attribute a ON a.attrelid = o.oid AND a.attname = :column AND a.attnum > 0 AND NOT a.attisdropped LEFT JOIN pg_description d ON d.objoid = o.oid AND d.classoid = 'pg_class'::regclass AND d.objsubid = a.attnum "
		} else {
			query += "LEFT JOIN pg_description d ON d.objoid = o.oid AND d.classoid = 'pg_class'::regclass AND d.objsubid = 0 "
		}
		query += "WHERE o.relname = :name AND n.nspname = :schema"
		switch kind {
		case "VIEW":
			query += " AND o.relkind = 'v'"
		case "TABLE":
			query += " AND o.relkind IN ('r', 'm')"
		}
	default:
		return "", catalogCheck{}, fmt.Errorf("unsupported comment object_type %q", kind)
	}
	return "COMMENT ON " + kind + " " + object, catalogCheck{query, parameters}, nil
}

// read checks local target existence and retrieves its annotation from the target database.
func (r *commentResource) read(ctx context.Context, data *commentModel) (bool, error) {
	if err := r.bound(data.ID, r.database.ValueString()); err != nil {
		return false, err
	}
	_, query, err := data.target()
	if err != nil {
		return false, err
	}
	rows, err := r.query(ctx, "SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local'", map[string]string{"database": data.DatabaseName.ValueString()})
	if err != nil || len(rows) == 0 {
		return false, err
	}
	rows, err = r.queryDatabase(ctx, data.DatabaseName.ValueString(), query.sql, query.parameters)
	if err != nil || len(rows) == 0 {
		return false, err
	}
	if len(rows) != 1 {
		return false, fmt.Errorf("comment target catalog identity is ambiguous")
	}
	data.Text = types.StringValue(rows[0]["text"])
	return true, nil
}

// reconcile changes only annotation text and verifies its catalog value, including removal.
func (r *commentResource) reconcile(ctx context.Context, data commentModel, deleting bool) error {
	actual := data
	found, err := r.read(ctx, &actual)
	if err != nil {
		return err
	}
	if !found {
		if deleting {
			return nil
		}
		return fmt.Errorf("comment target does not exist")
	}
	desired, sqlValue := data.Text.ValueString(), sqlclient.Literal(data.Text.ValueString())
	if deleting || desired == "" {
		desired, sqlValue = "", "NULL"
	}
	if actual.Text.ValueString() == desired {
		return nil
	}
	statement, _, _ := data.target() // read already validated this unchanged target.
	if _, err := r.queryDatabase(ctx, data.DatabaseName.ValueString(), statement+" IS "+sqlValue, nil); err != nil {
		return err
	}
	found, err = r.read(ctx, &actual)
	if err == nil && (!found || actual.Text.ValueString() != desired) {
		return fmt.Errorf("comment did not converge")
	}
	return err
}

// Create records annotation ownership and sets the requested text without creating its target.
func (r *commentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data commentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Invalid targets must fail before ownership is recorded, or Read and Delete could never succeed.
	if _, _, err := data.target(); err != nil {
		resp.Diagnostics.AddError("Create comment", err.Error())
		return
	}
	fields := map[string]string{"database_name": data.DatabaseName.ValueString(), "object_type": data.ObjectType.ValueString(), "object_name": data.ObjectName.ValueString()}
	if !data.SchemaName.IsNull() {
		fields["schema_name"] = data.SchemaName.ValueString()
	}
	if !data.ColumnName.IsNull() {
		fields["column_name"] = data.ColumnName.ValueString()
	}
	data.ID = r.identity(r.database.ValueString(), fields)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	if err := r.reconcile(ctx, data, false); err != nil {
		resp.Diagnostics.AddError("Create comment", err.Error())
	}
}

// ValidateConfig reports invalid comment targets during planning once the configuration is known.
func (r *commentResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data commentModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !req.Config.Raw.IsFullyKnown() {
		return
	}
	if _, _, err := data.target(); err != nil {
		resp.Diagnostics.AddError("Invalid comment target", err.Error())
	}
}

// Read refreshes comment text or removes state when the target object is absent.
func (r *commentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data commentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, &data)
	if err != nil {
		resp.Diagnostics.AddError("Read comment", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update reconciles annotation text under the existing object binding.
func (r *commentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data commentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.reconcile(ctx, data, false); err != nil {
		resp.Diagnostics.AddError("Update comment", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Delete clears the annotation with IS NULL while preserving the database object.
func (r *commentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data commentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.reconcile(ctx, data, true); err != nil {
		resp.Diagnostics.AddError("Delete comment", err.Error())
	}
}

// ImportState restores comment target identity; refresh retrieves the current text.
func (r *commentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importIdentity(ctx, req, resp, "database_name", "object_type", "object_name")
	if resp.Diagnostics.HasError() {
		return
	}
	var values map[string]string
	_ = json.Unmarshal([]byte(req.ID), &values)
	for _, field := range []string{"schema_name", "column_name"} {
		if values[field] != "" {
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(field), values[field])...)
		}
	}
}
