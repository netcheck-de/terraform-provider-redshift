package provider

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// routineMaxArguments is the documented limit for SQL UDF arguments and for each of the input and output
// arguments of a stored procedure.
// https://docs.aws.amazon.com/redshift/latest/dg/r_CREATE_FUNCTION.html
// https://docs.aws.amazon.com/redshift/latest/dg/r_CREATE_PROCEDURE.html
const routineMaxArguments = 32

// routineIdentityArguments is the JSON identity key holding the canonical input argument types, which tell
// overloads of one name apart.
const routineIdentityArguments = "arguments"

// routineType validates a configured argument or return type and returns its canonical spelling, keeping
// modifiers such as a VARCHAR length for CREATE.
func routineType(value string) (sqlclient.Keyword, error) {
	return sqlclient.TypeName(value)
}

// routineBaseType drops the modifiers of a canonical type. Redshift identifies an overload by type OIDs only, and
// oidvectortypes and format_type without a modifier report the bare name.
func routineBaseType(name sqlclient.Keyword) sqlclient.Keyword {
	if open := strings.IndexByte(string(name), '('); open >= 0 {
		return name[:open]
	}
	return name
}

// routineSignature canonicalizes input argument types into the comma-separated bare form that identifies an
// overload in ALTER, DROP and the catalog, for example int and varchar(10) → integer, character varying.
func routineSignature(argumentTypes []string) (sqlclient.Keyword, error) {
	bases := make([]string, len(argumentTypes))
	for i, argumentType := range argumentTypes {
		name, err := routineType(argumentType)
		if err != nil {
			return "", fmt.Errorf("argument %d: %w", i+1, err)
		}
		bases[i] = string(routineBaseType(name))
	}
	return sqlclient.Signature(bases...)
}

// routineSignatureTypes splits a canonical signature into its types; an empty signature has none.
func routineSignatureTypes(signature string) []string {
	if strings.TrimSpace(signature) == "" {
		return nil
	}
	parts := strings.Split(signature, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// routineTypesEquivalent reports whether two configured spellings name the same type including modifiers, such as
// int and integer. Invalid spellings compare literally, so ValidateConfig reports them instead of a plan diff.
func routineTypesEquivalent(a, b string) bool {
	left, leftErr := routineType(a)
	right, rightErr := routineType(b)
	if leftErr != nil || rightErr != nil {
		return a == b
	}
	return left == right
}

// routineTypeReplaces reports whether changing a type from its state to its planned spelling needs a new routine.
// A state type without modifiers, as an import or a refresh reports it from the type OIDs in the overload identity,
// format_type(prorettype, NULL) and SHOW PARAMETERS, says nothing about the declared modifiers, so it matches the
// same base type with modifiers and the redefinition restates them. Two different modifiers still replace.
func routineTypeReplaces(before, after string) bool {
	if routineTypesEquivalent(before, after) {
		return false
	}
	stored, err := routineType(before)
	if err != nil || routineBaseType(stored) != stored {
		return true
	}
	return !routineCatalogTypeMatches(after, before)
}

// routineDeclaredTypes renders the canonical declared types, modifiers included, as one comparable value: another
// spelling of the same types compares equal, while an added modifier is a change that CREATE OR REPLACE restates.
func routineDeclaredTypes(unknown bool, values ...string) attr.Value {
	if unknown {
		return types.StringUnknown()
	}
	canonical := make([]string, len(values))
	for i, value := range values {
		canonical[i] = routineCatalogType(value)
	}
	return types.StringValue(strings.Join(canonical, ", "))
}

// routineCatalogTypeMatches reports whether a configured type has the bare catalog type, which is all the catalog
// keeps of a routine argument or return type.
func routineCatalogTypeMatches(configured, catalog string) bool {
	left, leftErr := routineType(configured)
	right, rightErr := routineType(catalog)
	if leftErr != nil || rightErr != nil {
		return strings.EqualFold(configured, catalog)
	}
	return routineBaseType(left) == routineBaseType(right)
}

// routineCatalogType returns the canonical spelling of a catalog type, or the catalog text when TypeName does not
// know it, so an unexpected type still surfaces as drift rather than an error.
func routineCatalogType(catalog string) string {
	if name, err := routineType(catalog); err == nil {
		return string(name)
	}
	return catalog
}

// routineRejectedType explains the types the routine kind cannot use; TypeName accepts them for other contexts.
func routineRejectedType(name sqlclient.Keyword, kind string) error {
	switch {
	case name == "anyelement":
		return fmt.Errorf("ANYELEMENT is supported only by Python UDFs, which a %s cannot use", kind)
	case name == "refcursor" && kind == "function":
		return fmt.Errorf("refcursor is supported only by stored procedures")
	}
	return nil
}

// routineStrings returns a list's known string elements in order; argument order is part of a routine's signature.
func routineStrings(value types.List) []string {
	if value.IsNull() || value.IsUnknown() {
		return nil
	}
	var values []string
	for _, element := range value.Elements() {
		if text, ok := element.(types.String); ok && !text.IsNull() && !text.IsUnknown() {
			values = append(values, text.ValueString())
		}
	}
	return values
}

// routineOwnerMatches compares owners case-insensitively, because Redshift folds identifiers to lowercase unless
// enable_case_sensitive_identifier is set.
func routineOwnerMatches(configured types.String, catalog string) bool {
	return !configured.IsNull() && !configured.IsUnknown() && strings.EqualFold(configured.ValueString(), catalog)
}

// routineOwner keeps the configured spelling of an owner that the catalog reports in another case.
func routineOwner(configured types.String, catalog string) types.String {
	if routineOwnerMatches(configured, catalog) {
		return configured
	}
	return types.StringValue(catalog)
}

// routineVolatility maps pg_proc.provolatile to its CREATE FUNCTION keyword.
func routineVolatility(code string) (string, error) {
	switch code {
	case "v":
		return "VOLATILE", nil
	case "s":
		return "STABLE", nil
	case "i":
		return "IMMUTABLE", nil
	}
	return "", fmt.Errorf("unknown routine volatility %q", code)
}

// routineOwnerStep changes the owner with ALTER FUNCTION or ALTER PROCEDURE. An owner left unconfigured keeps its
// state value, so nothing is rendered for it.
func routineOwnerStep[M any](owner func(M) types.String, alter func(M) sqlclient.Statement) alterStep[M] {
	return alterStep[M]{
		attribute: "owner",
		value:     func(data M) attr.Value { return owner(data) },
		render: func(_, plan M) []string {
			name := knownString(owner(plan))
			if name == "" {
				return nil
			}
			return []string{alter(plan).KwIdent("OWNER TO", name).String()}
		},
	}
}

// routineDefinitionAttribute names one attribute that CREATE OR REPLACE restates.
type routineDefinitionAttribute[M any] struct {
	// name is the schema attribute.
	name string
	// value extracts it from a model.
	value func(M) attr.Value
}

// routineRedefinitionSteps returns one step per definition attribute. CREATE OR REPLACE restates the whole
// definition, so only the first changed attribute renders the statement and the others render nothing.
func routineRedefinitionSteps[M any](statement string, attributes ...routineDefinitionAttribute[M]) []alterStep[M] {
	steps := make([]alterStep[M], len(attributes))
	for i, attribute := range attributes {
		earlier := attributes[:i]
		steps[i] = alterStep[M]{
			attribute: attribute.name,
			value:     attribute.value,
			render: func(prev, plan M) []string {
				for _, other := range earlier {
					if after := other.value(plan); !after.IsUnknown() && !other.value(prev).Equal(after) {
						return nil
					}
				}
				return []string{statement}
			},
		}
	}
	return steps
}

// routineImportArguments reads the canonical input argument types from an import identity. The key must be
// present, with "" for a routine without input arguments, so an omitted key cannot silently select that overload.
func routineImportArguments(id string) ([]string, sqlclient.Keyword, error) {
	var values map[string]string
	if err := json.Unmarshal([]byte(id), &values); err != nil {
		return nil, "", fmt.Errorf("expected a JSON object: %w", err)
	}
	text, ok := values[routineIdentityArguments]
	if !ok {
		return nil, "", fmt.Errorf(`missing field %s; use "" for a routine without input arguments`, routineIdentityArguments)
	}
	signature, err := routineSignature(routineSignatureTypes(text))
	if err != nil {
		return nil, "", err
	}
	return routineSignatureTypes(string(signature)), signature, nil
}
