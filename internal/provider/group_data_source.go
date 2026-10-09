package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// groupDataSource verifies an existing SQL group without managing its membership.
type groupDataSource struct {
	// dataSourceClient provides read-only catalog execution and binding checks.
	dataSourceClient
}

// groupData contains the SQL group lookup key.
type groupData struct {
	// ID is the paired resource's JSON identity.
	ID types.String `tfsdk:"id"`
	// Name identifies the existing SQL user group.
	Name types.String `tfsdk:"name"`
}

// newGroupDataSource constructs a read-only SQL user group lookup.
func newGroupDataSource() datasource.DataSource { return &groupDataSource{} }

// Metadata identifies the group data source to Terraform.
func (d *groupDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_group"
}

// Schema defines the existing group name lookup.
func (d *groupDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Looks up an existing SQL user group.", Attributes: map[string]schema.Attribute{
		"id":   dataSourceIDAttribute(),
		"name": schema.StringAttribute{Required: true, MarkdownDescription: "SQL group name; a missing group raises an error."},
	}}
}

// Read verifies group existence without managing its membership.
func (d *groupDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data groupData
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := (&groupResource{d.resourceClient}).read(ctx, &groupModel{Name: data.Name})
	if err != nil {
		resp.Diagnostics.AddError("Read Redshift group", err.Error())
		return
	}
	if !found {
		resp.Diagnostics.AddError("Group not found", "No group named "+data.Name.ValueString()+" exists.")
		return
	}
	data.ID = d.identity(d.database.ValueString(), map[string]string{"name": data.Name.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
