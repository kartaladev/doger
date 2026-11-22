package dogerwatermill_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/ThreeDotsLabs/watermill/message"

	"github.com/kartaladev/doger"
	dogerwatermill "github.com/kartaladev/doger/events/watermill"
)

func TestNew(t *testing.T) {
	t.Parallel()

	pub := &recordingPublisher{}
	subs := (&countingFactory{}).Build

	tests := []struct {
		name    string
		pub     bool
		subs    bool
		opts    []dogerwatermill.Option
		wantErr bool
	}{
		{name: "defaults", pub: true, subs: true},
		{name: "nil_publisher", subs: true, wantErr: true},
		{name: "nil_factory", pub: true, wantErr: true},
		{name: "negative_retry_delay", pub: true, subs: true, opts: []dogerwatermill.Option{dogerwatermill.WithRetryDelay(-time.Second)}, wantErr: true},
		{name: "negative_max_attempts", pub: true, subs: true, opts: []dogerwatermill.Option{dogerwatermill.WithMaxAttempts(-1)}, wantErr: true},
		{name: "empty_metadata_prefix", pub: true, subs: true, opts: []dogerwatermill.Option{dogerwatermill.WithMetadataPrefix("")}, wantErr: true},
		{name: "zero_delay_and_unlimited_attempts", pub: true, subs: true, opts: []dogerwatermill.Option{dogerwatermill.WithRetryDelay(0), dogerwatermill.WithMaxAttempts(0)}},
		{name: "zero_max_tracked", pub: true, subs: true, opts: []dogerwatermill.Option{dogerwatermill.WithMaxTrackedMessages(0)}, wantErr: true},
		{name: "one_max_tracked", pub: true, subs: true, opts: []dogerwatermill.Option{dogerwatermill.WithMaxTrackedMessages(1)}},
		{name: "nil_option_ignored", pub: true, subs: true, opts: []dogerwatermill.Option{nil, dogerwatermill.WithMaxAttempts(3)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var (
				p message.Publisher
				f dogerwatermill.SubscriberFactory
			)
			if tt.pub {
				p = pub
			}
			if tt.subs {
				f = subs
			}

			transport, err := dogerwatermill.New(p, f, tt.opts...)

			if tt.wantErr {
				require.ErrorIs(t, err, dogerwatermill.ErrInvalidConfig)
				assert.Nil(t, transport)
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, transport)
		})
	}
}

func TestNew_Passive(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	pub := &recordingPublisher{}
	factory := &countingFactory{}

	transport, err := dogerwatermill.New(pub, factory.Build)

	require.NoError(t, err)
	var _ doger.EventTransport = transport
	assert.Zero(t, pub.calls.Load(), "publisher calls")
	assert.Zero(t, factory.calls.Load(), "factory calls")
}

func TestConcurrent(t *testing.T) {
	testWithTimeout(t, time.Minute)

	const (
		topics    = 10
		senders   = 50
		perSender = 20
		total     = senders * perSender
	)
	broker := newFakeBroker()
	transport, err := dogerwatermill.New(broker, broker.Factory)
	require.NoError(t, err)

	var (
		mu   sync.Mutex
		seen = map[string]int{}
		got  = make(chan struct{}, total*2)
	)
	handler := func(_ context.Context, env doger.Envelope) error {
		mu.Lock()
		seen[env.ID]++
		mu.Unlock()
		got <- struct{}{}
		return nil
	}
	stops := make([]func(context.Context) error, 0, topics)
	for i := range topics {
		stop, err := transport.Subscribe(context.Background(),
			doger.Subscription{Destination: fmt.Sprintf("topic-%d", i), Group: "g", Mode: doger.DeliverOnce}, handler)
		require.NoError(t, err)
		stops = append(stops, stop)
	}

	var wg sync.WaitGroup
	for s := range senders {
		wg.Go(func() {
			for n := range perSender {
				env := orderEnvelope()
				env.ID = fmt.Sprintf("%d-%d", s, n)
				env.Destination = fmt.Sprintf("topic-%d", (s+n)%topics)
				assert.NoError(t, transport.Send(context.Background(), env))
			}
		})
	}
	wg.Wait()
	for range total {
		select {
		case <-got:
		case <-time.After(deliverTimeout):
			t.Fatal("not every envelope was delivered")
		}
	}
	for _, stop := range stops {
		stopNow(t, stop)
	}

	mu.Lock()
	defer mu.Unlock()
	assert.Len(t, seen, total, "distinct envelopes")
	for id, n := range seen {
		assert.Equal(t, 1, n, "deliveries of %s", id)
	}
}
