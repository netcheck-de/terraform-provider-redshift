package provider

import (
	"fmt"
	"slices"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// privilegeFieldsKnown limits each check to its dependencies, not unrelated computed values or privilege elements.
func privilegeFieldsKnown(data types.Object, fields ...string) bool {
	for _, field := range fields {
		if value, ok := data.Attributes()[field]; ok && value.IsUnknown() {
			return false
		}
	}

	return !data.IsUnknown()
}

// privilegeSetKnown reports whether the set and every element are known, so validation can wait for the rest.
func privilegeSetKnown(value types.Set) bool {
	if value.IsUnknown() {
		return false
	}

	for _, element := range value.Elements() {
		if element.IsUnknown() {
			return false
		}
	}

	return true
}

// validateGrantOptions can reject known invalid elements without mistaking an unknown subset for an empty set.
func validateGrantOptions(privileges, options types.Set, recipientKnown, user bool) error {
	values := knownStrings(options)
	if len(values) != 0 && recipientKnown && !user {
		return fmt.Errorf("grant_option_privileges requires a user recipient; Redshift grants options only to users")
	}

	if privilegeSetKnown(privileges) {
		for _, option := range values {
			if !slices.Contains(knownStrings(privileges), option) {
				return fmt.Errorf("grant option privilege %q is not in privileges", option)
			}
		}
	}

	return nil
}

// validateConfig validates independently known parts before attempting the full SQL contract.
func (r *privilegeResource) validateConfig(data types.Object) error {
	privileges := data.Attributes()["privileges"].(types.Set)
	if r.grantOptions {
		options := data.Attributes()["grant_option_privileges"].(types.Set)

		recipientKnown := privilegeFieldsKnown(data, "grantee_type")
		if r.optionRecipient != nil {
			recipientKnown = privilegeFieldsKnown(data, r.fields...)
		}

		if err := validateGrantOptions(privileges, options, recipientKnown, r.optionGrantee(data)); err != nil {
			return err
		}
	}

	if _, ok := data.Attributes()["grantee_type"]; ok && r.recipient == nil && privilegeFieldsKnown(data, "grantee", "grantee_type") {
		if _, _, err := principal(data); err != nil {
			return err
		}
	}

	if r.partialAllowed != nil {
		allowed, err := r.partialAllowed(data)
		if err != nil {
			return err
		}

		for _, privilege := range knownStrings(privileges) {
			if len(allowed) > 0 && !privilegeAllowed(allowed, privilege) {
				return fmt.Errorf("unsupported privilege %q", privilege)
			}
		}
	}

	if r.destroyPrivileges != nil && privilegeFieldsKnown(data, "grantee_type", "object_type", "schema_name") {
		if _, err := r.destroyPrivileges(data); err != nil {
			return err
		}
	}

	if !privilegeFieldsKnown(data, r.fields...) {
		return nil
	}
	// Unknown privilege elements cannot invalidate an otherwise valid tuple.
	target, err := r.target(data)
	if err != nil {
		return err
	}

	for _, privilege := range knownStrings(privileges) {
		if !privilegeAllowed(target.allowed, privilege) {
			return fmt.Errorf("unsupported privilege %q", privilege)
		}
	}

	return nil
}

// fixedPrivilegeAllowlist is independent of names or selectors, so unknown identity fields cannot hide bad privileges.
func fixedPrivilegeAllowlist(allowed []sqlclient.Keyword) func(types.Object) ([]sqlclient.Keyword, error) {
	return func(types.Object) ([]sqlclient.Keyword, error) { return allowed, nil }
}
