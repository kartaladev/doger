package dogerwatermill_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/kartaladev/doger"
	dogerwatermill "github.com/kartaladev/doger/events/watermill"
)

var errClose = errors.New("close failed")

// closeOnce closes ch unless it is already closed.
func closeOnce(once *sync.Once, ch chan struct{}) { once.Do(func() { close(ch) }) }

// subscribeFake subscribes a transport over fake with h.
func subscribeFake(t *testing.T, fake *fakeSubscriber, ctx context.Context, h doger.EnvelopeHandler) func(context.Context) error {
	t.Helper()
	transport, err := dogerwatermill.New(&recordingPublisher{}, (&countingFactory{sub: fake}).Build)
	require.NoError(t, err)
	stop, err := transport.Subscribe(ctx, mailSubscription, h)
	require.NoError(t, err)

	return stop
}

func TestStop_WaitsHandler(t *testing.T) {
	testWithTimeout(t, 30*time.Second)
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	fake := newFakeSubscriber()
	started, release := make(chan struct{}), make(chan struct{})
	var returned atomic.Bool
	stop := subscribeFake(t, fake, context.Background(), func(context.Context, doger.Envelope) error {
		close(started)
		<-release
		returned.Store(true)
		return nil
	})
	go fake.deliver(dogerMessage("m-1"), deliverTimeout)
	<-started

	time.AfterFunc(50*time.Millisecond, func() { close(release) })
	err := stop(context.Background())

	require.NoError(t, err)
	assert.True(t, returned.Load(), "stop returned after the handler returned")
	assert.Equal(t, int64(1), fake.closes.Load(), "subscriber closes")
}

func TestStop_Deadline(t *testing.T) {
	testWithTimeout(t, 30*time.Second)

	fake := newFakeSubscriber()
	started, cancelled, giveUp := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var giveUpOnce sync.Once
	defer closeOnce(&giveUpOnce, giveUp)
	stop := subscribeFake(t, fake, context.Background(), func(ctx context.Context, _ doger.Envelope) error {
		close(started)
		select {
		case <-ctx.Done():
			close(cancelled)
		case <-giveUp: // the test failed; let the loop exit
		}
		return errOutOfStock
	})
	go fake.deliver(dogerMessage("m-1"), deliverTimeout)
	<-started

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	stopped := make(chan error, 1)
	go func() { stopped <- stop(ctx) }()

	select {
	case err := <-stopped:
		require.ErrorIs(t, err, context.DeadlineExceeded)
	case <-time.After(time.Second):
		closeOnce(&giveUpOnce, giveUp)
		<-stopped
		t.Fatal("stop did not return within 1s of its deadline")
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Error("the handler context was not cancelled")
	}
	assert.Equal(t, int64(1), fake.closes.Load(), "subscriber closes")
}

func TestStop_Edges(t *testing.T) {
	t.Run("stop_twice", func(t *testing.T) {
		defer goleak.VerifyNone(t, goleak.IgnoreCurrent())
		fake := newFakeSubscriber()
		stop := subscribeFake(t, fake, context.Background(), noopHandler)

		require.NoError(t, stop(context.Background()))
		require.NoError(t, stop(context.Background()))
		assert.Equal(t, int64(1), fake.closes.Load(), "subscriber closes")
	})
	t.Run("subscribe_context_cancelled", func(t *testing.T) {
		defer goleak.VerifyNone(t, goleak.IgnoreCurrent())
		fake := newFakeSubscriber()
		ctx, cancel := context.WithCancel(context.Background())
		stop := subscribeFake(t, fake, ctx, noopHandler)

		cancel()

		time.Sleep(100 * time.Millisecond) // let the loop see the cancellation
		_, settled := fake.deliver(dogerMessage("m-1"), time.Second)
		assert.False(t, settled, "a message was consumed after the cancellation")
		require.NoError(t, stop(context.Background()))
	})
	t.Run("closes_before_cancelling", func(t *testing.T) {
		fake := newFakeSubscriber()
		stop := subscribeFake(t, fake, context.Background(), noopHandler)

		require.NoError(t, stop(context.Background()))

		// a subscriber whose context is cancelled first logs an aborted
		// fetch (watermill-sql: "context canceled") on every clean stop
		fake.mu.Lock()
		defer fake.mu.Unlock()
		assert.NoError(t, fake.ctxAtClose, "the Subscribe context was cancelled before Close")
	})
	t.Run("close_error", func(t *testing.T) {
		fake := newFakeSubscriber()
		fake.closeErr = errClose
		stop := subscribeFake(t, fake, context.Background(), noopHandler)

		require.ErrorIs(t, stop(context.Background()), errClose)
	})
	t.Run("close_panics", func(t *testing.T) {
		fake := newFakeSubscriber()
		fake.closePanic = "kaboom"
		stop := subscribeFake(t, fake, context.Background(), noopHandler)

		assert.ErrorContains(t, stop(context.Background()), "kaboom")
	})
}
