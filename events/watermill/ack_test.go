package dogerwatermill_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/doger"
	dogerwatermill "github.com/kartaladev/doger/events/watermill"
)

var (
	errOutOfStock = errors.New("out of stock")
	errBadOrder   = errors.New("bad order")
)

// dogerMessage returns a message with UUID id carrying the doger metadata
// of an "orders.placed" envelope.
func dogerMessage(id string) *message.Message {
	msg := message.NewMessage(id, []byte(`{"ID":"1"}`))
	msg.Metadata = message.Metadata{
		"doger_event_type":   "orders.placed",
		"doger_version":      "1",
		"doger_content_type": "application/json",
		"doger_published_at": "2026-01-01T00:00:00Z",
	}

	return msg
}

// failingHandler fails its first failures calls with err, then returns
// nil; a negative failures fails every call. It counts its runs.
type failingHandler struct {
	failures int64
	err      error
	runs     atomic.Int64
}

func (h *failingHandler) handle(context.Context, doger.Envelope) error {
	n := h.runs.Add(1)
	if h.failures < 0 || n <= h.failures {
		return h.err
	}

	return nil
}

func TestAck(t *testing.T) {
	t.Parallel()

	poison := dogerwatermill.WithPoisonTopic("poison")
	tests := []struct {
		name       string
		opts       []dogerwatermill.Option
		failures   int64
		err        error
		runs       int64
		nacks      int
		wantLogged int
		wantPoison bool
	}{
		{name: "success", runs: 1},
		{name: "fails_twice_then_succeeds", failures: 2, err: errOutOfStock, runs: 3, nacks: 2},
		{name: "attempts_exhausted_logged", opts: []dogerwatermill.Option{dogerwatermill.WithMaxAttempts(3)}, failures: -1, err: errOutOfStock, runs: 3, nacks: 2, wantLogged: 1},
		{name: "attempts_exhausted_poison", opts: []dogerwatermill.Option{dogerwatermill.WithMaxAttempts(3), poison}, failures: -1, err: errOutOfStock, runs: 3, nacks: 2, wantPoison: true},
		{name: "permanent_logged", failures: -1, err: doger.PermanentError(errBadOrder), runs: 1, wantLogged: 1},
		{name: "permanent_poison", opts: []dogerwatermill.Option{poison}, failures: -1, err: doger.PermanentError(errBadOrder), runs: 1, wantPoison: true},
		{name: "unlimited_attempts", opts: []dogerwatermill.Option{dogerwatermill.WithMaxAttempts(0)}, failures: 20, err: errOutOfStock, runs: 21, nacks: 20},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake := newFakeSubscriber()
			pub := &recordingPublisher{}
			logger := watermill.NewCaptureLogger()
			h := &failingHandler{failures: tt.failures, err: tt.err}
			// no wait between attempts: the delay is TestAck_RetryDelay's
			opts := append([]dogerwatermill.Option{dogerwatermill.WithLogger(logger), dogerwatermill.WithRetryDelay(0)}, tt.opts...)
			transport, err := dogerwatermill.New(pub, (&countingFactory{sub: fake}).Build, opts...)
			require.NoError(t, err)
			stop, err := transport.Subscribe(context.Background(), mailSubscription, h.handle)
			require.NoError(t, err)
			defer stopNow(t, stop)

			nacks, acked := fake.deliverUntilAcked(dogerMessage("m-1"), 50, deliverTimeout)

			require.True(t, acked, "message acked")
			assert.Equal(t, tt.runs, h.runs.Load(), "handler runs")
			assert.Equal(t, tt.nacks, nacks, "nacks")
			assert.Equal(t, tt.wantLogged, errorsLogged(logger), "errors logged")
			got := pub.Published()
			if tt.wantPoison {
				require.Len(t, got, 1)
				assert.Equal(t, "poison", got[0].topic)
				assert.Equal(t, "m-1", got[0].msg.UUID)
			} else {
				assert.Empty(t, got)
			}
		})
	}
}

func TestAck_RetryDelay(t *testing.T) {
	t.Parallel()

	for _, delay := range []time.Duration{time.Second, 0, 30 * time.Second} {
		t.Run(delay.String(), func(t *testing.T) {
			t.Parallel()
			fake := newFakeSubscriber()
			clock := newFakeClock()
			h := &failingHandler{failures: 1, err: errOutOfStock}
			transport, err := dogerwatermill.New(&recordingPublisher{}, (&countingFactory{sub: fake}).Build,
				dogerwatermill.WithClock(clock), dogerwatermill.WithRetryDelay(delay))
			require.NoError(t, err)
			stop, err := transport.Subscribe(context.Background(), mailSubscription, h.handle)
			require.NoError(t, err)
			defer stopNow(t, stop)

			msg := dogerMessage("m-1")
			settled := make(chan bool, 1)
			go func() {
				acked, ok := fake.deliver(msg, deliverTimeout)
				settled <- ok && !acked
			}()

			if delay > 0 {
				select {
				case d := <-clock.Started:
					assert.Equal(t, delay, d, "scheduled delay")
				case <-time.After(deliverTimeout):
					require.FailNow(t, "no retry delay was scheduled")
				}
				select {
				case <-msg.Nacked():
					require.FailNow(t, "nacked before the clock advanced")
				case <-time.After(50 * time.Millisecond):
				}
				clock.Advance(delay - time.Nanosecond)
				select {
				case <-msg.Nacked():
					require.FailNow(t, "nacked before the full delay elapsed")
				case <-time.After(50 * time.Millisecond):
				}
				clock.Advance(time.Nanosecond)
			}
			assert.True(t, <-settled, "message nacked")
		})
	}
}

func TestAck_PoisonFailure(t *testing.T) {
	t.Parallel()

	fake := newFakeSubscriber()
	logger := watermill.NewCaptureLogger()
	pub := &recordingPublisher{publishFn: func(topic string, _ *message.Message) error {
		if topic == "poison" {
			return errBrokerDown
		}
		return nil
	}}
	transport, err := dogerwatermill.New(pub, (&countingFactory{sub: fake}).Build,
		dogerwatermill.WithLogger(logger), dogerwatermill.WithPoisonTopic("poison"))
	require.NoError(t, err)
	handler := func(context.Context, doger.Envelope) error { return doger.PermanentError(errBadOrder) }
	stop, err := transport.Subscribe(context.Background(), mailSubscription, handler)
	require.NoError(t, err)
	defer stopNow(t, stop)

	acked, settled := fake.deliver(dogerMessage("m-1"), deliverTimeout)

	require.True(t, settled, "message settled")
	assert.False(t, acked, "message nacked")
	assert.True(t, errorLogged(logger, errBrokerDown), "logged the publish error")
}

// failingPoison returns a publisher whose first failures publishes to the
// "poison" topic fail with errBrokerDown, and the counter of those publishes.
func failingPoison(failures int64) (*recordingPublisher, *atomic.Int64) {
	var poisonCalls atomic.Int64
	pub := &recordingPublisher{publishFn: func(topic string, _ *message.Message) error {
		if topic == "poison" && poisonCalls.Add(1) <= failures {
			return errBrokerDown
		}
		return nil
	}}

	return pub, &poisonCalls
}

func TestAck_PoisonFailureRetry(t *testing.T) {
	t.Parallel()

	t.Run("waits_the_retry_delay", func(t *testing.T) {
		t.Parallel()
		fake := newFakeSubscriber()
		clock := newFakeClock()
		pub, _ := failingPoison(1)
		transport, err := dogerwatermill.New(pub, (&countingFactory{sub: fake}).Build,
			dogerwatermill.WithLogger(watermill.NopLogger{}), dogerwatermill.WithPoisonTopic("poison"),
			dogerwatermill.WithClock(clock), dogerwatermill.WithRetryDelay(time.Second))
		require.NoError(t, err)
		handler := func(context.Context, doger.Envelope) error { return doger.PermanentError(errBadOrder) }
		stop, err := transport.Subscribe(context.Background(), mailSubscription, handler)
		require.NoError(t, err)
		defer stopNow(t, stop)

		msg := dogerMessage("m-1")
		settled := make(chan bool, 1)
		go func() {
			acked, ok := fake.deliver(msg, deliverTimeout)
			settled <- ok && !acked
		}()

		select {
		case <-msg.Nacked():
			require.FailNow(t, "nacked at once after the failed poison publish")
		case d := <-clock.Started:
			assert.Equal(t, time.Second, d, "scheduled delay")
		case <-time.After(deliverTimeout):
			require.FailNow(t, "neither nacked nor delayed")
		}
		clock.Advance(time.Second)
		assert.True(t, <-settled, "message nacked after the delay")
	})

	t.Run("handler_not_rerun", func(t *testing.T) {
		t.Parallel()
		fake := newFakeSubscriber()
		pub, poisonCalls := failingPoison(3)
		h := &failingHandler{failures: -1, err: errOutOfStock}
		transport, err := dogerwatermill.New(pub, (&countingFactory{sub: fake}).Build,
			dogerwatermill.WithLogger(watermill.NopLogger{}), dogerwatermill.WithPoisonTopic("poison"),
			dogerwatermill.WithRetryDelay(0), dogerwatermill.WithMaxAttempts(2))
		require.NoError(t, err)
		stop, err := transport.Subscribe(context.Background(), mailSubscription, h.handle)
		require.NoError(t, err)
		defer stopNow(t, stop)

		nacks, acked := fake.deliverUntilAcked(dogerMessage("m-1"), 50, deliverTimeout)

		require.True(t, acked, "message acked once poisoned")
		assert.Equal(t, int64(2), h.runs.Load(), "handler runs: the attempts, not rerun after giving up")
		assert.Equal(t, int64(4), poisonCalls.Load(), "poison publishes: 3 failed, then 1")
		assert.Equal(t, 4, nacks, "nacks: 1 retry, then 3 failed poison publishes")
		require.Len(t, pub.Published(), 1)
	})
}

func TestAck_HandlerPanic(t *testing.T) {
	t.Parallel()

	fake := newFakeSubscriber()
	logger := watermill.NewCaptureLogger()
	transport, err := dogerwatermill.New(&recordingPublisher{}, (&countingFactory{sub: fake}).Build,
		dogerwatermill.WithLogger(logger), dogerwatermill.WithRetryDelay(0), dogerwatermill.WithMaxAttempts(2))
	require.NoError(t, err)
	var runs atomic.Int64
	stop, err := transport.Subscribe(context.Background(), mailSubscription, func(context.Context, doger.Envelope) error {
		runs.Add(1)
		panic("kaboom")
	})
	require.NoError(t, err)
	defer stopNow(t, stop)

	nacks, acked := fake.deliverUntilAcked(dogerMessage("m-1"), 10, deliverTimeout)

	require.True(t, acked, "message given up on after the attempts")
	assert.Equal(t, 1, nacks, "nacks: a panic is an ordinary error")
	assert.Equal(t, int64(2), runs.Load(), "handler runs")
	require.Equal(t, 1, errorsLogged(logger))
	assert.ErrorContains(t, logger.Captured()[watermill.ErrorLogLevel][0].Err, "kaboom")
}

func TestAck_MaxTrackedMessages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		opts      []dogerwatermill.Option
		wantAcked bool // m-1's second failure exhausts its 2 attempts
	}{
		{name: "default_remembers_both", wantAcked: true},
		{name: "one_forgets_m1", opts: []dogerwatermill.Option{dogerwatermill.WithMaxTrackedMessages(1)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake := newFakeSubscriber()
			opts := append([]dogerwatermill.Option{
				dogerwatermill.WithLogger(watermill.NopLogger{}), dogerwatermill.WithRetryDelay(0), dogerwatermill.WithMaxAttempts(2),
			}, tt.opts...)
			transport, err := dogerwatermill.New(&recordingPublisher{}, (&countingFactory{sub: fake}).Build, opts...)
			require.NoError(t, err)
			h := &failingHandler{failures: -1, err: errOutOfStock}
			stop, err := transport.Subscribe(context.Background(), mailSubscription, h.handle)
			require.NoError(t, err)
			defer stopNow(t, stop)

			for _, id := range []string{"m-1", "m-2"} { // each fails once
				acked, settled := fake.deliver(dogerMessage(id), deliverTimeout)
				require.True(t, settled)
				require.False(t, acked)
			}
			acked, settled := fake.deliver(dogerMessage("m-1"), deliverTimeout)

			require.True(t, settled)
			assert.Equal(t, tt.wantAcked, acked)
		})
	}
}
