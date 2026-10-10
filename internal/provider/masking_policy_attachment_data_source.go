package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newMaskingPolicyAttachmentDataSource)

// maskingAttachmentDataSource is the shared catalog lookup with the attachment's full identity. catalogSpec
// builds identities from string attributes only, while an attachment's identity also holds its column list.
type maskingAttachmentDataSource struct {
	*catalogDataSource
}

// newMaskingPolicyAttachmentDataSource checks whether a masking policy is attached and reads its inputs and priority.
func newMaskingPolicyAttachmentDataSource() datasource.DataSource {
	return &maskingAttachmentDataSource{newCatalogDataSource(catalogSpec{
		name: "masking_policy_attachment", factory: newMaskingPolicyAttachmentResource, exists: true,
		identityFields: []string{"policy", "schema", "relation", "grantee", "grantee_type"}, identityDatabase: "database",
		lookup: func(ctx context.Context, client *resourceClient, data *types.Object) (bool, error) {
			model := maskingAttachmentModelFromObject(*data)
			found, err := (&maskingPolicyAttachmentResource{*client}).read(ctx, &model)
			if err == nil && found {
				lookupValue(data, "input_columns", model.InputColumns)
				lookupValue(data, "priority", model.Priority)
			}
			return found, err
		},
	}).(*catalogDataSource)}
}

// Read runs the shared lookup and then replaces its identity with the paired resource's, which includes columns.
func (d *maskingAttachmentDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	d.catalogDataSource.Read(ctx, req, resp)
	if resp.Diagnostics.HasError() {
		return
	}
	var data types.Object
	resp.Diagnostics.Append(resp.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !data.Attributes()["exists"].Equal(types.BoolValue(true)) {
		return
	}
	model := maskingAttachmentModelFromObject(data)
	id := d.identity(model.Database.ValueString(), maskingAttachmentIdentity(model))
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
}
