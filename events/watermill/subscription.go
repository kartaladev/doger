package dogerwatermill

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ThreeDotsLabs/watermill"
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

	attempts *attemptTracker // failing messages by UUID; owned by run
}

// run consumes the messages until ctx is done or the channel closes,
// logging a channel the subscriber closed on its own. A message received
// once ctx is done is nacked, not handled: select picks at random between
// ready cases, so the stop must be checked after receiving.
func (s *subscription) run(ctx context.Context) {
	defer close(s.done)
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-s.messages:
			if !ok {
				if ctx.Err() == nil {
					s.transport.cfg.logger.Error("doger: dogerwatermill: the subscriber closed its message channel; the subscription receives nothing more",
						errSubscriberGone, watermill.LogFields{"topic": s.sub.Destination, "group": s.sub.Group})
				}
				return
			}
			if ctx.Err() != nil {
				msg.Nack()
				return
			}
			s.handle(ctx, msg)
		}
	}
}

// errSubscriberGone is the error logged when a subscriber closes its
// message channel before the subscription stopped.
var errSubscriberGone = errors.New("doger: dogerwatermill: message channel closed")

// handle hands msg to the handler and acknowledges it by the result: nil
// acks; a permanent error, or an error once the attempts are exhausted,
// gives up on msg; another error waits the retry delay, then nacks.
//
// A message whose give-up is pending (its poison publish failed) is given
// up on again without running the handler.
func (s *subscription) handle(ctx context.Context, msg *message.Message) {
	if entry, ok := s.attempts.lookup(msg.UUID); ok && entry.giveUp != nil {
		s.giveUp(ctx, msg, entry.giveUp)
		return
	}
	env, err := decodeMessage(s.transport.cfg.prefix, msg)
	if err != nil {
		s.giveUp(ctx, msg, err)
		return
	}
	env.Destination = s.sub.Destination
	err = s.callHandler(msg, env)
	if err == nil {
		s.attempts.forget(msg.UUID)
		msg.Ack()
		return
	}
	if s.exhausted(msg.UUID, err) {
		s.giveUp(ctx, msg, err)
		return
	}
	s.retryLater(ctx, msg)
}

// giveUp gives up on msg after reason (see Transport.giveUp). When the
// poison publish fails, it remembers reason, so the redelivery is given up
// on without running the handler, and nacks msg after the retry delay.
func (s *subscription) giveUp(ctx context.Context, msg *message.Message, reason error) {
	if err := s.transport.giveUp(msg, reason); err != nil {
		s.attempts.touch(msg.UUID).giveUp = reason
		s.retryLater(ctx, msg)
		return
	}
	s.attempts.forget(msg.UUID)
}

// exhausted counts a failed attempt of the message uuid and reports
// whether to give up on it: err is permanent, or the attempts reached the
// maximum.
func (s *subscription) exhausted(uuid string, err error) bool {
	entry := s.attempts.touch(uuid)
	entry.attempts++
	if doger.IsPermanentError(err) {
		return true
	}
	maxAttempts := s.transport.cfg.maxAttempts

	return maxAttempts > 0 && entry.attempts >= maxAttempts
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
// cancelled when the subscription context is, a panic becoming an error.
func (s *subscription) callHandler(msg *message.Message, env doger.Envelope) (err error) {
	ctx, cancel := context.WithCancel(msg.Context())
	defer cancel()
	unlink := context.AfterFunc(s.subCtx, cancel)
	defer unlink()
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("doger: dogerwatermill: handler panicked: %v", r)
		}
	}()

	return s.handler(ctx, env)
}

// stop stops consuming, waits for the handler in flight until ctx ends,
// then closes the subscriber and cancels the subscription context (the
// handler's and the subscriber's), returning the subscriber's Close error
// (a Close panic included). When ctx ends first, it cancels the handler
// context, so a subscriber waiting for that handler's acknowledgement can
// close, and returns ctx's error at once: the subscriber then closes in
// the background, its Close error logged. Later calls return nil.
func (s *subscription) stop(ctx context.Context) error {
	var err error
	s.stopOnce.Do(func() {
		s.stopLoop()
		if err = s.waitDone(ctx); err != nil {
			s.cancelSub()
			go s.closeLate()
			return
		}
		err = closeSubscriber(s.subscriber)
		s.cancelSub()
	})

	return err
}

// closeLate closes the subscriber after stop's context ended, logging its
// Close error.
func (s *subscription) closeLate() {
	if err := closeSubscriber(s.subscriber); err != nil {
		s.transport.cfg.logger.Error("doger: dogerwatermill: close subscriber after the stop deadline", err,
			watermill.LogFields{"topic": s.sub.Destination, "group": s.sub.Group})
	}
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
