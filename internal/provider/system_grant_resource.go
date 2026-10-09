package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// newSystemGrantResource defines role system capabilities and their catalog reconciliation.
func newSystemGrantResource() resource.Resource {
	attributes := privilegeAttributes()
	attributes["role"] = privilegeString("Receiving SQL role.", false)
	return &privilegeResource{name: "system_grant", attributes: attributes, fields: []string{"role"}, prepare: systemGrantTarget}
}
