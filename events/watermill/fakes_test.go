package dogerwatermill_test

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"

	"github.com/kartaladev/doger"
)

// published is one message a recordingPublisher received.
type published struct {
	topic string
	msg   *message.Message
}

// recordingPublisher records the messages published to it. publishFn,
// when set, runs before recording and its error is returned.
type recordingPublisher struct {
	mu        sync.Mutex
	messages  []published
	calls     atomic.Int64
	publishFn func(topic string, msg *message.Message) error
}

func (p *recordingPublisher) Publish(topic string, msgs ...*message.Message) error {
	p.calls.Add(1)
	for _, msg := range msgs {
		if p.publishFn != nil {
			if err := p.publishFn(topic, msg); err != nil {
				return err
			}
		}
		p.mu.Lock()
		p.messages = append(p.messages, published{topic: topic, msg: msg})
		p.mu.Unlock()
	}

	return nil
}

func (p *recordingPublisher) Close() error { return nil }

// Published returns a copy of the recorded messages.
func (p *recordingPublisher) Published() []published {
	p.mu.Lock()
	defer p.mu.Unlock()

	return append([]published(nil), p.messages...)
}

// countingFactory is a SubscriberFactory that counts its calls and
// returns the subscriber and error it was given.
type countingFactory struct {
	calls atomic.Int64
	mu    sync.Mutex
	subs  []doger.Subscription
	sub   message.Subscriber
	err   error
}

func (f *countingFactory) Build(_ context.Context, sub doger.Subscription) (message.Subscriber, error) {
	f.calls.Add(1)
	f.mu.Lock()
	f.subs = append(f.subs, sub)
	f.mu.Unlock()

	return f.sub, f.err
}

// Received returns the subscriptions the factory was called with.
func (f *countingFactory) Received() []doger.Subscription {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]doger.Subscription(nil), f.subs...)
}

// fakeSubscriber is a watermill subscriber whose messages the test hands
// out with deliver. It records the topics it was subscribed to and its
// Close calls.
type fakeSubscriber struct {
	subscribeErr error
	closeErr     error
	closePanic   any
	closeBlock   chan struct{} // when set, Close waits for it to close

	mu     sync.Mutex
	topics []string

	closes     atomic.Int64
	subCtx     context.Context //nolint:containedctx // recorded to check its state at Close
	ctxAtClose error           // the Subscribe context's error when Close was first called
	out        chan *message.Message
	closed     chan struct{}
	closeOnce  sync.Once
}

func newFakeSubscriber() *fakeSubscriber {
	return &fakeSubscriber{out: make(chan *message.Message), closed: make(chan struct{})}
}

func (s *fakeSubscriber) Subscribe(ctx context.Context, topic string) (<-chan *message.Message, error) {
	s.mu.Lock()
	s.topics = append(s.topics, topic)
	s.subCtx = ctx
	s.mu.Unlock()
	if s.subscribeErr != nil {
		return nil, s.subscribeErr
	}

	return s.out, nil
}

func (s *fakeSubscriber) Close() error {
	s.closes.Add(1)
	if s.closeBlock != nil {
		<-s.closeBlock
	}
	s.closeOnce.Do(func() {
		s.mu.Lock()
		if s.subCtx != nil {
			s.ctxAtClose = s.subCtx.Err()
		}
		s.mu.Unlock()
		close(s.closed)
	})
	if s.closePanic != nil {
		panic(s.closePanic)
	}

	return s.closeErr
}

// Topics returns the topics the subscriber was subscribed to.
func (s *fakeSubscriber) Topics() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]string(nil), s.topics...)
}

// deliver hands msg to the consumer and waits for its Ack or Nack. It
// reports whether msg was acked, and false when the subscriber closed or
// nobody consumed msg within the timeout.
func (s *fakeSubscriber) deliver(msg *message.Message, timeout time.Duration) (acked, settled bool) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case s.out <- msg:
	case <-s.closed:
		return false, false
	case <-timer.C:
		return false, false
	}
	select {
	case <-msg.Acked():
		return true, true
	case <-msg.Nacked():
		return false, true
	case <-timer.C:
		return false, false
	}
}

// deliverUntilAcked delivers copies of msg until one is acked, up to max
// deliveries, and returns the number of nacks.
func (s *fakeSubscriber) deliverUntilAcked(msg *message.Message, maxDeliveries int, timeout time.Duration) (nacks int, acked bool) {
	for range maxDeliveries {
		acked, settled := s.deliver(msg.Copy(), timeout)
		if !settled {
			return nacks, false
		}
		if acked {
			return nacks, true
		}
		nacks++
	}

	return nacks, false
}

// fakeBroker is an in-process message log: Publish hands each message to
// one subscriber per group of the topic (round robin) and redelivers it
// to that subscriber on Nack. Its Factory builds subscribers by group.
type fakeBroker struct {
	mu     sync.Mutex
	groups map[string]map[string]*brokerGroup // topic -> group -> members
	wg     sync.WaitGroup
}

// brokerGroup holds the members of one group of one topic.
type brokerGroup struct {
	members []*brokerSubscriber
	next    int
}

func newFakeBroker() *fakeBroker {
	return &fakeBroker{groups: map[string]map[string]*brokerGroup{}}
}

func (b *fakeBroker) Publish(topic string, msgs ...*message.Message) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, group := range b.groups[topic] {
		live := group.members[:0]
		for _, m := range group.members {
			if !m.isClosed() {
				live = append(live, m)
			}
		}
		group.members = live
		if len(live) == 0 {
			continue
		}
		member := live[group.next%len(live)]
		group.next++
		for _, msg := range msgs {
			member.enqueue(msg)
		}
	}

	return nil
}

func (b *fakeBroker) Close() error { return nil }

// Factory is a SubscriberFactory building broker subscribers.
func (b *fakeBroker) Factory(_ context.Context, sub doger.Subscription) (message.Subscriber, error) {
	return &brokerSubscriber{broker: b, group: sub.Group, closed: make(chan struct{}), wake: make(chan struct{}, 1)}, nil
}

// brokerSubscriber is one member of a broker group. A pump goroutine
// hands its queued messages out one at a time.
type brokerSubscriber struct {
	broker *fakeBroker
	group  string

	mu     sync.Mutex
	queue  []*message.Message
	wake   chan struct{}
	closed chan struct{}
	once   sync.Once
}

func (s *brokerSubscriber) Subscribe(ctx context.Context, topic string) (<-chan *message.Message, error) {
	b := s.broker
	b.mu.Lock()
	if b.groups[topic] == nil {
		b.groups[topic] = map[string]*brokerGroup{}
	}
	g := b.groups[topic][s.group]
	if g == nil {
		g = &brokerGroup{}
		b.groups[topic][s.group] = g
	}
	g.members = append(g.members, s)
	b.mu.Unlock()

	out := make(chan *message.Message)
	b.wg.Go(func() { s.pump(ctx, out) })

	return out, nil
}

func (s *brokerSubscriber) Close() error {
	s.once.Do(func() { close(s.closed) })
	return nil
}

func (s *brokerSubscriber) isClosed() bool {
	select {
	case <-s.closed:
		return true
	default:
		return false
	}
}

func (s *brokerSubscriber) enqueue(msg *message.Message) {
	s.mu.Lock()
	s.queue = append(s.queue, msg)
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// pump delivers the queue to out, one message at a time, redelivering a
// copy on Nack, until the subscriber closes or ctx is done; it then closes
// out.
func (s *brokerSubscriber) pump(ctx context.Context, out chan<- *message.Message) {
	defer close(out)
	for {
		s.mu.Lock()
		var next *message.Message
		if len(s.queue) > 0 {
			next = s.queue[0]
		}
		s.mu.Unlock()
		if next == nil {
			select {
			case <-s.wake:
				continue
			case <-s.closed:
				return
			case <-ctx.Done():
				return
			}
		}
		msg := next.Copy()
		select {
		case out <- msg:
		case <-s.closed:
			return
		case <-ctx.Done():
			return
		}
		select {
		case <-msg.Acked():
			s.mu.Lock()
			s.queue = s.queue[1:]
			s.mu.Unlock()
		case <-msg.Nacked():
		case <-s.closed:
			return
		case <-ctx.Done():
			return
		}
	}
}

// fakeClock is a doger.Clock whose time moves only with Advance. Started
// records the delay of every timer AfterFunc schedules.
type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	timers  []*fakeTimer
	Started chan time.Duration
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Started: make(chan time.Duration, 64)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.now
}

func (c *fakeClock) Since(t time.Time) time.Duration { return c.Now().Sub(t) }

// AfterFunc reports d on Started once the timer is registered, so an
// Advance after receiving it runs the timer.
func (c *fakeClock) AfterFunc(d time.Duration, f func()) doger.Timer {
	timer := &fakeTimer{clock: c, f: f}
	if d <= 0 {
		c.Started <- d
		go f()
		return timer
	}
	c.mu.Lock()
	timer.at = c.now.Add(d)
	c.timers = append(c.timers, timer)
	c.mu.Unlock()
	c.Started <- d

	return timer
}

// Advance moves the clock by d, running the timers that became due.
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	var due []*fakeTimer
	pending := c.timers[:0]
	for _, timer := range c.timers {
		if timer.at.After(c.now) {
			pending = append(pending, timer)
		} else {
			due = append(due, timer)
		}
	}
	c.timers = pending
	c.mu.Unlock()
	for _, timer := range due {
		go timer.f()
	}
}

// fakeTimer is a call scheduled on a fakeClock.
type fakeTimer struct {
	clock *fakeClock
	at    time.Time
	f     func()
}

func (t *fakeTimer) Stop() bool {
	c := t.clock
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, timer := range c.timers {
		if timer == t {
			c.timers = append(c.timers[:i], c.timers[i+1:]...)
			return true
		}
	}

	return false
}
