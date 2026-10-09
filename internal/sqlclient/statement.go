package sqlclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Statement is an immutable SQL statement or fragment built from quoted values and trusted keywords.
// Every method returns a new Statement and never shares mutable storage with its receiver, so a common prefix
// such as Stmt("ALTER USER").Ident(name) can be extended independently for each option.
// String joins the tokens with single spaces; a statement built with Stmt starts with its verb, which
// SerializeMutations relies on to tell reads from writes.
type Statement struct {
	// tokens are rendered SQL pieces, never empty.
	tokens []string
	// err is the first value that could not be rendered; it travels with every derived statement.
	err error
}

// Stmt starts a statement with its verb, for example Stmt("CREATE USER").
func Stmt(verb ...Keyword) Statement {
	return Fragment().Kw(verb...)
}

// Fragment starts an empty piece of SQL, such as a grantee or a list item, to embed in a statement.
func Fragment() Statement {
	return Statement{}
}

// Kw returns a fragment of keywords, a shorthand for list items such as privileges.
func Kw(words ...Keyword) Statement {
	return Fragment().Kw(words...)
}

// Ident returns a fragment with one quoted identifier, a shorthand for list items.
func Ident(name string) Statement {
	return Fragment().Ident(name)
}

// Lit returns a fragment with one string literal, a shorthand for list items.
func Lit(value string) Statement {
	return Fragment().Lit(value)
}

// Int returns a fragment with one integer, a shorthand for list items such as IDENTITY(1, 1).
func Int(value int64) Statement {
	return Fragment().Int(value)
}

// with copies the receiver and appends the nonempty tokens, so derived statements never alias each other.
func (s Statement) with(tokens ...string) Statement {
	out := Statement{tokens: make([]string, len(s.tokens), len(s.tokens)+len(tokens)), err: s.err}
	copy(out.tokens, s.tokens)
	for _, token := range tokens {
		if token != "" {
			out.tokens = append(out.tokens, token)
		}
	}
	return out
}

// fail records the first rendering error.
func (s Statement) fail(err error) Statement {
	if s.err == nil {
		s.err = err
	}
	return s
}

// String renders the statement with single spaces between tokens.
func (s Statement) String() string {
	return strings.Join(s.tokens, " ")
}

// Err returns the first value that could not be rendered, so renderers can return it with String.
func (s Statement) Err() error {
	return s.err
}

// Kw appends trusted keywords; empty keywords are skipped.
func (s Statement) Kw(words ...Keyword) Statement {
	tokens := make([]string, len(words))
	for i, word := range words {
		tokens[i] = string(word)
	}
	return s.with(tokens...)
}

// Ident appends a quoted identifier.
func (s Statement) Ident(name string) Statement {
	return s.with(Identifier(name))
}

// Qualified appends a dotted name such as "schema"."table". Empty qualifiers before the last part are omitted,
// so an optional schema needs no branch; the last part is always rendered.
func (s Statement) Qualified(parts ...string) Statement {
	var quoted []string
	for i, part := range parts {
		if part != "" || i == len(parts)-1 {
			quoted = append(quoted, Identifier(part))
		}
	}
	return s.with(strings.Join(quoted, "."))
}

// Lit appends a string literal.
func (s Statement) Lit(value string) Statement {
	return s.with(Literal(value))
}

// Int appends an integer.
func (s Statement) Int(value int64) Statement {
	return s.with(strconv.FormatInt(value, 10))
}

// Bool appends true or false.
func (s Statement) Bool(value bool) Statement {
	return s.with(strconv.FormatBool(value))
}

// JSON appends value encoded as JSON inside a string literal, for options such as identity provider PARAMETERS.
func (s Statement) JSON(value any) Statement {
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	// HTML escapes would only make the literal harder to read in plans and logs.
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return s.fail(fmt.Errorf("encode JSON option: %w", err))
	}
	return s.Lit(strings.TrimSuffix(encoded.String(), "\n"))
}

// Body appends text as a dollar-quoted constant, such as a routine body. It uses $$ unless the text contains
// it (or ends in $), and otherwise the first of $body$, $body1$, … that the text cannot close early.
func (s Statement) Body(text string) Statement {
	tag := "$$"
	for n := 0; strings.Index(text+tag, tag) != len(text); n++ {
		suffix := ""
		if n > 0 {
			suffix = strconv.Itoa(n)
		}
		tag = "$body" + suffix + "$"
	}
	return s.with(tag + text + tag)
}

// Verbatim appends checked user SQL. A trailing newline is added when the text ends inside a -- comment,
// so the comment cannot swallow the rest of the statement.
func (s Statement) Verbatim(sql UserSQL) Statement {
	text := string(sql)
	if endsInLineComment(text) {
		text += "\n"
	}
	return s.with(text)
}

// Append adds the tokens of other fragments.
func (s Statement) Append(fragments ...Statement) Statement {
	out := s.with()
	for _, fragment := range fragments {
		out.tokens = append(out.tokens, fragment.tokens...)
		if fragment.err != nil {
			out = out.fail(fragment.err)
		}
	}
	return out
}

// KwIdent appends a keyword followed by a quoted identifier, such as OWNER TO "name".
func (s Statement) KwIdent(keyword Keyword, name string) Statement {
	return s.Kw(keyword).Ident(name)
}

// KwLit appends a keyword followed by a string literal, such as PASSWORD 'secret'.
func (s Statement) KwLit(keyword Keyword, value string) Statement {
	return s.Kw(keyword).Lit(value)
}

// KwInt appends a keyword followed by an integer, such as CONNECTION LIMIT 10.
func (s Statement) KwInt(keyword Keyword, value int64) Statement {
	return s.Kw(keyword).Int(value)
}

// KwQualified appends a keyword followed by a dotted name, such as ON TABLE "schema"."table".
func (s Statement) KwQualified(keyword Keyword, parts ...string) Statement {
	return s.Kw(keyword).Qualified(parts...)
}

// OptIdent appends keyword and a quoted identifier unless name is empty.
func (s Statement) OptIdent(keyword Keyword, name string) Statement {
	if name == "" {
		return s
	}
	return s.KwIdent(keyword, name)
}

// OptLit appends keyword and a string literal unless value is empty.
func (s Statement) OptLit(keyword Keyword, value string) Statement {
	if value == "" {
		return s
	}
	return s.KwLit(keyword, value)
}

// OptKw appends the keyword unless it is empty, for example an optional OneOf result.
func (s Statement) OptKw(keyword Keyword) Statement {
	return s.Kw(keyword)
}

// OptInt appends keyword and an integer unless value is nil.
func (s Statement) OptInt(keyword Keyword, value *int64) Statement {
	if value == nil {
		return s
	}
	return s.KwInt(keyword, *value)
}

// OptToggle appends ifTrue or ifFalse according to value, and nothing when value is nil.
func (s Statement) OptToggle(value *bool, ifTrue, ifFalse Keyword) Statement {
	if value == nil {
		return s
	}
	return s.Toggle(*value, ifTrue, ifFalse)
}

// If appends the keywords only when condition holds.
func (s Statement) If(condition bool, words ...Keyword) Statement {
	if !condition {
		return s
	}
	return s.Kw(words...)
}

// Toggle appends ifTrue or ifFalse, such as CREATEDB or NOCREATEDB.
func (s Statement) Toggle(value bool, ifTrue, ifFalse Keyword) Statement {
	if value {
		return s.Kw(ifTrue)
	}
	return s.Kw(ifFalse)
}

// When applies extend only when condition holds, for clauses with several parts.
func (s Statement) When(condition bool, extend func(Statement) Statement) Statement {
	if !condition {
		return s
	}
	return extend(s)
}

// joinItems renders nonempty items separated by commas and returns the first item error.
func joinItems(items []Statement) (string, error) {
	var rendered []string
	var err error
	for _, item := range items {
		if text := item.String(); text != "" {
			rendered = append(rendered, text)
		}
		if err == nil {
			err = item.err
		}
	}
	return strings.Join(rendered, ", "), err
}

// List appends the nonempty items separated by commas, such as a privilege list.
func (s Statement) List(items ...Statement) Statement {
	text, err := joinItems(items)
	out := s.with(text)
	if err != nil {
		return out.fail(err)
	}
	return out
}

// Paren appends the items as a parenthesized list after a space, such as a column list.
func (s Statement) Paren(items ...Statement) Statement {
	text, err := joinItems(items)
	out := s.with("(" + text + ")")
	if err != nil {
		return out.fail(err)
	}
	return out
}

// Args attaches the items as a parenthesized list to the previous token without a space, such as IDENTITY(1, 1).
func (s Statement) Args(items ...Statement) Statement {
	text, err := joinItems(items)
	group := "(" + text + ")"
	out := s.with()
	if last := len(out.tokens) - 1; last >= 0 {
		out.tokens[last] += group
	} else {
		out.tokens = append(out.tokens, group)
	}
	if err != nil {
		return out.fail(err)
	}
	return out
}

// OptParen appends a parenthesized list only when at least one item is nonempty.
func (s Statement) OptParen(items ...Statement) Statement {
	if text, err := joinItems(items); text == "" && err == nil {
		return s
	}
	return s.Paren(items...)
}

// OptArgs attaches a parenthesized list only when at least one item is nonempty.
func (s Statement) OptArgs(items ...Statement) Statement {
	if text, err := joinItems(items); text == "" && err == nil {
		return s
	}
	return s.Args(items...)
}

// Idents appends quoted identifiers separated by commas; wrap the result in Paren for a column list.
func (s Statement) Idents(names ...string) Statement {
	quoted := make([]string, len(names))
	for i, name := range names {
		quoted[i] = Identifier(name)
	}
	return s.with(strings.Join(quoted, ", "))
}
