package sqlclient

import (
	"fmt"
	"strconv"
	"strings"
)

// Keyword is SQL text that the builders emit unquoted, such as clause words, privileges and type names.
// Constants convert implicitly. A runtime value must come from OneOf or TypeName, or from a conversion annotated
// with //sql:trusted, because nothing quotes or escapes it.
type Keyword string

// UserSQL is configured SQL text, such as a view query or a column default, that is embedded verbatim.
// Only CheckUserSQL creates it, so the text cannot end the statement or leave a quote or comment open.
type UserSQL string

// OneOf returns the allowed keyword that equals value, ignoring case, so runtime input never becomes SQL text itself.
func OneOf(value string, allowed ...Keyword) (Keyword, error) {
	for _, keyword := range allowed {
		if strings.EqualFold(value, string(keyword)) {
			return keyword, nil
		}
	}
	names := make([]string, len(allowed))
	for i, keyword := range allowed {
		names[i] = string(keyword)
	}
	return "", fmt.Errorf("%q is not one of %s", value, strings.Join(names, ", "))
}

// typeModifier describes the parenthesized modifiers a Redshift type accepts; the zero value accepts none.
type typeModifier int

const (
	// lengthModifier accepts one length up to the type maximum, or MAX.
	lengthModifier typeModifier = iota + 1
	// precisionModifier accepts NUMERIC precision and optional scale.
	precisionModifier
	// fractionModifier accepts the fractional-second precision of INTERVAL DAY TO SECOND.
	fractionModifier
)

// typeRule maps one spelling to the canonical name: the one format_type() reports, in uppercase.
type typeRule struct {
	// canonical is the uppercase format_type() spelling without modifiers.
	canonical string
	// modifier selects the accepted parenthesized arguments.
	modifier typeModifier
	// maximum bounds a length, which MAX expands to so configuration matches the catalog.
	maximum int64
	// columnDefault is the modifier Redshift gives a column declared without one, for ColumnType.
	columnDefault string
}

// Maximum lengths documented for Redshift character and binary types.
const (
	maxCharLength    = 4096
	maxVarcharLength = 65535
	maxVarbyteLength = 1024000
	maxNumericDigits = 38
)

// typeRules covers the Redshift data types and their documented aliases.
// https://docs.aws.amazon.com/redshift/latest/dg/c_Supported_data_types.html
var typeRules = func() map[string]typeRule {
	rules := map[string]typeRule{}
	add := func(rule typeRule, spellings ...string) {
		for _, spelling := range append(spellings, rule.canonical) {
			rules[strings.ToLower(spelling)] = rule
		}
	}
	add(typeRule{canonical: "SMALLINT"}, "int2")
	add(typeRule{canonical: "INTEGER"}, "int", "int4")
	add(typeRule{canonical: "BIGINT"}, "int8")
	add(typeRule{canonical: "NUMERIC", modifier: precisionModifier, columnDefault: "(18,0)"}, "decimal")
	add(typeRule{canonical: "REAL"}, "float4")
	add(typeRule{canonical: "DOUBLE PRECISION"}, "float8", "float")
	add(typeRule{canonical: "BOOLEAN"}, "bool")
	character := typeRule{canonical: "CHARACTER", modifier: lengthModifier, maximum: maxCharLength, columnDefault: "(1)"}
	add(character, "char", "nchar")
	// BPCHAR is the same type, but a column declared as BPCHAR becomes CHAR(256) rather than CHAR(1).
	character.columnDefault = "(256)"
	rules["bpchar"] = character
	add(typeRule{canonical: "CHARACTER VARYING", modifier: lengthModifier, maximum: maxVarcharLength, columnDefault: "(256)"}, "varchar", "nvarchar", "text")
	add(typeRule{canonical: "DATE"})
	add(typeRule{canonical: "TIMESTAMP WITHOUT TIME ZONE"}, "timestamp")
	add(typeRule{canonical: "TIMESTAMP WITH TIME ZONE"}, "timestamptz")
	add(typeRule{canonical: "TIME WITHOUT TIME ZONE"}, "time")
	add(typeRule{canonical: "TIME WITH TIME ZONE"}, "timetz")
	add(typeRule{canonical: "INTERVAL YEAR TO MONTH"})
	add(typeRule{canonical: "INTERVAL DAY TO SECOND", modifier: fractionModifier})
	add(typeRule{canonical: "VARBYTE", modifier: lengthModifier, maximum: maxVarbyteLength, columnDefault: "(64000)"}, "varbinary", "binary varying")
	for _, name := range []string{"GEOMETRY", "GEOGRAPHY", "HLLSKETCH", "SUPER", "ANYELEMENT", "REFCURSOR"} {
		add(typeRule{canonical: name})
	}
	return rules
}()

// TypeName validates a Redshift data type, in any case, and returns the spelling format_type() reports for it in
// uppercase, for example int4 → INTEGER and varchar(10) → CHARACTER VARYING(10). Catalog types pass through it too, so
// configuration and catalog compare equal, and the provider writes and reports one spelling. Lengths are kept as written, except that MAX becomes the type's maximum. A type without modifiers stays bare, as
// routine arguments are reported; column definitions use ColumnType, which adds the server's default modifier.
func TypeName(value string) (Keyword, error) {
	normalized := normalizeTypeName(value)
	base, arguments, hasArguments := normalized, "", false
	if open := strings.IndexByte(normalized, '('); open >= 0 {
		if !strings.HasSuffix(normalized, ")") {
			return "", fmt.Errorf("data type %q has unbalanced parentheses", value)
		}
		base, arguments, hasArguments = strings.TrimSpace(normalized[:open]), normalized[open+1:len(normalized)-1], true
	}
	rule, found := typeRules[base]
	if !found {
		return "", fmt.Errorf("unsupported Redshift data type %q", value)
	}
	if !hasArguments {
		return Keyword(rule.canonical), nil
	}
	parts := strings.Split(arguments, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	numbers := make([]int64, len(parts))
	for i, part := range parts {
		if rule.modifier == lengthModifier && len(parts) == 1 && part == "max" {
			numbers[i] = rule.maximum
			continue
		}
		number, err := strconv.ParseInt(part, 10, 64)
		if err != nil || part != strconv.FormatInt(number, 10) {
			return "", fmt.Errorf("data type %q has an invalid modifier %q", value, part)
		}
		numbers[i] = number
	}
	switch {
	case rule.modifier == lengthModifier && len(numbers) == 1 && numbers[0] >= 1 && numbers[0] <= rule.maximum:
		return Keyword(fmt.Sprintf("%s(%d)", rule.canonical, numbers[0])), nil
	case rule.modifier == precisionModifier && len(numbers) == 1 && numbers[0] >= 1 && numbers[0] <= maxNumericDigits:
		// format_type() spells an omitted scale as 0.
		return Keyword(fmt.Sprintf("%s(%d,0)", rule.canonical, numbers[0])), nil
	case rule.modifier == precisionModifier && len(numbers) == 2 && numbers[0] >= 1 && numbers[0] <= maxNumericDigits && numbers[1] >= 0 && numbers[1] <= numbers[0]:
		return Keyword(fmt.Sprintf("%s(%d,%d)", rule.canonical, numbers[0], numbers[1])), nil
	case rule.modifier == fractionModifier && len(numbers) == 1 && numbers[0] >= 0 && numbers[0] <= 6:
		return Keyword(fmt.Sprintf("%s(%d)", rule.canonical, numbers[0])), nil
	}
	return "", fmt.Errorf("data type %q has unsupported modifiers", value)
}

// ColumnType is TypeName for column definitions. A type declared without modifiers gets the one Redshift gives the
// column, for example varchar → CHARACTER VARYING(256), char → CHARACTER(1) and bpchar → CHARACTER(256), so the
// result compares equal to format_type() of the column and never silently narrows it.
// https://docs.aws.amazon.com/redshift/latest/dg/r_Character_types.html
func ColumnType(value string) (Keyword, error) {
	name, err := TypeName(value)
	if err != nil {
		return "", err
	}
	if rule := typeRules[normalizeTypeName(value)]; rule.columnDefault != "" {
		return name + Keyword(rule.columnDefault), nil
	}
	return name, nil
}

// CatalogType returns the canonical spelling of a type the catalog reports, such as format_type() output, for the
// provider to compare and report. A type TypeName does not know is uppercased as well, so every reported type uses
// one case, unless it holds a quoted name, whose case is significant; it still surfaces as drift rather than an error.
func CatalogType(value string) string {
	if name, err := TypeName(value); err == nil {
		return string(name)
	}
	if strings.Contains(value, `"`) {
		return strings.TrimSpace(value)
	}
	return strings.ToUpper(strings.TrimSpace(value))
}

// normalizeTypeName lowercases a type and collapses its whitespace, so aliases are looked up by one spelling.
func normalizeTypeName(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

// Signature canonicalizes routine argument types into the comma-separated form that identifies an overload.
func Signature(argTypes ...string) (Keyword, error) {
	names := make([]string, len(argTypes))
	for i, argType := range argTypes {
		name, err := TypeName(argType)
		if err != nil {
			return "", fmt.Errorf("argument %d: %w", i+1, err)
		}
		names[i] = string(name)
	}
	return Keyword(strings.Join(names, ", ")), nil
}

// CheckUserSQL accepts configured SQL that stays one expression or query when embedded in a statement.
// It rejects ; outside quotes, unbalanced parentheses, and unterminated quotes, block comments or dollar quotes.
// The text must pass under both the transport's and Redshift's reading, so a crafted literal or name cannot hide a
// statement boundary from either. :name placeholders are rejected too: statements carrying user SQL run without
// parameters, and the direct transport would fail on an unbound placeholder only at apply time.
func CheckUserSQL(text string) (UserSQL, error) {
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("SQL text is empty")
	}
	for _, view := range readings {
		depth := 0
		for _, item := range lex(text, view) {
			if item.open && item.kind != lexLineComment {
				return "", fmt.Errorf("SQL text has an unterminated %s", lexemeNames[item.kind])
			}
			if item.kind == lexParameter {
				return "", fmt.Errorf("SQL text must not contain the parameter placeholder %s, because it runs without parameters", item.text)
			}
			if item.kind != lexCode {
				continue
			}
			for _, char := range item.text {
				switch char {
				case ';':
					return "", fmt.Errorf("SQL text must be a single statement or expression without ';'")
				case '(':
					depth++
				case ')':
					depth--
					if depth < 0 {
						return "", fmt.Errorf("SQL text has an unmatched ')'")
					}
				}
			}
		}
		if depth != 0 {
			return "", fmt.Errorf("SQL text has an unmatched '('")
		}
	}
	return UserSQL(text), nil
}

// lexemeNames describes the constructs that CheckUserSQL can report as unterminated.
var lexemeNames = map[lexemeKind]string{
	lexString:           "string literal",
	lexQuotedIdentifier: "quoted identifier",
	lexBlockComment:     "block comment",
	lexDollarQuote:      "dollar-quoted string",
}
