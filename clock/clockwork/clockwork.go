package dogerclockwork

import (
	"reflect"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/kartaladev/doger"
)

// Wrap returns a doger.Clock that delegates every method to clock. Wrap(nil)
// returns a nil doger.Clock, which a container reads as doger.RealClock().
// A typed nil, such as a nil *clockwork.FakeClock, counts as nil.
func Wrap(clock clockwork.Clock) doger.Clock {
	if isNil(clock) {
		return nil
	}

	return wrapped{clock: clock}
}

// isNil reports whether clock is nil or an interface holding a nil value
// of a nilable kind.
func isNil(clock clockwork.Clock) bool {
	if clock == nil {
		return true
	}
	v := reflect.ValueOf(clock)
	k := v.Kind()
	nilable := k == reflect.Pointer || k == reflect.Map || k == reflect.Slice ||
		k == reflect.Func || k == reflect.Chan || k == reflect.Interface

	return nilable && v.IsNil()
}

// Unwrap returns the clockwork clock a doger.Clock made by Wrap delegates
// to, and true. For any other clock, including doger.RealClock() and nil,
// it returns nil and false.
func Unwrap(clock doger.Clock) (clockwork.Clock, bool) {
	w, ok := clock.(wrapped)
	if !ok {
		return nil, false
	}

	return w.clock, true
}

// wrapped is the doger.Clock made by Wrap.
type wrapped struct {
	clock clockwork.Clock
}

func (w wrapped) Now() time.Time                  { return w.clock.Now() }
func (w wrapped) Since(t time.Time) time.Duration { return w.clock.Since(t) }

func (w wrapped) AfterFunc(d time.Duration, f func()) doger.Timer {
	return w.clock.AfterFunc(d, f)
}
