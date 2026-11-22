package dogerwatermill_test

import (
	"context"
	"errors"
	"sync"
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
	errNoBroker = errors.New("no broker")
	errNoTopic  = errors.New("no topic")
)

// deliverTimeout bounds how long a fake waits for the consumer.
const deliverTimeout = 5 * time.Second

// mailSubscription is the subscription of the spec's subscribe scenarios.
var mailSubscription = doger.Subscription{Destination: "orders.placed", Group: "mail", Mode: doger.DeliverOnce}

// noopHandler acknowledges every envelope.
func noopHandler(context.Context, doger.Envelope) error { return nil }

// stopNow stops a subscription with a bounded context, failing t on error.
func stopNow(t *testing.T, stop func(context.Context) error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), deliverTimeout)
	defer cancel()
	require.NoError(t, stop(ctx))
}

// errorsLogged returns how many errors logger recorded.
func errorsLogged(logger *watermill.CaptureLoggerAdapter) int {
	return len(logger.Captured()[watermill.ErrorLogLevel])
}

// errorLogged reports whether logger recorded an error matching target.
// (CaptureLoggerAdapter.HasError compares the other way round, so it
// misses wrapped errors.)
func errorLogged(logger *watermill.CaptureLoggerAdapter, target error) bool {
	for _, msg := range logger.Captured()[watermill.ErrorLogLevel] {
		if errors.Is(msg.Err, target) {
			return true
		}
	}

	return false
}

func TestSubscribe(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		factoryErr error
		panics     bool
		nilSub     bool
		subErr     error
		wantErr    error
		wantClosed int64
	}{
		{name: "subscribed"},
		{name: "factory_error", factoryErr: errNoBroker, wantErr: errNoBroker},
		{name: "subscribe_error", subErr: errNoTopic, wantErr: errNoTopic, wantClosed: 1},
		{name: "nil_subscriber", nilSub: true, wantErr: dogerwatermill.ErrInvalidConfig},
		{name: "factory_panics", panics: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake := newFakeSubscriber()
			fake.subscribeErr = tt.subErr
			factory := &countingFactory{sub: fake, err: tt.factoryErr}
			if tt.nilSub {
				factory.sub = nil
			}
			build := factory.Build
			if tt.panics {
				build = func(context.Context, doger.Subscription) (message.Subscriber, error) { panic("kaboom") }
			}
			transport, err := dogerwatermill.New(&recordingPublisher{}, build)
			require.NoError(t, err)

			stop, err := transport.Subscribe(context.Background(), mailSubscription, noopHandler)

			if tt.panics {
				require.ErrorContains(t, err, "kaboom")
				assert.Nil(t, stop)
				return
			}
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, stop)
				assert.Equal(t, tt.wantClosed, fake.closes.Load(), "subscriber closes")
				return
			}
			require.NoError(t, err)
			require.NotNil(t, stop)
			assert.Equal(t, []doger.Subscription{mailSubscription}, factory.Received())
			assert.Equal(t, []string{"orders.placed"}, fake.Topics())
			stopNow(t, stop)
		})
	}
}

// envelopeRecorder records the envelopes a handler receives.
type envelopeRecorder struct {
	mu   sync.Mutex
	envs []doger.Envelope
	got  chan struct{}
}

func newEnvelopeRecorder() *envelopeRecorder {
	return &envelopeRecorder{got: make(chan struct{}, 1024)}
}

func (r *envelopeRecorder) handle(_ context.Context, env doger.Envelope) error {
	r.mu.Lock()
	r.envs = append(r.envs, env)
	r.mu.Unlock()
	r.got <- struct{}{}

	return nil
}

// wait waits for n envelopes, then returns all recorded ones.
func (r *envelopeRecorder) wait(t *testing.T, n int) []doger.Envelope {
	t.Helper()
	timer := time.NewTimer(deliverTimeout)
	defer timer.Stop()
	for range n {
		select {
		case <-r.got:
		case <-timer.C:
			t.Fatalf("received fewer than %d envelopes within %v", n, deliverTimeout)
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]doger.Envelope(nil), r.envs...)
}

func TestRoundTrip(t *testing.T) {
	t.Parallel()

	withHeaders := orderEnvelope()
	withHeaders.Headers = map[string]string{"trace": "t-9", "tenant": "acme"}
	noHeaders := orderEnvelope()
	noHeaders.ID = "id-2"
	noHeaders.Headers = nil

	tests := []struct {
		name string
		env  doger.Envelope
	}{
		{name: "headers", env: withHeaders},
		{name: "no_headers", env: noHeaders},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			broker := newFakeBroker()
			transport, err := dogerwatermill.New(broker, broker.Factory)
			require.NoError(t, err)
			rec := newEnvelopeRecorder()
			stop, err := transport.Subscribe(context.Background(), mailSubscription, rec.handle)
			require.NoError(t, err)
			defer stopNow(t, stop)

			require.NoError(t, transport.Send(context.Background(), tt.env))

			got := rec.wait(t, 1)
			require.Len(t, got, 1)
			assert.Equal(t, tt.env, got[0])
		})
	}
}

func TestForeignMessages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		opts       []dogerwatermill.Option
		wantPoison bool
		wantLogged int
	}{
		{name: "dropped", wantLogged: 1},
		{name: "poison_topic", opts: []dogerwatermill.Option{dogerwatermill.WithPoisonTopic("poison")}, wantPoison: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake := newFakeSubscriber()
			pub := &recordingPublisher{}
			logger := watermill.NewCaptureLogger()
			var calls atomic.Int64
			handler := func(context.Context, doger.Envelope) error {
				calls.Add(1)
				return nil
			}
			transport, err := dogerwatermill.New(pub, (&countingFactory{sub: fake}).Build,
				append([]dogerwatermill.Option{dogerwatermill.WithLogger(logger)}, tt.opts...)...)
			require.NoError(t, err)
			stop, err := transport.Subscribe(context.Background(), mailSubscription, handler)
			require.NoError(t, err)
			defer stopNow(t, stop)

			acked, settled := fake.deliver(message.NewMessage("m-1", []byte("raw")), deliverTimeout)

			require.True(t, settled, "message settled")
			assert.True(t, acked, "message acked")
			assert.Zero(t, calls.Load(), "handler calls")
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

func TestSubscribe_Rejected(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		sub     doger.Subscription
		handler doger.EnvelopeHandler
	}{
		{name: "no_group", sub: doger.Subscription{Destination: "orders.placed", Mode: doger.DeliverOnce}, handler: noopHandler},
		{name: "no_destination", sub: doger.Subscription{Group: "mail", Mode: doger.DeliverOnce}, handler: noopHandler},
		{name: "nil_handler", sub: mailSubscription},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			factory := &countingFactory{sub: newFakeSubscriber()}
			transport, err := dogerwatermill.New(&recordingPublisher{}, factory.Build)
			require.NoError(t, err)

			stop, err := transport.Subscribe(context.Background(), tt.sub, tt.handler)

			if stop != nil {
				stopNow(t, stop)
			}
			require.ErrorIs(t, err, doger.ErrSubscriptionNotSupported)
			assert.Nil(t, stop)
			assert.Zero(t, factory.calls.Load(), "factory calls")
		})
	}
}

func TestSubscribe_ChannelClosed(t *testing.T) {
	t.Parallel()

	fake := newFakeSubscriber()
	logger := watermill.NewCaptureLogger()
	transport, err := dogerwatermill.New(&recordingPublisher{}, (&countingFactory{sub: fake}).Build, dogerwatermill.WithLogger(logger))
	require.NoError(t, err)
	stop, err := transport.Subscribe(context.Background(), mailSubscription, noopHandler)
	require.NoError(t, err)
	defer stopNow(t, stop)

	close(fake.out) // the backend gave up on its own

	assert.Eventually(t, func() bool { return errorsLogged(logger) == 1 }, deliverTimeout, 10*time.Millisecond,
		"the closed message channel was logged")
}
