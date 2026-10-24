package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// configurationDataSource reads one configuration parameter as the provider's session sees it.
type configurationDataSource struct {
	// dataSourceClient provides transport-neutral catalog execution.
	dataSourceClient
}

// configurationModel is the configuration and result of one parameter read.
type configurationModel struct {
	// ID records the warehouse binding, the provider database, and the parameter name.
	ID types.String `tfsdk:"id"`
	// Name is the lowercase parameter or dotted session context name.
	Name types.String `tfsdk:"name"`
	// Value is the textual value SHOW reports.
	Value types.String `tfsdk:"value"`
}

var _ = registerDataSource(newConfigurationDataSource)

// newConfigurationDataSource constructs the configuration parameter lookup.
func newConfigurationDataSource() datasource.DataSource { return &configurationDataSource{} }

// Metadata identifies the configuration lookup to Terraform.
func (d *configurationDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_configuration"
}

// Schema exposes the parameter name and its observed value.
func (d *configurationDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads the current value of one configuration parameter with `SHOW`, as the provider's own session sees it. Nothing is set or reset.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, MarkdownDescription: "JSON identity of this read: the warehouse binding, the provider database, and `name`."},
			"name": schema.StringAttribute{
				Required: true, MarkdownDescription: "Lowercase parameter name, such as `search_path`, `datestyle`, or `enable_case_sensitive_identifier`, or a dotted session context name such as `app_context.user_id`. `all` is rejected. A context variable must exist in the provider's session; a value set in another session is not visible.",
				Validators: []validator.String{stringvalidator.RegexMatches(configurationParameterName, "must be a lowercase parameter or dotted session context name such as app_context.user_id"), stringvalidator.NoneOf("all")},
			},
			"value": schema.StringAttribute{Computed: true, MarkdownDescription: "Value `SHOW` reports for the provider's session, as text."},
		},
	}
}

// Read runs SHOW in the provider's administration database.
func (d *configurationDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data configurationModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	name := data.Name.ValueString()

	statement, err := readConfigurationStatement(name)
	if err != nil {
		resp.Diagnostics.AddError("Invalid configuration parameter", err.Error())
		return
	}

	rows, err := d.query(ctx, statement, nil)
	if err != nil {
		resp.Diagnostics.AddError("Read configuration", err.Error())
		return
	}

	value, err := configurationValue(name, rows)
	if err != nil {
		resp.Diagnostics.AddError("Read configuration", err.Error())
		return
	}

	data.Value = types.StringValue(value)
	data.ID = d.identity(d.database.ValueString(), map[string]string{"name": name})
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
