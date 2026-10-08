package sqlclient

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// queryFunc adapts an inline transport for concurrency tests.
type queryFunc func(context.Context, Connection, string, map[string]string) ([]Row, error)

// Query executes the injected transport behavior.
func (f queryFunc) Query(ctx context.Context, target Connection, sql string, parameters map[string]string) ([]Row, error) {
	return f(ctx, target, sql, parameters)
}

// TestSerializedMutationsDoNotOverlap exercises concurrent grants routed to different databases.
func TestSerializedMutationsDoNotOverlap(t *testing.T) {
	var active, completed atomic.Int32
	client := SerializeMutations(queryFunc(func(context.Context, Connection, string, map[string]string) ([]Row, error) {
		assert.Equal(t, int32(1), active.Add(1))
		time.Sleep(time.Millisecond)
		active.Add(-1)
		completed.Add(1)
		return nil, nil
	}))
	var workers sync.WaitGroup
	for range 20 {
		workers.Go(func() {
			_, err := client.Query(context.Background(), Connection{Database: "analytics"}, "GRANT USAGE ON DATABASE analytics TO ROLE readers", nil)
			assert.NoError(t, err)
		})
	}
	workers.Wait()
	assert.Equal(t, int32(20), completed.Load())
}

// TestSerializedMutationsAllowReadsAndCancelledWaiters checks parallel reads and cancellation without issuing SQL.
func TestSerializedMutationsAllowReadsAndCancelledWaiters(t *testing.T) {
	started, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	client := SerializeMutations(queryFunc(func(_ context.Context, target Connection, sql string, parameters map[string]string) ([]Row, error) {
		if sql == "GRANT" {
			close(started)
			<-release
			return nil, nil
		}
		assert.Equal(t, "admin", target.Database)
		assert.Equal(t, "reader", parameters["name"])
		return []Row{{"name": "reader"}}, nil
	}))
	go func() {
		defer close(done)
		_, err := client.Query(context.Background(), Connection{}, "GRANT", nil)
		assert.NoError(t, err)
	}()
	<-started
	t.Cleanup(func() { close(release); <-done })
	for _, sql := range []string{" SELECT name", "show grants"} {
		rows, err := client.Query(context.Background(), Connection{Database: "admin"}, sql, map[string]string{"name": "reader"})
		require.NoError(t, err)
		assert.Equal(t, []Row{{"name": "reader"}}, rows)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err := client.Query(ctx, Connection{}, "REVOKE", nil)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	_, err = client.Query(ctx, Connection{}, "REVOKE", nil)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

// TestSerializedMutationsReleaseAfterErrors ensures transport failures neither retain the gate nor trigger a retry.
func TestSerializedMutationsReleaseAfterErrors(t *testing.T) {
	calls := 0
	failure := errors.New("catalog conflict")
	client := SerializeMutations(queryFunc(func(context.Context, Connection, string, map[string]string) ([]Row, error) {
		calls++
		return nil, failure
	}))
	for _, sql := range []string{"GRANT", ""} {
		_, err := client.Query(context.Background(), Connection{}, sql, nil)
		require.ErrorIs(t, err, failure)
	}
	assert.Equal(t, 2, calls)
}
