package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// userDataSource exposes non-secret attributes of an existing database user.
type userDataSource struct {
	// dataSourceClient provides read-only catalog execution and binding checks.
	dataSourceClient
}

// userData contains user lookup input and observed administrative capabilities.
type userData struct {
	// ID is the paired resource's JSON identity.
	ID types.String `tfsdk:"id"`
	// Name identifies the requested database user.
	Name types.String `tfsdk:"name"`
	// Superuser reports the SQL CREATEUSER capability.
	Superuser types.Bool `tfsdk:"superuser"`
	// CreateDB reports permission to create databases.
	CreateDB types.Bool `tfsdk:"create_database"`
}

var _ = registerDataSource(newUserDataSource)

// newUserDataSource constructs a read-only SQL user lookup.
func newUserDataSource() datasource.DataSource { return &userDataSource{} }

// Metadata identifies the user data source to Terraform.
func (d *userDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user"
}

// Schema defines user lookup and non-secret capability attributes.
func (d *userDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a database user without exposing its password.",
		Attributes: map[string]schema.Attribute{
			"id":              dataSourceIDAttribute(),
			"name":            schema.StringAttribute{Required: true, MarkdownDescription: "Database user name; a missing user raises an error."},
			"superuser":       schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether the user has CREATEUSER."},
			"create_database": schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether the user has CREATEDB."},
		},
	}
}

// Read resolves user capabilities without reading or changing credentials.
func (d *userDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data userData
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	user := userModel{Name: data.Name}
	found, err := (&userResource{d.resourceClient}).read(ctx, &user)
	if err != nil {
		resp.Diagnostics.AddError("Read Redshift user", err.Error())
		return
	}
	if !found {
		resp.Diagnostics.AddError("User not found", "No database user named "+data.Name.ValueString()+" exists.")
		return
	}
	data.Name, data.Superuser, data.CreateDB = user.Name, user.Superuser, user.CreateDB
	data.ID = d.identity(d.database.ValueString(), map[string]string{"name": data.Name.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
