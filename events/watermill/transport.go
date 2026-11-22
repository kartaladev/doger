package dogerwatermill

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"

	"github.com/kartaladev/doger"
)

// ErrInvalidConfig is matched by the error of [New] for a nil publisher, a
// nil factory or an invalid option, and by the error of Subscribe for a
// factory that returns no subscriber.
var ErrInvalidConfig = errors.New("doger: dogerwatermill: invalid configuration")

// Defaults of [New].
const (
	DefaultRetryDelay     = time.Second
	DefaultMaxAttempts    = 10
	DefaultMetadataPrefix = "doger_"
)

// SubscriberFactory builds the watermill subscriber of one
// doger.Subscription. The transport calls it once per Subscribe and owns
// the subscriber it returns: it closes it when the subscription stops.
//
// Map the subscription onto the backend's configuration: sub.Group is the
// consumer group, so members of one group compete and different groups
// each receive every message; sub.Ephemeral marks a group that ends with
// the subscription. A new group must start at the latest message, so set
// the backend's start position accordingly (see the package guide for
// Kafka, Redis Streams and NATS JetStream). Returning (nil, nil) is a
// configuration error.
type SubscriberFactory func(ctx context.Context, sub doger.Subscription) (message.Subscriber, error)

// Transport is a doger.EventTransport over a watermill publisher and the
// subscribers of a [SubscriberFactory]. Build it with [New]. It is safe
// for concurrent use.
type Transport struct {
	pub  message.Publisher
	subs SubscriberFactory
	cfg  config
}

var _ doger.EventTransport = (*Transport)(nil)

// config is the configuration the options set.
type config struct {
	retryDelay  time.Duration
	maxAttempts int
	poisonTopic string
	prefix      string
	logger      watermill.LoggerAdapter
	clock       doger.Clock
}

// Option configures [New].
type Option func(*config) error

// WithRetryDelay sets how long the transport waits after a handler error
// before it nacks the message, asking the backend to redeliver it.
// Default 1s; 0 nacks at once; a negative delay is an error.
func WithRetryDelay(d time.Duration) Option {
	return func(c *config) error {
		if d < 0 {
			return fmt.Errorf("%w: negative retry delay %v", ErrInvalidConfig, d)
		}
		c.retryDelay = d
		return nil
	}
}

// WithMaxAttempts sets how many times one message (by UUID) is handled
// before the transport gives up on it: it then goes to the poison topic
// (see [WithPoisonTopic]), or is acknowledged and logged. Attempts are
// counted per process and reset when it restarts. Default 10; 0 means
// unlimited; a negative count is an error.
func WithMaxAttempts(n int) Option {
	return func(c *config) error {
		if n < 0 {
			return fmt.Errorf("%w: negative max attempts %d", ErrInvalidConfig, n)
		}
		c.maxAttempts = n
		return nil
	}
}

// WithPoisonTopic sets the topic a message is published to when the
// transport gives up on it (a permanent handler error, the attempts
// exhausted, or a message without doger metadata), before it is
// acknowledged. A failed publish to it nacks the message. Default none:
// the message is acknowledged and logged.
func WithPoisonTopic(topic string) Option {
	return func(c *config) error {
		c.poisonTopic = topic
		return nil
	}
}

// WithMetadataPrefix sets the prefix of the metadata keys carrying the
// envelope's fields. Default "doger_"; an empty prefix is an error.
func WithMetadataPrefix(prefix string) Option {
	return func(c *config) error {
		if prefix == "" {
			return fmt.Errorf("%w: empty metadata prefix", ErrInvalidConfig)
		}
		c.prefix = prefix
		return nil
	}
}

// WithLogger sets the logger of dropped messages and failures. Default
// watermill.NewSlogLogger(nil), the slog default logger; nil restores it.
func WithLogger(logger watermill.LoggerAdapter) Option {
	return func(c *config) error {
		c.logger = logger
		return nil
	}
}

// WithClock sets the clock of the retry delay, such as a fake clock in
// tests. Default doger.RealClock(); nil restores it.
func WithClock(clock doger.Clock) Option {
	return func(c *config) error {
		c.clock = clock
		return nil
	}
}

// New returns a transport publishing with pub and consuming through the
// subscribers subs builds. It starts no goroutine and calls neither until
// Send or Subscribe. It never closes pub, which the caller owns. A nil pub
// or subs, or an invalid option, returns an error matching
// [ErrInvalidConfig]. Options apply in order, the last one winning.
func New(pub message.Publisher, subs SubscriberFactory, opts ...Option) (*Transport, error) {
	if pub == nil {
		return nil, fmt.Errorf("%w: nil publisher", ErrInvalidConfig)
	}
	if subs == nil {
		return nil, fmt.Errorf("%w: nil subscriber factory", ErrInvalidConfig)
	}
	cfg, err := applyOptions(opts)
	if err != nil {
		return nil, err
	}

	return &Transport{pub: pub, subs: subs, cfg: cfg}, nil
}

// applyOptions returns the defaults with opts applied in order, nil
// options skipped, or the first option's error.
func applyOptions(opts []Option) (config, error) {
	cfg := config{
		retryDelay:  DefaultRetryDelay,
		maxAttempts: DefaultMaxAttempts,
		prefix:      DefaultMetadataPrefix,
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(&cfg); err != nil {
			return config{}, err
		}
	}
	if cfg.logger == nil {
		cfg.logger = watermill.NewSlogLogger(nil)
	}
	if cfg.clock == nil {
		cfg.clock = doger.RealClock()
	}

	return cfg, nil
}

// Publisher returns the publisher the transport was built with, such as
// for a forwarder relaying outbox messages to the broker.
func (t *Transport) Publisher() message.Publisher { return t.pub }

// Message returns the message Send would publish for env: its UUID is
// env.ID, its payload env.Payload, and its metadata carries the other
// fields under the transport's metadata prefix. An outbox writing messages
// itself uses it, so subscribers decode them as the transport's own.
func (t *Transport) Message(env doger.Envelope) *message.Message {
	return encodeMessage(t.cfg.prefix, env)
}

// Send publishes env as one message to topic env.Destination: its UUID is
// env.ID, its payload env.Payload, and its metadata carries the other
// fields under the metadata prefix (see [WithMetadataPrefix]). A done ctx
// returns its error without publishing; ctx becomes the message context,
// for backends that honor it. A publish error or panic is returned.
func (t *Transport) Send(ctx context.Context, env doger.Envelope) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	msg := t.Message(env)
	msg.SetContext(ctx)

	return t.publish(env.Destination, msg)
}

// Subscribe builds the subscription's subscriber with the factory,
// subscribes it to sub.Destination and consumes it in one goroutine,
// handing each message carrying doger metadata to h. A factory or
// subscribe error is returned, and a subscriber already built is closed.
// The returned stop function ends consumption, waits for the handler in
// flight and closes the subscriber.
func (t *Transport) Subscribe(ctx context.Context, sub doger.Subscription, h doger.EnvelopeHandler) (func(context.Context) error, error) {
	subscriber, err := t.buildSubscriber(ctx, sub)
	if err != nil {
		return nil, err
	}
	subCtx, cancelSub := context.WithCancel(ctx)
	messages, err := subscriber.Subscribe(subCtx, sub.Destination)
	if err != nil {
		cancelSub()
		return nil, errors.Join(
			fmt.Errorf("doger: dogerwatermill: subscribe to %q: %w", sub.Destination, err),
			closeSubscriber(subscriber),
		)
	}

	loopCtx, stopLoop := context.WithCancel(subCtx)
	consumer := &subscription{
		transport:  t,
		sub:        sub,
		handler:    h,
		subscriber: subscriber,
		messages:   messages,
		subCtx:     subCtx,
		cancelSub:  cancelSub,
		stopLoop:   stopLoop,
		done:       make(chan struct{}),
		attempts:   map[string]int{},
	}
	go consumer.run(loopCtx)

	return consumer.stop, nil
}

// buildSubscriber calls the factory for sub, returning its error, an error
// matching ErrInvalidConfig for a nil subscriber, or its panic as an error.
func (t *Transport) buildSubscriber(ctx context.Context, sub doger.Subscription) (subscriber message.Subscriber, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("doger: dogerwatermill: subscriber factory for %q panicked: %v", sub.Destination, r)
		}
	}()
	subscriber, err = t.subs(ctx, sub)
	if err != nil {
		return nil, fmt.Errorf("doger: dogerwatermill: build subscriber for %q: %w", sub.Destination, err)
	}
	if subscriber == nil {
		return nil, fmt.Errorf("%w: the subscriber factory returned no subscriber for %q", ErrInvalidConfig, sub.Destination)
	}

	return subscriber, nil
}

// giveUp ends the deliveries of msg after reason: it publishes msg to the
// poison topic and acks it, nacking it when that publish fails, or, with
// no poison topic, logs reason and acks it.
func (t *Transport) giveUp(msg *message.Message, reason error) {
	fields := watermill.LogFields{"message_uuid": msg.UUID}
	if t.cfg.poisonTopic == "" {
		t.cfg.logger.Error("doger: dogerwatermill: dropped message", reason, fields)
		msg.Ack()
		return
	}
	if err := t.publish(t.cfg.poisonTopic, msg.Copy()); err != nil {
		t.cfg.logger.Error("doger: dogerwatermill: poison publish failed", err, fields)
		msg.Nack()
		return
	}
	msg.Ack()
}

// publish publishes msg to topic, returning a publish panic as an error.
func (t *Transport) publish(topic string, msg *message.Message) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("doger: dogerwatermill: publish to %q panicked: %v", topic, r)
		}
	}()
	if err := t.pub.Publish(topic, msg); err != nil {
		return fmt.Errorf("doger: dogerwatermill: publish to %q: %w", topic, err)
	}

	return nil
}
