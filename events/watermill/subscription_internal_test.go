package dogerwatermill

import (
	"errors"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
)

// A subscription remembers the attempts of a bounded number of messages:
// one that failed here and was then handled elsewhere never returns, and
// must not stay in memory for the life of the process.
func TestSubscription_AttemptsBounded(t *testing.T) {
	s := &subscription{transport: &Transport{cfg: config{maxAttempts: DefaultMaxAttempts}}, attempts: newAttemptTracker(DefaultMaxTrackedMessages)}

	for i := range 100_000 {
		s.exhausted("m-"+strconv.Itoa(i), errors.New("out of stock"))
	}

	assert.Equal(t, DefaultMaxTrackedMessages, s.attempts.len(), "messages remembered")
}

func TestAttemptTracker(t *testing.T) {
	tracker := newAttemptTracker(2)

	tracker.touch("a").attempts++
	tracker.touch("b").attempts++
	tracker.touch("a").attempts++ // a failed most recently: b is the oldest
	tracker.touch("c").attempts++ // beyond the limit: b is forgotten

	a, okA := tracker.lookup("a")
	_, okB := tracker.lookup("b")
	c, okC := tracker.lookup("c")
	assert.True(t, okA)
	assert.Equal(t, 2, a.attempts, "a's attempts kept")
	assert.False(t, okB, "the least recently failed is forgotten")
	assert.True(t, okC)
	assert.Equal(t, 1, c.attempts)

	tracker.forget("a")
	tracker.forget("unknown")
	_, okA = tracker.lookup("a")
	assert.False(t, okA, "forgotten")
	assert.Equal(t, 1, tracker.len())
}

func TestAttemptTracker_MiddleMove(t *testing.T) {
	tracker := newAttemptTracker(3)
	for _, uuid := range []string{"x", "y", "z", "y", "w", "v"} { // y moves from the middle
		tracker.touch(uuid)
	}

	var kept []string
	for _, uuid := range []string{"x", "y", "z", "w", "v"} {
		if _, ok := tracker.lookup(uuid); ok {
			kept = append(kept, uuid)
		}
	}
	assert.Equal(t, []string{"y", "w", "v"}, kept, "x, then z, forgotten in the order they last failed")
}
