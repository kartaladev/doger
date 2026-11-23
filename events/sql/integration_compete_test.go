package eventsql_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kartaladev/doger"
	eventsql "github.com/kartaladev/doger/events/sql"
)

func TestCompete_TwoInstances(t *testing.T) {
	t.Parallel()
	forEachEngine(t, func(t *testing.T, db *testDB) {
		// 200 events take over a minute on a loaded machine: the test
		// fails on a stall of its sends and handlers, not on its length
		testWithBackstop(t)
		progress := watchProgress(t)
		const dest, n = "orders.placed", 200
		i1 := schemaTransport(t, db, fast()...)
		i2 := db.Transport(t, fast()...)

		var (
			mu       sync.Mutex
			inFlight = map[string]bool{}
			overlaps []string
		)
		mail := newCollector()
		mailHandler := func(ctx context.Context, env doger.Envelope) error {
			progress()
			mu.Lock()
			if inFlight[env.ID] {
				overlaps = append(overlaps, env.ID)
			}
			inFlight[env.ID] = true
			mu.Unlock()
			time.Sleep(time.Millisecond)
			mu.Lock()
			delete(inFlight, env.ID)
			mu.Unlock()
			return mail.handle(ctx, env)
		}
		cache1, cache2 := newCollector(), newCollector()
		withProgress := func(h doger.EnvelopeHandler) doger.EnvelopeHandler {
			return func(ctx context.Context, env doger.Envelope) error {
				progress()
				return h(ctx, env)
			}
		}
		subscribe(t, i1, group(dest, "mail"), mailHandler)
		subscribe(t, i2, group(dest, "mail"), mailHandler)
		subscribe(t, i1, doger.Subscription{Destination: dest, Group: "cache/i1", Mode: doger.DeliverAll}, withProgress(cache1.handle))
		subscribe(t, i2, doger.Subscription{Destination: dest, Group: "cache/i2", Mode: doger.DeliverAll}, withProgress(cache2.handle))

		for i := range n {
			send(t, i1, fmt.Sprintf("e%03d", i), dest)
			progress()
		}

		got := mail.expect(t, n)
		mail.quiet(t)
		assert.Len(t, distinct(got), n, "each event handled once by the DeliverOnce group")
		assert.Len(t, distinct(cache1.expect(t, n)), n)
		assert.Len(t, distinct(cache2.expect(t, n)), n)
		cache1.quiet(t)
		mu.Lock()
		assert.Empty(t, overlaps, "events handled by two members at once")
		mu.Unlock()
	})
}

// distinct returns the distinct values of ids.
func distinct(ids []string) map[string]bool {
	set := map[string]bool{}
	for _, id := range ids {
		set[id] = true
	}
	return set
}

func TestCompete_Workers(t *testing.T) {
	t.Parallel()

	for _, workers := range []int{1, 4, 16} {
		t.Run(fmt.Sprintf("workers_%d", workers), func(t *testing.T) {
			t.Parallel()
			forEachEngine(t, func(t *testing.T, db *testDB) {
				testWithBackstop(t)
				progress := watchProgress(t) // a stall fails it, not its length
				const dest, n = "orders.placed", 64
				tr := schemaTransport(t, db, fast(eventsql.Workers(workers), eventsql.BatchSize(16))...)
				var running, peak atomic.Int32
				c := newCollector()
				subscribe(t, tr, group(dest, "mail"), func(ctx context.Context, env doger.Envelope) error {
					progress()
					now := running.Add(1)
					for {
						old := peak.Load()
						if now <= old || peak.CompareAndSwap(old, now) {
							break
						}
					}
					time.Sleep(20 * time.Millisecond)
					running.Add(-1)
					return c.handle(ctx, env)
				})

				for i := range n {
					send(t, tr, fmt.Sprintf("e%02d", i), dest)
					progress()
				}

				assert.Len(t, distinct(c.expect(t, n)), n)
				assert.LessOrEqual(t, int(peak.Load()), workers)
			})
		})
	}
}
