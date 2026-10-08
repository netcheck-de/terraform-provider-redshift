// Package redshiftconn executes transport-neutral Redshift SQL over verified TLS PostgreSQL connections.
package redshiftconn

import (
	"fmt"
	"strconv"
	"strings"
)

// identifierByte recognizes ASCII parameter names and dollar-quote tag characters.
func identifierByte(value byte, first bool) bool {
	return value == '_' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || !first && value >= '0' && value <= '9'
}

// bindParameters converts named placeholders to positional values while preserving SQL lexical constructs.
func bindParameters(sql string, parameters map[string]string) (string, []any, error) {
	var output strings.Builder
	var arguments []any
	positions := map[string]int{}
	for offset := 0; offset < len(sql); {
		start := offset
		switch {
		case sql[offset] == '\'' || sql[offset] == '"':
			quote := sql[offset]
			escaped := quote == '\'' && start > 0 && (sql[start-1] == 'E' || sql[start-1] == 'e') && (start == 1 || !identifierByte(sql[start-2], false))
			offset++
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
					break
				}
				offset++
			}
		case strings.HasPrefix(sql[offset:], "--"):
			if end := strings.IndexByte(sql[offset:], '\n'); end >= 0 {
				offset += end + 1
			} else {
				offset = len(sql)
			}
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
		case sql[offset] == '$':
			end := offset + 1
			for end < len(sql) && identifierByte(sql[end], end == offset+1) {
				end++
			}
			if end < len(sql) && sql[end] == '$' {
				delimiter := sql[offset : end+1]
				if closeAt := strings.Index(sql[end+1:], delimiter); closeAt >= 0 {
					offset = end + 1 + closeAt + len(delimiter)
				} else {
					offset = len(sql)
				}
			} else {
				offset++
			}
		case strings.HasPrefix(sql[offset:], "::") || strings.HasPrefix(sql[offset:], ":="):
			offset += 2
		case sql[offset] == ':' && offset+1 < len(sql) && identifierByte(sql[offset+1], true):
			end := offset + 2
			for end < len(sql) && identifierByte(sql[end], false) {
				end++
			}
			name := sql[offset+1 : end]
			value, ok := parameters[name]
			if !ok {
				return "", nil, fmt.Errorf("missing SQL parameter %q", name)
			}
			position, used := positions[name]
			if !used {
				arguments = append(arguments, value)
				position = len(arguments)
				positions[name] = position
			}
			output.WriteByte('$')
			output.WriteString(strconv.Itoa(position))
			offset = end
			continue
		default:
			offset++
		}
		output.WriteString(sql[start:offset])
	}
	for name := range parameters {
		if _, used := positions[name]; !used {
			return "", nil, fmt.Errorf("unused SQL parameter %q", name)
		}
	}
	return output.String(), arguments, nil
}
