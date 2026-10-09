package sqlclient

import (
	"context"
	"slices"
	"strings"
)

// serializedClient prevents overlapping catalog writes through a shared provider connection.
type serializedClient struct {
	// client is the underlying Data API or direct SQL transport.
	client Client
	// mutation permits one autocommit catalog write at a time across databases.
	mutation chan struct{}
}

// observationVerbs lead the statements that only read the catalog.
var observationVerbs = []string{"SELECT", "SHOW", "DESC"}

// ObservationVerbs returns the leading keywords of statements that SerializeMutations lets run concurrently, so
// tests that check statement shapes share the list instead of keeping a copy that can drift.
func ObservationVerbs() []string {
	return slices.Clone(observationVerbs)
}

// SerializeMutations serializes writes while allowing SELECT, SHOW and DESC observations to run concurrently.
// Redshift catalog writes can conflict even when they target different grant tuples.
// The gate covers statement completion, including Data API polling, without retrying mutations.
func SerializeMutations(client Client) Client {
	return &serializedClient{client: client, mutation: make(chan struct{}, 1)}
}

// Query waits for exclusive mutation access with cancellation before executing non-observational SQL.
func (c *serializedClient) Query(ctx context.Context, target Connection, sql string, parameters map[string]string) ([]Row, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fields := strings.Fields(sql)
	if len(fields) > 0 && slices.ContainsFunc(observationVerbs, func(verb string) bool { return strings.EqualFold(fields[0], verb) }) {
		return c.client.Query(ctx, target, sql, parameters)
	}
	select {
	case c.mutation <- struct{}{}:
		defer func() { <-c.mutation }()
		return c.client.Query(ctx, target, sql, parameters)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
