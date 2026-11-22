package dogerwatermill_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/doger"
	dogerwatermill "github.com/kartaladev/doger/events/watermill"
)

var errBrokerDown = errors.New("broker down")

// orderEnvelope is the envelope of the spec's message fields scenario.
func orderEnvelope() doger.Envelope {
	return doger.Envelope{
		ID:          "id-1",
		Destination: "orders.placed",
		EventType:   "orders.placed",
		Version:     2,
		ContentType: "application/json",
		Payload:     []byte(`{"ID":"1"}`),
		Headers:     map[string]string{"trace": "t-9"},
		PublishedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func TestSend_Fields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		opts     []dogerwatermill.Option
		metadata message.Metadata
	}{
		{
			name: "default_prefix",
			metadata: message.Metadata{
				"doger_event_type":   "orders.placed",
				"doger_version":      "2",
				"doger_content_type": "application/json",
				"doger_published_at": "2026-01-01T00:00:00Z",
				"doger_h_trace":      "t-9",
			},
		},
		{
			name: "custom_prefix",
			opts: []dogerwatermill.Option{dogerwatermill.WithMetadataPrefix("x-")},
			metadata: message.Metadata{
				"x-event_type":   "orders.placed",
				"x-version":      "2",
				"x-content_type": "application/json",
				"x-published_at": "2026-01-01T00:00:00Z",
				"x-h_trace":      "t-9",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pub := &recordingPublisher{}
			transport, err := dogerwatermill.New(pub, (&countingFactory{}).Build, tt.opts...)
			require.NoError(t, err)

			err = transport.Send(context.Background(), orderEnvelope())

			assert.NoError(t, err)
			got := pub.Published()
			require.Len(t, got, 1)
			assert.Equal(t, "orders.placed", got[0].topic)
			assert.Equal(t, "id-1", got[0].msg.UUID)
			assert.Equal(t, message.Payload(`{"ID":"1"}`), got[0].msg.Payload)
			assert.Equal(t, tt.metadata, got[0].msg.Metadata)
		})
	}
}

func TestSend_Failures(t *testing.T) {
	t.Parallel()

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name          string
		publish       func(string, *message.Message) error
		ctx           context.Context
		wantErr       error
		wantContains  string
		wantPublished int
	}{
		{
			name:    "publish_error",
			publish: func(string, *message.Message) error { return errBrokerDown },
			ctx:     context.Background(),
			wantErr: errBrokerDown,
		},
		{
			name:    "cancelled_context",
			publish: func(string, *message.Message) error { return nil },
			ctx:     cancelled,
			wantErr: context.Canceled,
		},
		{
			name:         "publish_panics",
			publish:      func(string, *message.Message) error { panic("kaboom") },
			ctx:          context.Background(),
			wantContains: "kaboom",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pub := &recordingPublisher{publishFn: tt.publish}
			transport, err := dogerwatermill.New(pub, (&countingFactory{}).Build)
			require.NoError(t, err)

			err = transport.Send(tt.ctx, orderEnvelope())

			require.Error(t, err)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			}
			if tt.wantContains != "" {
				assert.ErrorContains(t, err, tt.wantContains)
			}
			assert.Len(t, pub.Published(), tt.wantPublished)
		})
	}
}

func TestTransport_MessageAndPublisher(t *testing.T) {
	t.Parallel()

	pub := &recordingPublisher{}
	transport, err := dogerwatermill.New(pub, (&countingFactory{}).Build, dogerwatermill.WithMetadataPrefix("x-"))
	require.NoError(t, err)
	require.NoError(t, transport.Send(context.Background(), orderEnvelope()))
	sent := pub.Published()[0].msg

	msg := transport.Message(orderEnvelope())

	assert.Same(t, pub, transport.Publisher())
	assert.Equal(t, sent.UUID, msg.UUID)
	assert.Equal(t, sent.Payload, msg.Payload)
	assert.Equal(t, sent.Metadata, msg.Metadata)
}
