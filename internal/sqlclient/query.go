package sqlclient

import (
	"fmt"
	"slices"
	"strings"
)

// Param binds one :name placeholder of a Query to a value.
type Param struct {
	// Name is the placeholder without the leading colon.
	Name string
	// Value is sent as a bound parameter, never as SQL text.
	Value string
}

// Bind returns the binding for :name.
func Bind(name, value string) Param {
	return Param{Name: name, Value: value}
}

// Query is an immutable catalog SELECT whose conditions carry their own bindings, so the emitted
// placeholders and the parameter map cannot drift apart.
type Query struct {
	// columns is the select list.
	columns []Keyword
	// source is the FROM clause, empty for a query without one.
	source Keyword
	// conditions are joined with AND.
	conditions []Keyword
	// order is the ORDER BY list.
	order []Keyword
	// params are the bindings attached by From and the conditions.
	params []Param
}

// Select starts a query with its select list.
func Select(columns ...Keyword) Query {
	return Query{columns: slices.Clone(columns)}
}

// From sets the source relation, with bindings for any placeholders it contains.
func (q Query) From(source Keyword, params ...Param) Query {
	out := q.clone()
	out.source = source
	out.params = append(out.params, params...)
	return out
}

// Where adds a condition joined with AND. A condition containing OR must parenthesize it.
func (q Query) Where(condition Keyword, params ...Param) Query {
	out := q.clone()
	out.conditions = append(out.conditions, condition)
	out.params = append(out.params, params...)
	return out
}

// WhereIf adds the condition only when include holds.
func (q Query) WhereIf(include bool, condition Keyword, params ...Param) Query {
	if !include {
		return q
	}
	return q.Where(condition, params...)
}

// WhereEither adds onTrue or onFalse according to choice. params may serve either branch; only those
// the chosen condition references are bound.
func (q Query) WhereEither(choice bool, onTrue, onFalse Keyword, params ...Param) Query {
	condition := onFalse
	if choice {
		condition = onTrue
	}
	used := placeholders(string(condition))
	var kept []Param
	for _, param := range params {
		if slices.Contains(used, param.Name) {
			kept = append(kept, param)
		}
	}
	return q.Where(condition, kept...)
}

// OptEq adds column = :name unless value is empty, for optional lookup filters.
func (q Query) OptEq(column Keyword, name, value string) Query {
	if value == "" {
		return q
	}
	return q.Where(column+" = :"+Keyword(name), Bind(name, value))
}

// OrderBy appends sort keys.
func (q Query) OrderBy(columns ...Keyword) Query {
	out := q.clone()
	out.order = append(out.order, columns...)
	return out
}

// clone copies every slice so derived queries never share storage.
func (q Query) clone() Query {
	return Query{
		columns:    slices.Clone(q.columns),
		source:     q.source,
		conditions: slices.Clone(q.conditions),
		order:      slices.Clone(q.order),
		params:     slices.Clone(q.params),
	}
}

// Build renders the query and its parameters. It fails when a placeholder has no binding, a binding is unused,
// one name is bound to different values, or a value is empty, which the Data API rejects. params is nil when
// the query has no placeholders.
func (q Query) Build() (string, map[string]string, error) {
	if len(q.columns) == 0 {
		return "", nil, fmt.Errorf("query has no select list")
	}
	var sql strings.Builder
	sql.WriteString("SELECT " + joinKeywords(q.columns, ", "))
	if q.source != "" {
		sql.WriteString(" FROM " + string(q.source))
	}
	if len(q.conditions) > 0 {
		if len(q.conditions) > 1 {
			for _, condition := range q.conditions {
				if hasTopLevelOr(string(condition)) {
					return "", nil, fmt.Errorf("query condition %q must parenthesize OR before it is joined with AND", condition)
				}
			}
		}
		sql.WriteString(" WHERE " + joinKeywords(q.conditions, " AND "))
	}
	if len(q.order) > 0 {
		sql.WriteString(" ORDER BY " + joinKeywords(q.order, ", "))
	}
	text := sql.String()
	var params map[string]string
	for _, param := range q.params {
		if param.Value == "" {
			return "", nil, fmt.Errorf("query parameter :%s is empty", param.Name)
		}
		if previous, found := params[param.Name]; found && previous != param.Value {
			return "", nil, fmt.Errorf("query parameter :%s is bound to different values", param.Name)
		}
		if params == nil {
			params = map[string]string{}
		}
		params[param.Name] = param.Value
	}
	used := placeholders(text)
	for _, name := range used {
		if _, found := params[name]; !found {
			return "", nil, fmt.Errorf("query placeholder :%s has no binding", name)
		}
	}
	for _, param := range q.params {
		if !slices.Contains(used, param.Name) {
			return "", nil, fmt.Errorf("query parameter :%s is not used", param.Name)
		}
	}
	return text, params, nil
}

// joinKeywords joins trusted SQL pieces with separator.
func joinKeywords(keywords []Keyword, separator string) string {
	parts := make([]string, len(keywords))
	for i, keyword := range keywords {
		parts[i] = string(keyword)
	}
	return strings.Join(parts, separator)
}

// hasTopLevelOr reports an OR outside parentheses, quotes and comments, which AND would bind more tightly than intended.
func hasTopLevelOr(condition string) bool {
	depth := 0
	for _, item := range lex(condition, transportReading) {
		if item.kind != lexCode {
			continue
		}
		for i := 0; i < len(item.text); i++ {
			switch char := item.text[i]; {
			case char == '(':
				depth++
			case char == ')':
				depth--
			case depth == 0 && identifierByte(char, true) && (i == 0 || !identifierByte(item.text[i-1], false)):
				end := i
				for end < len(item.text) && identifierByte(item.text[end], false) {
					end++
				}
				if strings.EqualFold(item.text[i:end], "OR") {
					return true
				}
				i = end - 1
			}
		}
	}
	return false
}
