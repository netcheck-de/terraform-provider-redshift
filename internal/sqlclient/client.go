// Package sqlclient defines the transport-independent Redshift SQL contract.
package sqlclient

import (
	"context"
	"strings"
)

// Connection selects a database within the client's configured warehouse.
type Connection struct {
	// Database selects the local database in which the statement executes.
	Database string
}

// Row contains column names and textual values; SQL NULL is represented by an empty string.
type Row map[string]string

// Client executes each statement in autocommit mode and waits for completion.
// Implementations bind named parameters safely, honor cancellation, and return
// textual results. Warehouse routing, authentication, and connections belong to
// the implementation. Construction must not connect to the database.
type Client interface {
	// Query binds named parameters and returns all textual rows after statement completion.
	Query(context.Context, Connection, string, map[string]string) ([]Row, error)
}

// Identifier quotes a Redshift SQL identifier.
func Identifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

// Literal quotes a Redshift SQL string literal.
func Literal(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
