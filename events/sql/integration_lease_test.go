package eventsql_test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/doger"
	eventsql "github.com/kartaladev/doger/events/sql"
)

// logRecorder is a slog.Handler keeping every record's message and
// "error" attribute.
type logRecorder struct {
	mu      sync.Mutex
	records []loggedRecord
}

type loggedRecord struct {
	msg string
	err error
}

func (r *logRecorder) Enabled(context.Context, slog.Level) bool { return true }
func (r *logRecorder) WithAttrs([]slog.Attr) slog.Handler       { return r }
func (r *logRecorder) WithGroup(string) slog.Handler            { return r }

func (r *logRecorder) Handle(_ context.Context, rec slog.Record) error {
	lr := loggedRecord{msg: rec.Message}
	rec.Attrs(func(a slog.Attr) bool {
		if err, ok := a.Value.Any().(error); ok && a.Key == "error" {
			lr.err = err
		}
		return true
	})
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, lr)
	return nil
}

// Logged reports whether a record's error matches target.
func (r *logRecorder) Logged(target error) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, lr := range r.records {
		if errors.Is(lr.err, target) {
			return true
		}
	}
	return false
}

// Count returns how many records have message msg.
func (r *logRecorder) Count(msg string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, lr := range r.records {
		if lr.msg == msg {
			n++
		}
	}
	return n
}

// waitFor waits for cond, failing the test after the wait timeout.
func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(waitTimeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out: %s", msg)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// attempts records the attempt of every handled envelope.
type attempts struct {
	mu  sync.Mutex
	got []int
}

func (a *attempts) record(ctx context.Context) {
	n, _ := eventsql.AttemptFromContext(ctx)
	a.mu.Lock()
	defer a.mu.Unlock()
	a.got = append(a.got, n)
}

func (a *attempts) Got() []int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]int{}, a.got...)
}

// TestLease runs with a 200ms lease. No outcome it checks depends on how
// fast the machine is: a handler that must outlive its lease waits for
// what proves the lease ended, and a single worker keeps its member from
// claiming the delivery it is still settling.
func TestLease(t *testing.T) {
	const dest, lease = "orders.placed", 200 * time.Millisecond

	t.Run("crashed_member", func(t *testing.T) {
		t.Parallel()
		forEachEngine(t, func(t *testing.T, db *testDB) {
			testWithBackstop(t)
			tr := schemaTransport(t, db, fast(eventsql.Lease(lease))...)
			seedGroup(t, db, dest, "mail", false)
			send(t, tr, "e1", dest)
			// "i1" claimed e1 and crashed: its lease runs 200ms from now
			leaseInterval := "CURRENT_TIMESTAMP + INTERVAL '200 milliseconds'"
			if db.kind == "mysql" {
				leaseInterval = "UTC_TIMESTAMP(6) + INTERVAL 200000 MICROSECOND"
			}
			claimed := time.Now()
			db.Exec(t, "UPDATE doger_event_deliveries SET lease_owner = 'i1', lease_token = 'crashed', attempts = 1, lease_until = "+leaseInterval)

			got := &attempts{}
			c := newCollector()
			subscribe(t, tr, group(dest, "mail"), func(ctx context.Context, env doger.Envelope) error {
				got.record(ctx)
				return c.handle(ctx, env)
			})

			assert.Equal(t, []string{"e1"}, c.expect(t, 1))
			assert.GreaterOrEqual(t, time.Since(claimed), lease-50*time.Millisecond, "handled before the lease expired")
			assert.Equal(t, []int{2}, got.Got())
		})
	})

	t.Run("slow_handler", func(t *testing.T) {
		t.Parallel()
		forEachEngine(t, func(t *testing.T, db *testDB) {
			testWithBackstop(t)
			logs := &logRecorder{}
			// one worker, busy with the slow handler: i1 claims nothing
			// else meanwhile, so e1 goes to i2 once the lease expired
			i1 := schemaTransport(t, db, fast(eventsql.Lease(lease), eventsql.Workers(1), eventsql.Logger(slog.New(logs)))...)
			// i2 keeps the default 30s lease: it handles e1 once, until
			// i1's late acknowledgement came
			i2 := db.Transport(t, fast()...)
			seedGroup(t, db, dest, "mail", false)
			send(t, i1, "e1", dest)

			type ended struct {
				deadline time.Duration // from the handler's start
				hasDL    bool
				err      error
			}
			i1Ended := make(chan ended, 1)
			i2Claimed := make(chan struct{}) // closed once i2 runs e1
			claimedByI2 := sync.OnceFunc(func() { close(i2Claimed) })
			subscribe(t, i1, group(dest, "mail"), func(ctx context.Context, _ doger.Envelope) error {
				start := time.Now()
				dl, ok := ctx.Deadline()
				<-ctx.Done()
				i1Ended <- ended{deadline: dl.Sub(start), hasDL: ok, err: ctx.Err()}
				<-i2Claimed
				return nil // the late acknowledgement, once i2 holds the lease
			})
			t.Cleanup(claimedByI2) // before the stop of the cleanup, on a failure
			var e ended
			select {
			case e = <-i1Ended:
			case <-time.After(waitTimeout):
				t.Fatal("i1's handler context did not end")
			}
			require.ErrorIs(t, e.err, context.DeadlineExceeded)
			require.True(t, e.hasDL, "i1's handler context has no deadline")
			// the context ends before the lease does, so the handler is
			// cancelled before another member can claim the delivery
			assert.Less(t, e.deadline, lease)

			i2Handled := make(chan struct{})
			subscribe(t, i2, group(dest, "mail"), func(context.Context, doger.Envelope) error {
				claimedByI2()
				waitFor(t, func() bool { return logs.Logged(eventsql.ErrLeaseLost) }, "i1's late acknowledgement was not logged as ErrLeaseLost")
				assert.Equal(t, 1, db.Count(t, "SELECT COUNT(*) FROM doger_event_deliveries"), "i1's acknowledgement removed i2's claim")
				close(i2Handled)
				return nil
			})
			select {
			case <-i2Handled:
			case <-time.After(waitTimeout):
				t.Fatal("i2 did not handle e1")
			}
		})
	})

	// skipped_claims is a regression test: the claims of a batch that
	// waited for the busy worker past their lease were handed over with an
	// ended context, or skipped with their attempt still counted, so
	// their first run saw attempt 2. They are released, uncounted.
	t.Run("skipped_claims", func(t *testing.T) {
		t.Parallel()
		forEachEngine(t, func(t *testing.T, db *testDB) {
			testWithBackstop(t)
			tr := schemaTransport(t, db, fast(eventsql.Lease(lease), eventsql.Workers(1), eventsql.BatchSize(3))...)
			seedGroup(t, db, dest, "mail", false)
			for _, id := range []string{"e1", "e2", "e3"} {
				send(t, tr, id, dest)
			}

			// the first run is slow: the other claims of its batch wait
			// for the worker past their lease. It is usually e1, but
			// delivery is unordered: a claim can skip a row another
			// statement locks for a moment (MySQL)
			type run struct {
				attempt int
				// the handler's deadline came from a claim made after the
				// slow handler returned, not from the claim that waited
				claimedAfterSlow bool
			}
			var mu sync.Mutex
			runs := map[string][]run{}
			var slowID string
			var slowReturned time.Time
			c := newCollector()
			subscribe(t, tr, group(dest, "mail"), func(ctx context.Context, env doger.Envelope) error {
				n, _ := eventsql.AttemptFromContext(ctx)
				dl, _ := ctx.Deadline()
				mu.Lock()
				slow := slowID == ""
				if slow {
					slowID = env.ID
				}
				runs[env.ID] = append(runs[env.ID], run{attempt: n, claimedAfterSlow: !slowReturned.IsZero() && dl.After(slowReturned)})
				mu.Unlock()
				if slow {
					time.Sleep(lease + 100*time.Millisecond) // the other claims wait past their lease
					mu.Lock()
					slowReturned = time.Now()
					mu.Unlock()
				}
				return c.handle(ctx, env)
			})

			assert.ElementsMatch(t, []string{"e1", "e2", "e3"}, c.expect(t, 3))
			waitFor(t, func() bool { return db.Count(t, "SELECT COUNT(*) FROM doger_event_deliveries") == 0 }, "the deliveries were not removed")
			mu.Lock()
			defer mu.Unlock()
			for _, id := range []string{"e1", "e2", "e3"} {
				if id != slowID {
					assert.Equal(t, []run{{attempt: 1, claimedAfterSlow: true}}, runs[id], "runs of %s", id)
				}
			}
		})
	})

	t.Run("fast_handler", func(t *testing.T) {
		t.Parallel()
		forEachEngine(t, func(t *testing.T, db *testDB) {
			testWithBackstop(t)
			// one worker, busy until e1 is settled: i1 claims nothing
			// meanwhile, and i2 subscribes once e1 was removed, so the
			// acknowledgement holds the lease however late it comes
			i1 := schemaTransport(t, db, fast(eventsql.Lease(lease), eventsql.Workers(1))...)
			i2 := db.Transport(t, fast(eventsql.Lease(lease))...)
			seedGroup(t, db, dest, "mail", false)
			send(t, i1, "e1", dest)

			var i1Runs atomic.Int32
			handled := make(chan struct{})
			subscribe(t, i1, group(dest, "mail"), func(context.Context, doger.Envelope) error {
				if i1Runs.Add(1) == 1 {
					time.Sleep(50 * time.Millisecond)
					close(handled)
				}
				return nil
			})
			<-handled
			waitFor(t, func() bool { return db.Count(t, "SELECT COUNT(*) FROM doger_event_deliveries") == 0 }, "e1 was not removed")
			c := newCollector()
			subscribe(t, i2, group(dest, "mail"), c.handle)

			time.Sleep(lease) // past the lease
			c.quiet(t)
			assert.Equal(t, int32(1), i1Runs.Load(), "i1 handled e1 again")
			assert.Zero(t, db.Count(t, "SELECT COUNT(*) FROM doger_events"))
		})
	})
}

func TestLease_AttemptInContext(t *testing.T) {
	t.Parallel()
	forEachEngine(t, func(t *testing.T, db *testDB) {
		testWithBackstop(t)
		const dest = "orders.placed"
		tr := schemaTransport(t, db, fast(eventsql.MaxAttempts(5), eventsql.Backoff(eventsql.FixedBackoff(10*time.Millisecond)))...)
		got := &attempts{}
		done := make(chan struct{})
		subscribe(t, tr, group(dest, "mail"), func(ctx context.Context, _ doger.Envelope) error {
			got.record(ctx)
			if len(got.Got()) < 3 {
				return errOutOfStock
			}
			close(done)
			return nil
		})
		send(t, tr, "e1", dest)

		select {
		case <-done:
		case <-time.After(waitTimeout):
			t.Fatalf("attempts %v, want 3", got.Got())
		}
		assert.Equal(t, []int{1, 2, 3}, got.Got())
	})
}

// errOutOfStock is the transient handler error of the retry tests.
var errOutOfStock = errors.New("out of stock")
