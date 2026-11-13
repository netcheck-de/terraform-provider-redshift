package sqlclient

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
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
	synctest.Test(t, func(t *testing.T) {
		var active, completed atomic.Int32

		client := SerializeMutations(queryFunc(func(context.Context, Connection, string, map[string]string) ([]Row, error) {
			assert.Equal(t, int32(1), active.Add(1))
			time.Sleep(time.Second)
			active.Add(-1)
			completed.Add(1)

			return nil, nil
		}))

		var workers sync.WaitGroup

		started := time.Now()

		for range 20 {
			workers.Go(func() {
				_, err := client.Query(context.Background(), Connection{Database: "analytics"}, "GRANT USAGE ON DATABASE analytics TO ROLE readers", nil)
				assert.NoError(t, err)
			})
		}

		workers.Wait()
		assert.Equal(t, int32(20), completed.Load())
		assert.Equal(t, 20*time.Second, time.Since(started))
	})
}

// TestSerializedMutationsAllowReadsAndCancelledWaiters checks parallel reads and cancellation without issuing SQL.
func TestSerializedMutationsAllowReadsAndCancelledWaiters(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
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

		defer func() { close(release); <-done }()

		for _, sql := range []string{" SELECT name", "show grants", "DESC DATASHARE sales", "desc identity provider oidc"} {
			rows, err := client.Query(context.Background(), Connection{Database: "admin"}, sql, map[string]string{"name": "reader"})
			require.NoError(t, err)
			assert.Equal(t, []Row{{"name": "reader"}}, rows)
		}

		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()

		waitStarted := time.Now()
		_, err := client.Query(ctx, Connection{}, "REVOKE", nil)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Equal(t, time.Minute, time.Since(waitStarted))

		_, err = client.Query(ctx, Connection{}, "REVOKE", nil)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Equal(t, time.Minute, time.Since(waitStarted))
	})
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

// TestSerializedMutationsStopAfterUnsettledMutation keeps a writer queued behind a statement that may still be running,
// and every later writer, from overlapping it, while observations keep working.
func TestSerializedMutationsStopAfterUnsettledMutation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		started, release := make(chan struct{}), make(chan struct{})
		unsettled := fmt.Errorf("cancel statement: %w", ErrUnsettled)

		var mutations atomic.Int32

		client := SerializeMutations(queryFunc(func(_ context.Context, _ Connection, sql string, _ map[string]string) ([]Row, error) {
			if sql == "SELECT 1" {
				return []Row{{"value": "1"}}, nil
			}

			mutations.Add(1)
			close(started)
			<-release

			return nil, unsettled
		}))

		var workers sync.WaitGroup
		workers.Go(func() {
			_, err := client.Query(context.Background(), Connection{}, "ALTER TABLE sales", nil)
			assert.ErrorIs(t, err, ErrUnsettled)
		})
		<-started
		workers.Go(func() {
			_, err := client.Query(context.Background(), Connection{}, "GRANT", nil)
			if assert.ErrorIs(t, err, ErrUnsettled) {
				assert.Contains(t, err.Error(), "refusing to send another change")
			}
		})
		synctest.Wait()
		close(release)
		workers.Wait()

		_, err := client.Query(context.Background(), Connection{}, "REVOKE", nil)
		require.ErrorIs(t, err, unsettled)
		rows, err := client.Query(context.Background(), Connection{}, "SELECT 1", nil)
		require.NoError(t, err)
		assert.Equal(t, []Row{{"value": "1"}}, rows)
		assert.Equal(t, int32(1), mutations.Load())
	})
}

// TestObservationVerbsAreACopy keeps callers from changing which statements bypass the mutation gate.
func TestObservationVerbsAreACopy(t *testing.T) {
	verbs := ObservationVerbs()
	verbs[0] = "DROP"

	assert.Equal(t, []string{"SELECT", "SHOW", "DESC"}, ObservationVerbs())
}
