package dogerwmsql_test

import (
	"context"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/doger"
)

// TestSchema_PayloadBytes: with the binary schemas on both sides, any
// payload, JSON or not, comes back byte for byte (MySQL's JSON column
// rewrites JSON, and both reject non-JSON).
func TestSchema_PayloadBytes(t *testing.T) {
	t.Parallel()

	payloads := map[string][]byte{
		"compact_json": []byte(`{"n":1}`),
		"binary":       {0x00, 0xff, 0x10, 'r', 'a', 'w'},
	}
	for _, e := range engines {
		for name, payload := range payloads {
			t.Run(e.name+"/"+name, func(t *testing.T) {
				t.Parallel()
				testWithTimeout(t, time.Minute)
				db := openDB(t, e)
				pub := newPublisher(t, e, db)
				sub, err := e.factory(db, fast())(context.Background(), doger.Subscription{Destination: topic, Group: "g", Mode: doger.DeliverOnce})
				require.NoError(t, err)
				t.Cleanup(func() { _ = sub.Close() })
				messages, err := sub.Subscribe(t.Context(), topic)
				require.NoError(t, err)

				require.NoError(t, pub.Publish(topic, message.NewMessage("m-1", payload)))

				select {
				case msg := <-messages:
					msg.Ack()
					assert.Equal(t, message.Payload(payload), msg.Payload)
				case <-time.After(10 * time.Second):
					t.Fatal("no message received")
				}
			})
		}
	}
}
