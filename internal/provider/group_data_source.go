package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// groupDataSource reads an existing SQL group and its members without managing them.
type groupDataSource struct {
	// dataSourceClient provides read-only catalog execution and binding checks.
	dataSourceClient
}

// groupData contains the SQL group lookup key and its observed ID and members. Members is a plain Go slice, so a
// zero groupData is a valid null configuration.
type groupData struct {
	// ID is the paired resource's JSON identity.
	ID types.String `tfsdk:"id"`
	// Name identifies the existing SQL user group.
	Name types.String `tfsdk:"name"`
	// GroupID is the catalog group ID.
	GroupID types.Int64 `tfsdk:"group_id"`
	// Members are the names of the users in the group.
	Members []string `tfsdk:"members"`
}

var _ = registerDataSource(newGroupDataSource)

// newGroupDataSource constructs a read-only SQL user group lookup.
func newGroupDataSource() datasource.DataSource { return &groupDataSource{} }

// Metadata identifies the group data source to Terraform.
func (d *groupDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_group"
}

// Schema defines the existing group name lookup and its observed ID and members.
func (d *groupDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Looks up an existing SQL user group and its members.", Attributes: map[string]schema.Attribute{
		"id":       dataSourceIDAttribute(),
		"name":     schema.StringAttribute{Required: true, MarkdownDescription: "SQL group name; a missing group raises an error."},
		"group_id": schema.Int64Attribute{Computed: true, MarkdownDescription: "Group ID from `pg_group.grosysid`."},
		"members":  schema.SetAttribute{Computed: true, ElementType: types.StringType, MarkdownDescription: "Names of the users in the group; empty when it has none."},
	}}
}

// Read resolves the group's ID and members without changing them.
func (d *groupDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data groupData
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	group := groupModel{Name: data.Name, ID: types.StringNull()}
	found, err := (&groupResource{d.resourceClient}).read(ctx, &group)
	if err != nil {
		resp.Diagnostics.AddError("Read Redshift group", err.Error())
		return
	}
	if !found {
		resp.Diagnostics.AddError("Group not found", "No group named "+data.Name.ValueString()+" exists.")
		return
	}
	data.GroupID = group.GroupID
	data.Members = knownStrings(group.Members)
	if data.Members == nil {
		data.Members = []string{}
	}
	data.ID = d.identity(d.database.ValueString(), map[string]string{"name": data.Name.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
