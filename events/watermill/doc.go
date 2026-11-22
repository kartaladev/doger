// Package dogerwatermill is a doger event transport over watermill
// (github.com/ThreeDotsLabs/watermill): any watermill backend whose
// subscribers can be built per consumer group (SQL, Kafka, NATS JetStream,
// Redis Streams and others) carries a container's externalized events.
//
// [New] takes a watermill message.Publisher and a [SubscriberFactory] that
// builds one subscriber per doger.Subscription, so the factory maps the
// subscription's group, mode and ephemeral flag onto the backend's own
// configuration. The returned [*Transport] implements doger.EventTransport.
//
// # Message mapping
//
// Send publishes one message per envelope: its UUID is the envelope ID,
// its payload the envelope payload, and metadata keys under a prefix
// ("doger_" by default, see [WithMetadataPrefix]) carry the event type,
// version, content type, publish time and headers. A received message
// without the event type key is not doger's: it never reaches a handler
// and is given up on.
//
// # Acknowledgement, retries and poison messages
//
// Each subscription consumes its messages one at a time in one goroutine.
// A nil handler result acks the message. An error waits the retry delay
// ([WithRetryDelay], 1s by default), then nacks it, so the backend
// redelivers it. The transport gives up on a message after
// [WithMaxAttempts] runs (10 by default, 0 for unlimited), on a
// doger.PermanentError, or when it carries no doger metadata: it publishes
// the message to the [WithPoisonTopic] topic and acks it (a failed publish
// nacks it), or, with no poison topic, logs and acks it.
//
// Attempts are counted per message UUID in the consuming process: they
// reset when the process restarts or the message is acked, so a process
// that crashes while handling a message retries it again from one.
// Permanent errors and the poison topic are the intended exits. Delivery
// is at least once: deduplicate in listeners by
// doger.EnvelopeFromContext(ctx).ID.
package dogerwatermill
