package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// identityProviderDataSource exposes an existing AWSIDC SQL integration.
type identityProviderDataSource struct {
	// dataSourceClient provides read-only catalog execution and binding checks.
	dataSourceClient
}

// identityProviderData contains integration lookup input and observed AWSIDC settings.
type identityProviderData struct {
	// ID is the paired resource's JSON identity.
	ID types.String `tfsdk:"id"`
	// Name identifies the SQL identity provider.
	Name types.String `tfsdk:"name"`
	// Namespace reports the prefix used by federated SQL identities.
	Namespace types.String `tfsdk:"namespace"`
	// ApplicationARN reports the bound AWS managed application.
	ApplicationARN types.String `tfsdk:"application_arn"`
	// IAMRoleARN reports the attached integration IAM role.
	IAMRoleARN types.String `tfsdk:"iam_role_arn"`
	// Enabled reports SQL identity-provider availability.
	Enabled types.Bool `tfsdk:"enabled"`
}

var _ = registerDataSource(newIdentityProviderDataSource)

// newIdentityProviderDataSource constructs a read-only AWSIDC integration lookup.
func newIdentityProviderDataSource() datasource.DataSource { return &identityProviderDataSource{} }

// Metadata identifies the identity provider data source to Terraform.
func (d *identityProviderDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_identity_provider"
}

// Schema defines integration lookup identity and computed application/role settings.
func (d *identityProviderDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up an existing AWSIDC SQL identity provider.",
		Attributes: map[string]schema.Attribute{
			"id":              dataSourceIDAttribute(),
			"name":            schema.StringAttribute{Required: true, MarkdownDescription: "SQL identity provider name."},
			"namespace":       schema.StringAttribute{Computed: true, MarkdownDescription: "Federated user and group-role prefix."},
			"application_arn": schema.StringAttribute{Computed: true, MarkdownDescription: "Managed application ARN."},
			"iam_role_arn":    schema.StringAttribute{Computed: true, MarkdownDescription: "Integration IAM role ARN."},
			"enabled":         schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether the provider is enabled."},
		},
	}
}

// Read resolves an existing AWSIDC SQL integration without modifying it.
func (d *identityProviderDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data identityProviderData
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	provider := identityProviderModel{Name: data.Name}
	found, err := (&identityProviderResource{d.resourceClient}).read(ctx, &provider)
	if err != nil {
		resp.Diagnostics.AddError("Read identity provider", err.Error())
		return
	}
	if !found {
		resp.Diagnostics.AddError("Identity provider not found", "No SQL identity provider named "+data.Name.ValueString()+" exists.")
		return
	}
	data.Name, data.Namespace, data.ApplicationARN, data.IAMRoleARN, data.Enabled = provider.Name, provider.Namespace, provider.ApplicationARN, provider.IAMRoleARN, provider.Enabled
	data.ID = d.identity(d.database.ValueString(), map[string]string{"name": data.Name.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
