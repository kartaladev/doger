package dogerwmsql

// ForwarderTopic returns the forwarder topic of o, for the tests.
func ForwarderTopic(o *Outbox) string { return o.topic }
