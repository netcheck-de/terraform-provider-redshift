package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
)

// dataSourceIDAttribute exposes the paired resource's JSON identity as a read-only observation.
func dataSourceIDAttribute() schema.StringAttribute {
	return schema.StringAttribute{Computed: true, MarkdownDescription: "JSON identity of the observed object, using the same format as the paired resource. Null for a missing relationship."}
}

// dataSourceClient supplies the same routing and ownership contract to read-only lookups.
type dataSourceClient struct {
	// resourceClient shares SQL execution helpers without granting lifecycle ownership.
	resourceClient
}

// Configure receives the shared SQL client and catalog ownership binding for lookups.
func (d *dataSourceClient) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	data, ok := req.ProviderData.(providerData)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider client", fmt.Sprintf("Expected provider connection data, got %T.", req.ProviderData))
		return
	}
	d.client, d.warehouse, d.database = data.client, data.warehouse, data.database
	d.datashareARN = data.datashareARN
}
