package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// foldedNameValidator rejects a known name with ASCII uppercase letters. Redshift folds them to lowercase, also in
// quoted identifiers, so the object would be created under a spelling no catalog read of the configured name finds.
// Names in the colon-prefixed principal form, such as an identity-provider role "aad:Sales", keep their case.
type foldedNameValidator struct{}

// Description explains the validator in plain text.
func (foldedNameValidator) Description(context.Context) string {
	return "must not contain uppercase letters, which Redshift folds to lowercase"
}

// MarkdownDescription explains the validator in Markdown.
func (v foldedNameValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

// ValidateString reports an uppercase letter in a known value; unknown values wait for apply-time validation.
func (foldedNameValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() || strings.Contains(req.ConfigValue.ValueString(), ":") {
		return
	}
	if err := tableFolded(req.Path.String(), req.ConfigValue.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid name", err.Error())
	}
}

// foldNames adds the uppercase check to the named top-level string attributes of a resource schema. Lookups copy
// validators from the resource attributes, so they reject the same names.
func foldNames(s *schema.Schema, names ...string) {
	for _, name := range names {
		attribute := s.Attributes[name].(schema.StringAttribute)
		attribute.Validators = append(attribute.Validators, foldedNameValidator{})
		s.Attributes[name] = attribute
	}
}
