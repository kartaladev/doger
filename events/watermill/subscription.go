package dogerwatermill

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ThreeDotsLabs/watermill/message"

	"github.com/kartaladev/doger"
)

// subscription is the state of one consuming loop: the subscriber the
// factory built for sub, its message channel, and what stops the loop.
type subscription struct {
	transport  *Transport
	sub        doger.Subscription
	handler    doger.EnvelopeHandler
	subscriber message.Subscriber
	messages   <-chan *message.Message
	subCtx     context.Context    //nolint:containedctx // the parent of the handler contexts
	cancelSub  context.CancelFunc // cancels subCtx: the handler contexts and the subscriber's
	stopLoop   context.CancelFunc // stops consuming new messages
	done       chan struct{}      // closed when run returns
	stopOnce   sync.Once

	attempts map[string]int // handler runs per message UUID; owned by run
}

// run consumes the messages until ctx is done or the channel closes.
func (s *subscription) run(ctx context.Context) {
	defer close(s.done)
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-s.messages:
			if !ok {
				return
			}
			s.handle(ctx, msg)
		}
	}
}

// handle hands msg to the handler and acknowledges it by the result: nil
// acks; a permanent error, or an error once the attempts are exhausted,
// gives up on msg; another error waits the retry delay, then nacks.
func (s *subscription) handle(ctx context.Context, msg *message.Message) {
	t := s.transport
	env, err := decodeMessage(t.cfg.prefix, msg)
	if err != nil {
		t.giveUp(msg, err)
		return
	}
	env.Destination = s.sub.Destination
	err = s.callHandler(msg, env)
	if err == nil {
		delete(s.attempts, msg.UUID)
		msg.Ack()
		return
	}
	if s.exhausted(msg.UUID, err) {
		delete(s.attempts, msg.UUID)
		t.giveUp(msg, err)
		return
	}
	s.retryLater(ctx, msg)
}

// exhausted counts a failed attempt of the message uuid and reports
// whether to give up on it: err is permanent, or the attempts reached the
// maximum.
func (s *subscription) exhausted(uuid string, err error) bool {
	s.attempts[uuid]++
	if doger.IsPermanentError(err) {
		return true
	}
	maxAttempts := s.transport.cfg.maxAttempts

	return maxAttempts > 0 && s.attempts[uuid] >= maxAttempts
}

// retryLater waits the retry delay on the transport's clock, or until ctx
// is done, then nacks msg so the backend redelivers it.
func (s *subscription) retryLater(ctx context.Context, msg *message.Message) {
	cfg := s.transport.cfg
	if cfg.retryDelay > 0 {
		fired := make(chan struct{})
		timer := cfg.clock.AfterFunc(cfg.retryDelay, func() { close(fired) })
		select {
		case <-fired:
		case <-ctx.Done():
			timer.Stop()
		}
	}
	msg.Nack()
}

// callHandler runs the handler on env with the context of msg, also
// cancelled when the subscription context is.
func (s *subscription) callHandler(msg *message.Message, env doger.Envelope) error {
	ctx, cancel := context.WithCancel(msg.Context())
	defer cancel()
	unlink := context.AfterFunc(s.subCtx, cancel)
	defer unlink()

	return s.handler(ctx, env)
}

// stop stops consuming, waits for the handler in flight until ctx ends,
// then closes the subscriber and cancels the subscription context (the
// handler's and the subscriber's). When ctx ends first, it cancels the
// handler context before closing, so a subscriber waiting for that
// handler's acknowledgement can close. It returns ctx's error when the
// wait was cut short, joined with the subscriber's Close error (a Close
// panic included). Later calls return nil.
func (s *subscription) stop(ctx context.Context) error {
	var err error
	s.stopOnce.Do(func() {
		s.stopLoop()
		if err = s.waitDone(ctx); err != nil {
			s.cancelSub()
		}
		err = errors.Join(err, closeSubscriber(s.subscriber))
		s.cancelSub()
	})

	return err
}

// waitDone waits for run to return, or for ctx to end, returning ctx's
// error then.
func (s *subscription) waitDone(ctx context.Context) error {
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// closeSubscriber closes subscriber, returning a Close panic as an error.
func closeSubscriber(subscriber message.Subscriber) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("doger: dogerwatermill: subscriber Close panicked: %v", r)
		}
	}()
	if err := subscriber.Close(); err != nil {
		return fmt.Errorf("doger: dogerwatermill: close subscriber: %w", err)
	}

	return nil
}
