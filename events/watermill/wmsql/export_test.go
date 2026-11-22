package dogerwmsql

import "github.com/ThreeDotsLabs/watermill"

// ForwarderTopic returns the forwarder topic of o, for the tests.
func ForwarderTopic(o *Outbox) string { return o.topic }

// OutboxLogger returns the logger of o, for the tests.
func OutboxLogger(o *Outbox) watermill.LoggerAdapter { return o.logger }
