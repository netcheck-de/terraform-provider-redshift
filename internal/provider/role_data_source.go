package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// roleDataSource verifies an existing role without owning its lifecycle.
type roleDataSource struct {
	// dataSourceClient provides read-only catalog execution and binding checks.
	dataSourceClient
}

// roleData contains the SQL role lookup key.
type roleData struct {
	// ID is the paired resource's JSON identity.
	ID types.String `tfsdk:"id"`
	// Name identifies the existing role, including its optional namespace prefix.
	Name types.String `tfsdk:"name"`
}

// newRoleDataSource constructs a read-only SQL role lookup.
func newRoleDataSource() datasource.DataSource { return &roleDataSource{} }

// Metadata identifies the role data source to Terraform.
func (d *roleDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_role"
}

// Schema defines the existing role name lookup.
func (d *roleDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up an existing Redshift role.",
		Attributes: map[string]schema.Attribute{
			"id":   dataSourceIDAttribute(),
			"name": schema.StringAttribute{Required: true, MarkdownDescription: "Role name."},
		},
	}
}

// Read verifies role existence without managing memberships or privileges.
func (d *roleDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data roleData
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	role := roleModel{Name: data.Name}
	found, err := (&roleResource{d.resourceClient}).read(ctx, &role)
	if err != nil {
		resp.Diagnostics.AddError("Read Redshift role", err.Error())
		return
	}
	if !found {
		resp.Diagnostics.AddError("Role not found", "No role named "+data.Name.ValueString()+" exists.")
		return
	}
	data.Name = role.Name
	data.ID = d.identity(d.database.ValueString(), map[string]string{"name": data.Name.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
