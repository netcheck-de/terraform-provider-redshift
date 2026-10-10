package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// identityProviderDataSource exposes an existing SQL identity provider of any supported type.
type identityProviderDataSource struct {
	// dataSourceClient provides read-only catalog execution and binding checks.
	dataSourceClient
}

// identityProviderData contains the lookup name and the observed provider settings; it mirrors
// identityProviderModel without the write-only secret and its version.
type identityProviderData struct {
	// ID is the paired resource's JSON identity.
	ID types.String `tfsdk:"id"`
	// Name identifies the SQL identity provider.
	Name types.String `tfsdk:"name"`
	// Type reports awsidc or azure.
	Type types.String `tfsdk:"type"`
	// Namespace reports the prefix used by federated SQL identities.
	Namespace types.String `tfsdk:"namespace"`
	// ApplicationARN reports the bound AWS managed application.
	ApplicationARN types.String `tfsdk:"application_arn"`
	// IAMRoleARN reports the attached integration IAM role.
	IAMRoleARN types.String `tfsdk:"iam_role_arn"`
	// Issuer reports the Azure token issuer.
	Issuer types.String `tfsdk:"issuer"`
	// ClientID reports the Azure application ID.
	ClientID types.String `tfsdk:"client_id"`
	// Audience reports the Azure token audiences.
	Audience []string `tfsdk:"audience"`
	// AutoCreateRoles is not in the catalog and stays null.
	AutoCreateRoles types.Bool `tfsdk:"auto_create_roles"`
	// IncludeGroups is not in the catalog and stays null.
	IncludeGroups types.String `tfsdk:"auto_create_roles_include_groups"`
	// ExcludeGroups is not in the catalog and stays null.
	ExcludeGroups types.String `tfsdk:"auto_create_roles_exclude_groups"`
	// Enabled reports SQL identity-provider availability.
	Enabled types.Bool `tfsdk:"enabled"`
	// ProviderID reports the catalog uid.
	ProviderID types.Int64 `tfsdk:"provider_id"`
	// InstanceID reports the catalog instance identifier.
	InstanceID types.String `tfsdk:"instance_id"`
	// IdentityCenterInstanceARN reports the Identity Center instance of an AWSIDC provider.
	IdentityCenterInstanceARN types.String `tfsdk:"identity_center_instance_arn"`
}

var _ = registerDataSource(newIdentityProviderDataSource)

// newIdentityProviderDataSource constructs a read-only identity provider lookup.
func newIdentityProviderDataSource() datasource.DataSource { return &identityProviderDataSource{} }

// Metadata identifies the identity provider data source to Terraform.
func (d *identityProviderDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_identity_provider"
}

// Schema defines the lookup name and the observed catalog settings of either provider type.
func (d *identityProviderDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	observed := func(description string) schema.StringAttribute {
		return schema.StringAttribute{Computed: true, MarkdownDescription: description}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up an existing SQL identity provider (`awsidc` or `azure`) from `svv_identity_providers`, which only superusers can read.",
		Attributes: map[string]schema.Attribute{
			"id":                               dataSourceIDAttribute(),
			"name":                             schema.StringAttribute{Required: true, MarkdownDescription: "SQL identity provider name; a missing provider raises an error."},
			"type":                             observed("Identity provider type: `awsidc` or `azure`."),
			"namespace":                        observed("Prefix of federated users and group roles."),
			"application_arn":                  observed("Identity Center managed application ARN; null for `azure`."),
			"iam_role_arn":                     observed("Identity Center integration IAM role ARN; null for `azure`."),
			"issuer":                           observed("Microsoft Entra ID token issuer URL; null for `awsidc`."),
			"client_id":                        observed("Microsoft Entra ID application (client) ID; null for `awsidc`."),
			"audience":                         schema.SetAttribute{Computed: true, ElementType: types.StringType, MarkdownDescription: "Accepted Microsoft Entra ID token audiences; null for `awsidc` or when none are set."},
			"auto_create_roles":                schema.BoolAttribute{Computed: true, MarkdownDescription: "Always null: the catalog does not report automatic role creation."},
			"auto_create_roles_include_groups": observed("Always null: the catalog does not report the group filter."),
			"auto_create_roles_exclude_groups": observed("Always null: the catalog does not report the group filter."),
			"enabled":                          schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether the provider is enabled."},
			"provider_id":                      schema.Int64Attribute{Computed: true, MarkdownDescription: "Catalog ID of the identity provider (`svv_identity_providers.uid`)."},
			"instance_id":                      observed("Catalog instance identifier (`svv_identity_providers.instanceid`): the application ARN for `awsidc` and the tenant ID for `azure`."),
			"identity_center_instance_arn":     observed("IAM Identity Center instance ARN of an `awsidc` provider; null for `azure`."),
		},
	}
}

// Read resolves an existing SQL identity provider without modifying it.
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
	data = identityProviderData{
		Name: provider.Name, Type: provider.Type, Namespace: provider.Namespace, ApplicationARN: provider.ApplicationARN,
		IAMRoleARN: provider.IAMRoleARN, Issuer: provider.Issuer, ClientID: provider.ClientID, Audience: provider.Audience,
		AutoCreateRoles: types.BoolNull(), IncludeGroups: types.StringNull(), ExcludeGroups: types.StringNull(),
		Enabled: provider.Enabled, ProviderID: provider.ProviderID, InstanceID: provider.InstanceID,
		IdentityCenterInstanceARN: provider.IdentityCenterInstanceARN,
	}
	data.ID = d.identity(d.database.ValueString(), map[string]string{"name": data.Name.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
