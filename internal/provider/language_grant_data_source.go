package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newLanguageGrantDataSource)

// languageGrantOptionLookupDescription describes the observed grant options; the paired resource's text explains how
// to change them, which does not apply to a read-only lookup.
const languageGrantOptionLookupDescription = "Subset of `privileges` the grantee currently holds `WITH GRANT OPTION`, " +
	"from `admin_option`. Only users can hold grant options, so it is empty for other grantees."

// newLanguageGrantDataSource reads one grantee's explicit privileges on one language.
func newLanguageGrantDataSource() datasource.DataSource {
	d := newPrivilegeDataSource(newLanguageGrantResource).(*catalogDataSource)
	d.attributes["grant_option_privileges"] = schema.SetAttribute{Computed: true, ElementType: types.StringType, MarkdownDescription: languageGrantOptionLookupDescription}
	return d
}
