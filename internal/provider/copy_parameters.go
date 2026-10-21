package provider

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// copyArgument selects what follows a COPY parameter keyword.
type copyArgument int

const (
	// copyFlag is a keyword without an argument, such as GZIP.
	copyFlag copyArgument = iota
	// copyText takes a string literal, such as DELIMITER ','.
	copyText
	// copyOptionalText takes an optional string literal, such as ACCEPTINVCHARS [ '?' ].
	copyOptionalText
	// copyInteger takes an integer, such as IGNOREHEADER 1.
	copyInteger
	// copyChoice takes one keyword of an allowlist, such as ENCODING UTF16.
	copyChoice
)

// copyScope limits a parameter to templates or to copy jobs.
type copyScope int

const (
	// copyEverywhere is accepted by templates and copy jobs.
	copyEverywhere copyScope = iota
	// copyTemplateOnly is rejected by COPY JOB, which refuses MAXERROR and IGNOREALLERRORS.
	copyTemplateOnly
	// copyJobOnly names the data source, which CREATE TEMPLATE excludes.
	copyJobOnly
)

// copyParameter describes one COPY parameter keyword.
type copyParameter struct {
	// argument selects the value that follows the keyword.
	argument copyArgument
	// choices allowlists the keyword values of a copyChoice parameter, in their canonical spelling.
	choices []sqlclient.Keyword
	// scope limits the parameter to templates or copy jobs.
	scope copyScope
	// spelling replaces the keyword when rendering, where the grammar requires a following word, as NULL AS does.
	spelling sqlclient.Keyword
}

// copyParameters is the one table that validates, renders, and parses the data format, file compression, data
// conversion, and data load parameters of COPY, keyed by their UPPERCASE keyword. Source paths, authorization, and
// MANIFEST are not parameters here: a template excludes them and a copy job configures its source and IAM role as
// separate arguments.
// https://docs.aws.amazon.com/redshift/latest/dg/copy-parameters-data-format.html
// https://docs.aws.amazon.com/redshift/latest/dg/copy-parameters-file-compression.html
// https://docs.aws.amazon.com/redshift/latest/dg/copy-parameters-data-conversion.html
// https://docs.aws.amazon.com/redshift/latest/dg/copy-parameters-data-load.html
var copyParameters = map[sqlclient.Keyword]copyParameter{
	"ACCEPTANYDATE":    {argument: copyFlag},
	"ACCEPTINVCHARS":   {argument: copyOptionalText},
	"BLANKSASNULL":     {argument: copyFlag},
	"BZIP2":            {argument: copyFlag},
	"COMPROWS":         {argument: copyInteger},
	"COMPUPDATE":       {argument: copyChoice, choices: []sqlclient.Keyword{"ON", "OFF", "PRESET"}},
	"DATEFORMAT":       {argument: copyText},
	"DELIMITER":        {argument: copyText},
	"EMPTYASNULL":      {argument: copyFlag},
	"ENCODING":         {argument: copyChoice, choices: []sqlclient.Keyword{"UTF8", "UTF16", "UTF16LE", "UTF16BE", "ISO88591"}},
	"ENCRYPTED":        {argument: copyFlag},
	"ESCAPE":           {argument: copyFlag},
	"EXPLICIT_IDS":     {argument: copyFlag},
	"FILLRECORD":       {argument: copyFlag},
	"GZIP":             {argument: copyFlag},
	"IGNOREALLERRORS":  {argument: copyFlag, scope: copyTemplateOnly},
	"IGNOREBLANKLINES": {argument: copyFlag},
	"IGNOREHEADER":     {argument: copyInteger},
	"LZOP":             {argument: copyFlag},
	"MAXERROR":         {argument: copyInteger, scope: copyTemplateOnly},
	"NOLOAD":           {argument: copyFlag},
	"NULL":             {argument: copyText, spelling: "NULL AS"},
	"QUOTE":            {argument: copyText},
	"REGION":           {argument: copyText, scope: copyJobOnly},
	"REMOVEQUOTES":     {argument: copyFlag},
	"ROUNDEC":          {argument: copyFlag},
	"STATUPDATE":       {argument: copyChoice, choices: []sqlclient.Keyword{"ON", "OFF"}},
	"TIMEFORMAT":       {argument: copyText},
	"TRIMBLANKS":       {argument: copyFlag},
	"TRUNCATECOLUMNS":  {argument: copyFlag},
	"ZSTD":             {argument: copyFlag},
}

// copyFormats lists the data_format keywords of FORMAT [AS] data_format and their argument: JSON and AVRO take
// 'auto', 'auto ignorecase', or a JSONPaths file, FIXEDWIDTH its column specification.
var copyFormats = map[sqlclient.Keyword]copyArgument{
	"AVRO":       copyText,
	"CSV":        copyFlag,
	"FIXEDWIDTH": copyText,
	"JSON":       copyText,
	"ORC":        copyFlag,
	"PARQUET":    copyFlag,
	"SHAPEFILE":  copyFlag,
}

// copyFormatKeywords lists the format keywords in order, so error messages do not depend on map order.
func copyFormatKeywords() []sqlclient.Keyword {
	return slices.Sorted(maps.Keys(copyFormats))
}

// copyFormatNames lists the format keywords for validators and descriptions.
func copyFormatNames() []string {
	keywords := copyFormatKeywords()
	names := make([]string, len(keywords))
	for i, keyword := range keywords {
		names[i] = string(keyword)
	}
	return names
}

// copyParameterNames lists the parameter keywords one kind of object accepts, sorted.
func copyParameterNames(job bool) []string {
	var names []string
	for name, parameter := range copyParameters {
		if parameter.allowed(job) {
			names = append(names, string(name))
		}
	}
	slices.Sort(names)
	return names
}

// allowed reports whether a template (job = false) or a copy job accepts the parameter.
func (p copyParameter) allowed(job bool) bool {
	return p.scope == copyEverywhere || (p.scope == copyJobOnly) == job
}

// copyParameterKeyword returns the table's keyword for a configured key. Keys must be spelled in UPPERCASE as the
// table holds them, so configuration, state, and the catalog share one spelling.
func copyParameterKeyword(name string, job bool) (sqlclient.Keyword, copyParameter, error) {
	for keyword, parameter := range copyParameters {
		if string(keyword) != name {
			continue
		}
		if !parameter.allowed(job) {
			kind := "a template"
			if job {
				kind = "a copy job"
			}
			return "", copyParameter{}, fmt.Errorf("COPY parameter %s is not allowed in %s", name, kind)
		}
		return keyword, parameter, nil
	}
	return "", copyParameter{}, fmt.Errorf("unsupported COPY parameter %q; use one of %s in UPPERCASE", name, strings.Join(copyParameterNames(job), ", "))
}

// copyCanonicalValue validates a parameter value and returns the spelling the catalog reports: a choice in
// UPPERCASE and an integer without sign or leading zeros. Text is compared as written.
func copyCanonicalValue(name string, parameter copyParameter, value string) (string, error) {
	switch parameter.argument {
	case copyFlag:
		if value != "" {
			return "", fmt.Errorf("COPY parameter %s takes no value; set it to \"\"", name)
		}
	case copyInteger:
		number, err := strconv.ParseInt(value, 10, 64)
		if err != nil || number < 0 {
			return "", fmt.Errorf("COPY parameter %s needs a non-negative integer, got %q", name, value)
		}
		return strconv.FormatInt(number, 10), nil
	case copyChoice:
		choice, err := sqlclient.OneOf(value, parameter.choices...)
		if err != nil {
			return "", fmt.Errorf("COPY parameter %s: %w", name, err)
		}
		return string(choice), nil
	}
	return value, nil
}

// copyLiteralValue decodes the server's quoted literal once, before doubled backslashes lose their meaning.
// Configured strings never pass through this decoder: literal backslash+n and newline are different values.
// Unknown escapes such as NULL AS '\N' remain literal.
func copyLiteralValue(token sqlclient.Token) string {
	if token.Raw == "" {
		return token.Text
	}
	value := token.Raw[1 : len(token.Raw)-1]
	var result strings.Builder
	for i := 0; i < len(value); i++ {
		if value[i] == '\'' && i+1 < len(value) && value[i+1] == '\'' {
			result.WriteByte('\'')
			i++
			continue
		}
		if value[i] == '\\' && i+1 < len(value) {
			var control byte
			switch value[i+1] {
			case '\\', '\'':
				control = value[i+1]
			case 't':
				control = '\t'
			case 'n':
				control = '\n'
			case 'r':
				control = '\r'
			case 'b':
				control = '\b'
			case 'f':
				control = '\f'
			case 'v':
				control = '\v'
			}
			if control != 0 {
				result.WriteByte(control)
				i++
				continue
			}
		}
		result.WriteByte(value[i])
	}
	return result.String()
}

// copyCanonicalParameters validates configured parameters and returns them in their catalog spelling.
func copyCanonicalParameters(parameters map[string]string, job bool) (map[string]string, error) {
	canonical := make(map[string]string, len(parameters))
	for _, name := range slices.Sorted(maps.Keys(parameters)) {
		_, parameter, err := copyParameterKeyword(name, job)
		if err != nil {
			return nil, err
		}
		if canonical[name], err = copyCanonicalValue(name, parameter, parameters[name]); err != nil {
			return nil, err
		}
	}
	return canonical, nil
}

// copyCheckFormat validates format and its argument; an empty format means the default pipe-delimited text.
func copyCheckFormat(format, argument string) (sqlclient.Keyword, error) {
	if format == "" {
		if argument != "" {
			return "", fmt.Errorf("format_argument requires format")
		}
		return "", nil
	}
	keyword, err := sqlclient.OneOf(format, copyFormatKeywords()...)
	if err != nil {
		return "", fmt.Errorf("format: %w", err)
	}
	if keyword == "SHAPEFILE" {
		_, err := copySimplify(argument)
		return keyword, err
	}
	switch copyFormats[keyword] {
	case copyText:
		if argument == "" {
			return "", fmt.Errorf("format %s needs format_argument, such as 'auto'", keyword)
		}
	default:
		if argument != "" {
			return "", fmt.Errorf("format %s takes no format_argument", keyword)
		}
	}
	return keyword, nil
}

// copyFormatFragment renders FORMAT AS data_format [ 'argument' ], or nothing for the default format.
func copyFormatFragment(format, argument string) (sqlclient.Statement, error) {
	keyword, err := copyCheckFormat(format, argument)
	if err != nil || keyword == "" {
		return sqlclient.Fragment(), err
	}
	fragment := sqlclient.Kw("FORMAT AS", keyword)
	if keyword == "SHAPEFILE" {
		simplify, err := copySimplify(argument)
		return fragment.Append(simplify), err
	}
	if argument != "" {
		fragment = fragment.Lit(argument)
	}
	return fragment, nil
}

// copySimplify models the documented SHAPEFILE subclause without accepting arbitrary SQL.
func copySimplify(argument string) (sqlclient.Statement, error) {
	fragment := sqlclient.Fragment()
	fields := strings.Fields(argument)
	if len(fields) == 0 {
		return fragment, nil
	}
	if !strings.EqualFold(fields[0], "SIMPLIFY") {
		return fragment, fmt.Errorf("SHAPEFILE format_argument must be SIMPLIFY [AUTO] [tolerance]")
	}
	fragment = fragment.Kw("SIMPLIFY")
	fields = fields[1:]
	if len(fields) > 0 && strings.EqualFold(fields[0], "AUTO") {
		fragment = fragment.Kw("AUTO")
		fields = fields[1:]
	}
	if len(fields) == 0 {
		return fragment, nil
	}
	value, err := strconv.ParseFloat(fields[0], 64)
	if len(fields) != 1 || err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return fragment, fmt.Errorf("SHAPEFILE SIMPLIFY tolerance must be a finite non-negative number")
	}
	// The command reference shows quotes, but the live parser requires an unquoted decimal, not a string literal.
	if value == 0 {
		value = 0 // Normalize negative zero before emitting an unsigned decimal.
	}
	tolerance := sqlclient.Keyword(strconv.FormatFloat(value, 'f', -1, 64)) //sql:trusted ParseFloat checked a finite non-negative number; FormatFloat emits only decimal digits and a decimal point.
	return fragment.Kw(tolerance), nil
}

// copyFormatArgumentValue preserves equivalent configured SHAPEFILE spelling and numeric precision.
func copyFormatArgumentValue(configured types.String, format, catalog string) types.String {
	if strings.EqualFold(format, "SHAPEFILE") && !configured.IsUnknown() && !configured.IsNull() {
		before, err := copySimplify(configured.ValueString())
		after, otherErr := copySimplify(catalog)
		if err == nil && otherErr == nil && before.String() == after.String() {
			return configured
		}
	}
	if catalog == "" {
		if !configured.IsUnknown() && !configured.IsNull() && configured.ValueString() == "" {
			return configured
		}
		return types.StringNull()
	}
	return types.StringValue(catalog)
}

// copyParameterFragment renders one parameter with its argument, for CREATE and ALTER TEMPLATE ... ADD.
func copyParameterFragment(name, value string, job bool) (sqlclient.Statement, error) {
	keyword, parameter, err := copyParameterKeyword(name, job)
	if err != nil {
		return sqlclient.Statement{}, err
	}
	canonical, err := copyCanonicalValue(name, parameter, value)
	if err != nil {
		return sqlclient.Statement{}, err
	}
	fragment := sqlclient.Kw(keyword)
	if parameter.spelling != "" {
		fragment = sqlclient.Kw(parameter.spelling)
	}
	return copyArgumentFragment(fragment, parameter, canonical)
}

// copyArgumentFragment appends a validated, canonical argument to a fragment.
func copyArgumentFragment(fragment sqlclient.Statement, parameter copyParameter, canonical string) (sqlclient.Statement, error) {
	switch parameter.argument {
	case copyText:
		return fragment.Lit(canonical), nil
	case copyOptionalText:
		if canonical == "" {
			return fragment, nil
		}
		return fragment.Lit(canonical), nil
	case copyInteger:
		number, _ := strconv.ParseInt(canonical, 10, 64) // copyCanonicalValue parsed it.
		return fragment.Int(number), nil
	case copyChoice:
		choice, err := sqlclient.OneOf(canonical, parameter.choices...)
		return fragment.Kw(choice), err
	}
	return fragment, nil
}

// copyParameterFragments renders every parameter in keyword order, so statements do not depend on map order.
func copyParameterFragments(parameters map[string]string, job bool) ([]sqlclient.Statement, error) {
	fragments := make([]sqlclient.Statement, 0, len(parameters))
	for _, name := range slices.Sorted(maps.Keys(parameters)) {
		fragment, err := copyParameterFragment(name, parameters[name], job)
		if err != nil {
			return nil, err
		}
		fragments = append(fragments, fragment)
	}
	return fragments, nil
}

// copyDefinition is the format and parameters parsed from server-printed COPY options.
type copyDefinition struct {
	// format is the data_format keyword, or "" for the default text format.
	format string
	// formatArgument is the format's string argument, such as auto for JSON 'auto'.
	formatArgument string
	// parameters maps each parameter keyword to its canonical value, "" for flags.
	parameters map[string]string
}

// parseCopyOptions parses COPY options as SHOW TEMPLATE prints them: keywords in any order, AS kept or dropped
// (NULL AS ” is printed as NULL ”), and optional commas between parameters. A keyword the table does not know is
// an error, so an option this provider cannot manage is reported rather than dropped.
func parseCopyOptions(tokens []sqlclient.Token) (copyDefinition, error) {
	definition := copyDefinition{parameters: map[string]string{}}
	simplify := ""
	next := func(i int) (sqlclient.Token, bool) {
		if i < len(tokens) {
			return tokens[i], true
		}
		return sqlclient.Token{}, false
	}
	isWord := func(i int, word string) bool {
		token, ok := next(i)
		return ok && token.Kind == sqlclient.TokenWord && strings.EqualFold(token.Text, word)
	}
	for i := 0; i < len(tokens); i++ {
		token := tokens[i]
		if token.Kind == sqlclient.TokenSymbol && token.Text == "," {
			continue
		}
		if token.Kind == sqlclient.TokenSymbol && token.Text == ";" && i == len(tokens)-1 {
			break
		}
		if token.Kind != sqlclient.TokenWord {
			return copyDefinition{}, fmt.Errorf("unexpected %q in COPY options", token.Text)
		}
		word := strings.ToUpper(token.Text)
		if word == "SIMPLIFY" {
			if simplify != "" {
				return copyDefinition{}, fmt.Errorf("COPY options name SIMPLIFY twice")
			}
			simplify = "SIMPLIFY"
			if isWord(i+1, "AUTO") {
				i++
				simplify += " AUTO"
			}
			if value, ok := next(i + 1); ok && (value.Kind == sqlclient.TokenString || value.Kind == sqlclient.TokenWord && len(value.Text) > 0 && value.Text[0] >= '0' && value.Text[0] <= '9') {
				i++
				tolerance := value.Text
				// Unquoted decimal and exponent tolerances are split at punctuation by Tokens.
				for value.Kind != sqlclient.TokenString && i+2 < len(tokens) && tokens[i+1].Kind == sqlclient.TokenSymbol && strings.ContainsAny(tokens[i+1].Text, ".+-") && tokens[i+2].Kind == sqlclient.TokenWord {
					tolerance += tokens[i+1].Text + tokens[i+2].Text
					i += 2
				}
				simplify += " " + tolerance
			}
			if _, err := copySimplify(simplify); err != nil {
				return copyDefinition{}, err
			}
			continue
		}
		explicitFormat := word == "FORMAT"
		if explicitFormat {
			i++
			if isWord(i, "AS") {
				i++
			}
			following, ok := next(i)
			if !ok || following.Kind != sqlclient.TokenWord {
				return copyDefinition{}, fmt.Errorf("FORMAT without a data format in COPY options")
			}
			word = strings.ToUpper(following.Text)
		}
		if argument, ok := copyFormatArgument(word); ok {
			if definition.format != "" {
				return copyDefinition{}, fmt.Errorf("COPY options name two formats, %s and %s", definition.format, word)
			}
			definition.format = word
			if argument == copyText {
				if isWord(i+1, "AS") {
					i++
				}
				value, ok := next(i + 1)
				if !ok || value.Kind != sqlclient.TokenString {
					return copyDefinition{}, fmt.Errorf("format %s without its argument in COPY options", word)
				}
				definition.formatArgument = copyLiteralValue(value)
				i++
			}
			continue
		}
		if explicitFormat {
			return copyDefinition{}, fmt.Errorf("unsupported COPY format %q", word)
		}
		parameter, ok := copyParameterByWord(word)
		if !ok {
			return copyDefinition{}, fmt.Errorf("unsupported COPY parameter %q", word)
		}
		if _, seen := definition.parameters[word]; seen {
			return copyDefinition{}, fmt.Errorf("COPY parameter %s appears twice", word)
		}
		if parameter.argument != copyFlag && isWord(i+1, "AS") {
			i++
		}
		value := ""
		following, present := next(i + 1)
		switch parameter.argument {
		case copyText, copyOptionalText:
			if present && following.Kind == sqlclient.TokenString {
				value = copyLiteralValue(following)
				i++
			} else if parameter.argument == copyText {
				return copyDefinition{}, fmt.Errorf("COPY parameter %s without its string argument", word)
			}
		case copyInteger, copyChoice:
			if !present || following.Kind != sqlclient.TokenWord {
				return copyDefinition{}, fmt.Errorf("COPY parameter %s without its argument", word)
			}
			canonical, err := copyCanonicalValue(word, parameter, following.Text)
			if err != nil {
				return copyDefinition{}, err
			}
			value = canonical
			i++
		}
		definition.parameters[word] = value
	}
	if simplify != "" {
		if definition.format != "SHAPEFILE" {
			return copyDefinition{}, fmt.Errorf("COPY SIMPLIFY requires SHAPEFILE format")
		}
		definition.formatArgument = simplify
	}
	return definition, nil
}

// copyFormatArgument looks up a format keyword in its UPPERCASE spelling.
func copyFormatArgument(word string) (copyArgument, bool) {
	for keyword, argument := range copyFormats {
		if string(keyword) == word {
			return argument, true
		}
	}
	return 0, false
}

// copyParameterByWord looks up a parameter keyword in its UPPERCASE spelling, whatever its scope, because the
// catalog reports what exists.
func copyParameterByWord(word string) (copyParameter, bool) {
	for keyword, parameter := range copyParameters {
		if string(keyword) == word {
			return parameter, true
		}
	}
	return copyParameter{}, false
}

// copyParametersValue reconciles configured parameters with the catalog's: state keeps the configured map while it
// equals the catalog after canonicalization, so IGNOREHEADER = "01" or ENCODING = "utf16" do not drift, and
// otherwise holds the catalog map, which a plan then moves back to the configuration. An empty catalog map keeps an
// unset or empty configuration as it is.
func copyParametersValue(configured types.Map, catalog map[string]string, job bool) types.Map {
	known := knownMap(configured)
	if canonical, err := copyCanonicalParameters(known, job); !configured.IsUnknown() && err == nil && maps.Equal(canonical, catalog) {
		if configured.IsNull() {
			return types.MapNull(types.StringType)
		}
		return configured
	}
	if len(catalog) == 0 {
		return types.MapNull(types.StringType)
	}
	elements := make(map[string]attr.Value, len(catalog))
	for name, value := range catalog {
		elements[name] = types.StringValue(value)
		if configuredValue, ok := known[name]; ok {
			if _, parameter, err := copyParameterKeyword(name, job); err == nil {
				if canonical, err := copyCanonicalValue(name, parameter, configuredValue); err == nil && canonical == value {
					elements[name] = types.StringValue(configuredValue)
				}
			}
		}
	}
	return types.MapValueMust(types.StringType, elements)
}

// copyValidateKnown checks independent known options even while other inputs await planning.
func copyValidateKnown(format, argument types.String, parameters types.Map, job bool) error {
	if !format.IsUnknown() {
		if !argument.IsUnknown() {
			if _, err := copyCheckFormat(knownString(format), knownString(argument)); err != nil {
				return err
			}
		} else if knownString(format) != "" {
			if _, err := sqlclient.OneOf(format.ValueString(), copyFormatKeywords()...); err != nil {
				return err
			}
		}
	}
	for name, value := range parameters.Elements() {
		_, parameter, err := copyParameterKeyword(name, job)
		if err != nil {
			return err
		}
		if value.IsUnknown() {
			continue
		}
		if value.IsNull() {
			return fmt.Errorf("COPY parameter %s must not be null", name)
		}
		if _, err := copyCanonicalValue(name, parameter, value.(types.String).ValueString()); err != nil {
			return err
		}
	}
	return nil
}

// copyOptionsKnown prevents apply renderers from treating deferred options as absent.
func copyOptionsKnown(format, argument types.String, parameters types.Map) bool {
	if format.IsUnknown() || argument.IsUnknown() || parameters.IsUnknown() {
		return false
	}
	for _, value := range parameters.Elements() {
		if value.IsUnknown() {
			return false
		}
	}
	return true
}
