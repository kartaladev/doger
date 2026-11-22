package dogerwatermill

import (
	"fmt"
	"maps"
	"strconv"
	"strings"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"

	"github.com/kartaladev/doger"
)

// Metadata keys of the envelope fields, after the metadata prefix.
const (
	keyEventType    = "event_type"
	keyVersion      = "version"
	keyContentType  = "content_type"
	keyPublishedAt  = "published_at"
	keyHeaderPrefix = "h_"
)

// encodeMessage returns the message of env, its metadata keys prefixed
// with prefix.
func encodeMessage(prefix string, env doger.Envelope) *message.Message {
	msg := message.NewMessage(env.ID, env.Payload)
	msg.Metadata.Set(prefix+keyEventType, env.EventType)
	msg.Metadata.Set(prefix+keyVersion, strconv.Itoa(env.Version))
	msg.Metadata.Set(prefix+keyContentType, env.ContentType)
	msg.Metadata.Set(prefix+keyPublishedAt, env.PublishedAt.UTC().Format(time.RFC3339Nano))
	for name, value := range env.Headers {
		msg.Metadata.Set(prefix+keyHeaderPrefix+name, value)
	}

	return msg
}

// errForeignMessage is the reason a message without doger metadata is
// given up on.
var errForeignMessage = doger.PermanentError(fmt.Errorf("doger: dogerwatermill: message without doger metadata"))

// decodeMessage returns the envelope encodeMessage made msg from, msg
// carrying its metadata keys under prefix, or a permanent error for a
// message without them or with malformed values.
func decodeMessage(prefix string, msg *message.Message) (doger.Envelope, error) {
	eventType, ok := msg.Metadata[prefix+keyEventType]
	if !ok {
		return doger.Envelope{}, errForeignMessage
	}
	version, err := strconv.Atoi(msg.Metadata.Get(prefix + keyVersion))
	if err != nil {
		return doger.Envelope{}, doger.PermanentError(fmt.Errorf("doger: dogerwatermill: version of message %q: %w", msg.UUID, err))
	}
	publishedAt, err := time.Parse(time.RFC3339Nano, msg.Metadata.Get(prefix+keyPublishedAt))
	if err != nil {
		return doger.Envelope{}, doger.PermanentError(fmt.Errorf("doger: dogerwatermill: publish time of message %q: %w", msg.UUID, err))
	}
	var headers map[string]string
	headerPrefix := prefix + keyHeaderPrefix
	for key, value := range maps.All(msg.Metadata) {
		if name, ok := strings.CutPrefix(key, headerPrefix); ok {
			if headers == nil {
				headers = map[string]string{}
			}
			headers[name] = value
		}
	}

	return doger.Envelope{
		ID:          msg.UUID,
		EventType:   eventType,
		ContentType: msg.Metadata.Get(prefix + keyContentType),
		Version:     version,
		Payload:     msg.Payload,
		Headers:     headers,
		PublishedAt: publishedAt,
	}, nil
}
