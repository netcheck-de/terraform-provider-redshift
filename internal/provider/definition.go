package provider

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// definitionFingerprint hashes catalog definition text after collapsing whitespace outside quoted text and comments,
// because Redshift re-renders view, routine, and policy bodies with its own spacing and line breaks but keeps
// literals, quoted identifiers, and comments as written.
func definitionFingerprint(catalogText string) string {
	sum := sha256.Sum256([]byte(definitionNormalize(catalogText)))
	return hex.EncodeToString(sum[:])
}

// definitionNormalize collapses each whitespace run outside quoted text and comments to one space and trims the ends.
// String literals, quoted identifiers, dollar-quoted text, and comments are kept verbatim, and the line break that
// ends a line comment stays a line break, so text after it cannot be read as part of the comment.
func definitionNormalize(text string) string {
	var out strings.Builder

	separator := ""

	for i := 0; i < len(text); {
		if strings.ContainsRune(" \t\n\r\f\v", rune(text[i])) {
			if separator == "" && out.Len() > 0 {
				separator = " "
			}

			i++

			continue
		}

		end := definitionSpanEnd(text, i)

		out.WriteString(separator)
		out.WriteString(text[i:end])

		separator = ""
		if strings.HasPrefix(text[i:], "--") && end < len(text) {
			separator = "\n"
		}

		i = end
	}

	return out.String()
}

// definitionSpanEnd returns the end of the token starting at start: a whole literal, quoted identifier, dollar-quoted
// string, or comment, which may run to the end of an unterminated text, or else one byte.
func definitionSpanEnd(text string, start int) int {
	rest := text[start:]
	switch {
	case rest[0] == '\'':
		// Redshift treats a backslash in a literal as an escape, besides the doubled quote.
		for i := 1; i < len(rest); i++ {
			switch {
			case rest[i] == '\\':
				i++
			case rest[i] == '\'' && i+1 < len(rest) && rest[i+1] == '\'':
				i++
			case rest[i] == '\'':
				return start + i + 1
			}
		}
	case rest[0] == '"':
		for i := 1; i < len(rest); i++ {
			if rest[i] == '"' {
				if i+1 < len(rest) && rest[i+1] == '"' {
					i++
					continue
				}

				return start + i + 1
			}
		}
	case strings.HasPrefix(rest, "--"):
		if newline := strings.IndexByte(rest, '\n'); newline >= 0 {
			return start + newline
		}
	case strings.HasPrefix(rest, "/*"):
		if end := strings.Index(rest[2:], "*/"); end >= 0 {
			return start + 2 + end + 2
		}
	case rest[0] == '$':
		tag := definitionDollarTag(rest)
		if tag == "" {
			return start + 1
		}

		if end := strings.Index(rest[len(tag):], tag); end >= 0 {
			return start + len(tag) + end + len(tag)
		}
	default:
		return start + 1
	}

	return len(text)
}

// definitionDollarTag returns the opening $tag$ of a dollar-quoted string at the start of text, or "" when the dollar
// sign starts something else, such as the positional parameter $1.
func definitionDollarTag(text string) string {
	for i := 1; i < len(text); i++ {
		c := text[i]
		switch {
		case c == '$':
			return text[:i+1]
		case c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || i > 1 && c >= '0' && c <= '9':
		default:
			return ""
		}
	}

	return ""
}

// definitionFingerprintAttribute exposes the fingerprint that detects definition changes made outside
// Terraform.
func definitionFingerprintAttribute() schema.StringAttribute {
	return schema.StringAttribute{Computed: true, MarkdownDescription: "SHA-256 of the catalog definition with whitespace collapsed outside literals, quoted identifiers, and comments; detects definition changes made outside Terraform."}
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

	stored := fingerprint.ValueString()
	if configured.IsNull() || configured.IsUnknown() || stored != current {
		return types.StringValue(catalogText), types.StringValue(current)
	}

	return configured, types.StringValue(current)
}
