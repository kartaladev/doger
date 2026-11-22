package dogerwatermill_test

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/pubsub/gochannel"

	"github.com/kartaladev/doger"
	dogerwatermill "github.com/kartaladev/doger/events/watermill"
)

// New takes any watermill publisher and a factory building one subscriber
// per subscription. gochannel keeps the example in memory; a real
// deployment passes a SQL, Kafka, NATS JetStream or Redis Streams
// publisher and a factory that sets the subscription's consumer group.
func ExampleNew() {
	pubSub := gochannel.NewGoChannel(gochannel.Config{}, watermill.NopLogger{})
	defer pubSub.Close()

	factory := func(_ context.Context, _ doger.Subscription) (message.Subscriber, error) {
		// set sub.Group as the backend's consumer group here
		return gochannel.NewGoChannel(gochannel.Config{}, watermill.NopLogger{}), nil
	}

	// zero configuration: retry after 1s, give up after 10 attempts,
	// metadata keys prefixed "doger_"
	transport, err := dogerwatermill.New(pubSub, factory)
	fmt.Println(transport != nil, err)

	// every default can be overridden; invalid values are rejected
	_, err = dogerwatermill.New(pubSub, factory,
		dogerwatermill.WithRetryDelay(5*time.Second),
		dogerwatermill.WithMaxAttempts(-1),
	)
	fmt.Println(errors.Is(err, dogerwatermill.ErrInvalidConfig))

	// the transport is the container's event transport
	root := doger.NewWithOpts(&doger.InjectorOpts{EventTransport: transport})
	_ = root.Shutdown()

	// Output:
	// true <nil>
	// true
}

// A message the transport gives up on (a permanent handler error, the
// attempts exhausted, or a message without doger metadata) is published to
// the poison topic, then acknowledged. Without a poison topic it is
// acknowledged and logged.
func ExampleWithPoisonTopic() {
	pubSub := gochannel.NewGoChannel(gochannel.Config{}, watermill.NopLogger{})
	defer pubSub.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	poisoned, err := pubSub.Subscribe(ctx, "orders.poison")
	if err != nil {
		panic(err)
	}

	transport, err := dogerwatermill.New(pubSub,
		func(context.Context, doger.Subscription) (message.Subscriber, error) { return pubSub, nil },
		dogerwatermill.WithPoisonTopic("orders.poison"),
		dogerwatermill.WithMaxAttempts(3), // ordinary errors: give up after 3 runs
	)
	if err != nil {
		panic(err)
	}
	stop, err := transport.Subscribe(ctx,
		doger.Subscription{Destination: "orders.placed", Group: "mail", Mode: doger.DeliverOnce},
		func(context.Context, doger.Envelope) error {
			return doger.PermanentError(errors.New("unknown customer")) // no retry
		})
	if err != nil {
		panic(err)
	}
	defer stop(context.Background())

	err = transport.Send(ctx, doger.Envelope{
		ID:          "order-1",
		Destination: "orders.placed",
		EventType:   "orders.placed",
		Version:     1,
		ContentType: "application/json",
		Payload:     []byte(`{"ID":"1"}`),
		PublishedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		panic(err)
	}

	msg := <-poisoned
	msg.Ack()
	fmt.Println(msg.UUID, msg.Metadata.Get("doger_event_type"))

	// Output:
	// order-1 orders.placed
}
