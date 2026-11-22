# dogerwatermill

`github.com/kartaladev/doger/events/watermill` (package `dogerwatermill`) is a [doger](https://github.com/kartaladev/doger) event transport over [watermill](https://watermill.io): any watermill backend whose subscribers can be built per consumer group (SQL, Kafka, NATS JetStream, Redis Streams) carries a container's externalized events. It depends on doger and watermill core only.

```sh
go get github.com/kartaladev/doger/events/watermill@latest
```

## Quick start

```go
transport, err := dogerwatermill.New(publisher, func(ctx context.Context, sub doger.Subscription) (message.Subscriber, error) {
    // build a subscriber whose consumer group is sub.Group and whose new
    // groups start at the latest message
    return newSubscriber(sub.Group)
})
root := doger.NewWithOpts(&doger.InjectorOpts{EventTransport: transport})
```

For PostgreSQL and MySQL, use the factories, binary schemas and outbox of [`wmsql`](./wmsql).

## Defaults

| Behavior | Default | Override |
|---|---|---|
| Wait after a handler error before nacking | 1s | `WithRetryDelay(d)` |
| Attempts before giving up on a message | 10, per process | `WithMaxAttempts(n)`; 0 is unlimited |
| A message given up on | acked and logged | `WithPoisonTopic(topic)` |
| Metadata key prefix | `doger_` | `WithMetadataPrefix(p)` |
| Logger | `watermill.NewSlogLogger(nil)` | `WithLogger(l)` |
| Clock of the retry wait | real clock | `WithClock(c)` |

Delivery is at least once: deduplicate in listeners on `doger.EnvelopeFromContext(ctx).ID`, which is also the message UUID.

The [Watermill transport guide](https://github.com/kartaladev/doger/blob/main/docs/docs/service-lifecycle/watermill-transport.md) covers the backend recipes (Kafka, Redis Streams, NATS JetStream, and which backends cannot meet the contract), the delivery rules and the outbox.
