package dogerwmsql

import (
	"context"
	stdsql "database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
)

// DefaultCleanupTimeout bounds the deletion of an ephemeral group's
// offset (see [WithCleanupTimeout]).
const DefaultCleanupTimeout = 30 * time.Second

// ephemeralSubscriber is the subscriber of an ephemeral group: Close also
// deletes the group's stored offset for every topic it subscribed to.
type ephemeralSubscriber struct {
	message.Subscriber
	db          *stdsql.DB
	group       string
	deleteQuery func(topic string) string
	timeout     time.Duration // bounds the deletion

	mu     sync.Mutex
	topics []string
}

func (s *ephemeralSubscriber) Subscribe(ctx context.Context, topic string) (<-chan *message.Message, error) {
	messages, err := s.Subscriber.Subscribe(ctx, topic)
	if err == nil {
		s.mu.Lock()
		s.topics = append(s.topics, topic)
		s.mu.Unlock()
	}

	return messages, err
}

// Close closes the subscriber, then deletes the group's offsets, joining
// the errors of both.
func (s *ephemeralSubscriber) Close() error {
	err := s.Subscriber.Close()

	s.mu.Lock()
	topics := s.topics
	s.topics = nil
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()
	for _, topic := range topics {
		if _, delErr := s.db.ExecContext(ctx, s.deleteQuery(topic), s.group); delErr != nil {
			err = errors.Join(err, fmt.Errorf("doger: dogerwmsql: delete ephemeral group %q of %q: %w", s.group, topic, delErr))
		}
	}

	return err
}
