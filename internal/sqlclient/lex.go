package sqlclient

import "strings"

// lexemeKind classifies a span of SQL text by the lexical construct that owns it.
type lexemeKind int

const (
	// lexCode is ordinary SQL text outside quotes, comments and placeholders.
	lexCode lexemeKind = iota
	// lexString is a single-quoted literal, including E-prefixed strings.
	lexString
	// lexQuotedIdentifier is a double-quoted identifier.
	lexQuotedIdentifier
	// lexLineComment runs from -- to the end of the line, including the newline.
	lexLineComment
	// lexBlockComment is a possibly nested /* */ comment.
	lexBlockComment
	// lexDollarQuote is a $tag$ body $tag$ constant.
	lexDollarQuote
	// lexOperator is :: or :=, which must not be read as a placeholder.
	lexOperator
	// lexParameter is a :name placeholder.
	lexParameter
)

// lexeme is one classified span of SQL text.
type lexeme struct {
	// kind selects the lexical construct.
	kind lexemeKind
	// text is the exact source span.
	text string
	// open marks a quote, comment or dollar body that runs to the end of the text without its terminator.
	open bool
}

// identifierByte recognizes ASCII parameter names and dollar-quote tag characters.
func identifierByte(value byte, first bool) bool {
	return value == '_' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || !first && value >= '0' && value <= '9'
}

// reading selects whose view of SQL text lex reproduces.
type reading bool

const (
	// transportReading matches the direct transport's parameter binder (internal/redshiftconn/parameters.go),
	// so placeholders found under it are exactly the ones the binder replaces.
	transportReading reading = false
	// redshiftReading matches the server: backslash escapes apply in every literal, and $ continues an identifier,
	// so x$a$ is a name rather than the start of a dollar quote.
	redshiftReading reading = true
)

// readings lists both views; text that must not end a statement early is checked under each of them.
var readings = []reading{transportReading, redshiftReading}

// identifierContinues reports whether the $ at offset extends a name in the current code span under the Redshift
// reading. Bytes of multibyte UTF-8 characters count because Redshift accepts them in identifiers. The word must
// start like a name: after a number such as 1 or a parameter such as $1 the server opens a dollar quote instead.
func identifierContinues(sql string, code, offset int) bool {
	if code < 0 {
		return false
	}
	start := offset
	for start > code && (identifierByte(sql[start-1], false) || sql[start-1] == '$' || sql[start-1] >= 0x80) {
		start--
	}
	return start < offset && (identifierByte(sql[start], true) || sql[start] >= 0x80)
}

// lex splits SQL text into lexemes. Under transportReading the boundaries equal those of the direct transport's
// binder, which honors backslash escapes only in E-prefixed strings and opens a dollar quote at every $tag$.
// Callers that embed text check both readings, so a crafted literal or name cannot hide a statement boundary
// from either.
func lex(sql string, view reading) []lexeme {
	var lexemes []lexeme
	code := -1
	flush := func(end int) {
		if code >= 0 {
			lexemes = append(lexemes, lexeme{kind: lexCode, text: sql[code:end]})
			code = -1
		}
	}
	emit := func(kind lexemeKind, start, end int, open bool) {
		flush(start)
		lexemes = append(lexemes, lexeme{kind: kind, text: sql[start:end], open: open})
	}
	for offset := 0; offset < len(sql); {
		start := offset
		switch {
		case sql[offset] == '\'' || sql[offset] == '"':
			quote := sql[offset]
			escaped := quote == '\'' && (view == redshiftReading || start > 0 && (sql[start-1] == 'E' || sql[start-1] == 'e') && (start == 1 || !identifierByte(sql[start-2], false)))
			offset++
			open := true
			for offset < len(sql) {
				if sql[offset] == '\\' && escaped && offset+1 < len(sql) {
					offset += 2
					continue
				}
				if sql[offset] == quote {
					offset++
					if offset < len(sql) && sql[offset] == quote {
						offset++
						continue
					}
					open = false
					break
				}
				offset++
			}
			kind := lexString
			if quote == '"' {
				kind = lexQuotedIdentifier
			}
			emit(kind, start, offset, open)
		case strings.HasPrefix(sql[offset:], "--"):
			open := true
			if end := strings.IndexByte(sql[offset:], '\n'); end >= 0 {
				offset += end + 1
				open = false
			} else {
				offset = len(sql)
			}
			emit(lexLineComment, start, offset, open)
		case strings.HasPrefix(sql[offset:], "/*"):
			offset += 2
			depth := 1
			for offset < len(sql) && depth != 0 {
				switch {
				case strings.HasPrefix(sql[offset:], "/*"):
					depth++
					offset += 2
				case strings.HasPrefix(sql[offset:], "*/"):
					depth--
					offset += 2
				default:
					offset++
				}
			}
			emit(lexBlockComment, start, offset, depth != 0)
		case sql[offset] == '$' && dollarDelimiter(sql[offset:]) != "" && (view == transportReading || !identifierContinues(sql, code, offset)):
			delimiter := dollarDelimiter(sql[offset:])
			open := true
			if closeAt := strings.Index(sql[offset+len(delimiter):], delimiter); closeAt >= 0 {
				offset += len(delimiter) + closeAt + len(delimiter)
				open = false
			} else {
				offset = len(sql)
			}
			emit(lexDollarQuote, start, offset, open)
		case strings.HasPrefix(sql[offset:], "::") || strings.HasPrefix(sql[offset:], ":="):
			offset += 2
			emit(lexOperator, start, offset, false)
		case sql[offset] == ':' && offset+1 < len(sql) && identifierByte(sql[offset+1], true):
			offset += 2
			for offset < len(sql) && identifierByte(sql[offset], false) {
				offset++
			}
			emit(lexParameter, start, offset, false)
		default:
			if code < 0 {
				code = offset
			}
			offset++
		}
	}
	flush(len(sql))
	return lexemes
}

// dollarDelimiter returns the $tag$ opening at the start of text, or "" when text does not open a dollar quote.
// A digit cannot start a tag, so positional parameters such as $1 stay ordinary code.
func dollarDelimiter(text string) string {
	end := 1
	for end < len(text) && identifierByte(text[end], end == 1) {
		end++
	}
	if end < len(text) && text[end] == '$' {
		return text[:end+1]
	}
	return ""
}

// placeholders returns the distinct :name parameters in order of first use, as the transports bind them.
func placeholders(sql string) []string {
	var names []string
	seen := map[string]bool{}
	for _, item := range lex(sql, transportReading) {
		if name := strings.TrimPrefix(item.text, ":"); item.kind == lexParameter && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	return names
}

// endsInLineComment reports whether text ends inside a -- comment under either reading,
// in which case anything appended on the same line would be commented out.
func endsInLineComment(text string) bool {
	for _, view := range readings {
		lexemes := lex(text, view)
		if last := len(lexemes) - 1; last >= 0 && lexemes[last].kind == lexLineComment && lexemes[last].open {
			return true
		}
	}
	return false
}
