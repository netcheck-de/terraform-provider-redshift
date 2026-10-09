package provider

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// definitionFingerprint hashes catalog definition text after collapsing whitespace, because Redshift
// re-renders view, routine, and policy bodies with its own spacing and line breaks.
func definitionFingerprint(catalogText string) string {
	sum := sha256.Sum256([]byte(strings.Join(strings.Fields(catalogText), " ")))
	return hex.EncodeToString(sum[:])
}

// definitionFingerprintAttribute exposes the fingerprint that detects definition changes made outside
// Terraform.
func definitionFingerprintAttribute() schema.StringAttribute {
	return schema.StringAttribute{Computed: true, MarkdownDescription: "SHA-256 of the catalog definition with whitespace collapsed; detects definition changes made outside Terraform."}
}

// recordDefinition chooses the definition and fingerprint to store after Create or Update re-read the catalog.
// The planned fingerprint is unknown then, so it cannot go through reconcileDefinition, which would surface the
// catalog text and make Terraform reject the apply for changing the configured definition. State keeps the
// configured text and records the fingerprint of what the catalog now holds.
func recordDefinition(configured types.String, catalogText string) (types.String, types.String) {
	return configured, types.StringValue(definitionFingerprint(catalogText))
}

// reconcileDefinition chooses the definition and fingerprint to store after Read. Redshift never returns the
// configured text verbatim, so comparing texts would always drift; instead, state keeps the configured text while
// the catalog still matches the fingerprint recordDefinition stored at apply time, and surfaces the catalog text,
// which then differs from configuration, once the catalog changed or nothing was configured, as after import.
func reconcileDefinition(configured, fingerprint types.String, catalogText string) (types.String, types.String) {
	current := definitionFingerprint(catalogText)
	if configured.IsNull() || configured.IsUnknown() || fingerprint.ValueString() != current {
		return types.StringValue(catalogText), types.StringValue(current)
	}
	return configured, fingerprint
}
