package webuitest

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/kartaladev/doger"
)

// The checks below keep this package free of test-only dependencies: it is
// a non-test package of the root module, so whatever it imports becomes a
// requirement of every consumer of the module. msgAndArgs is an optional
// format string and its arguments, describing the check.

// checkEqual reports an error when got is not want.
func checkEqual[T comparable](t testing.TB, want, got T, msgAndArgs ...any) {
	t.Helper()

	if got != want {
		t.Errorf("%sgot %v, want %v", describe(msgAndArgs), got, want)
	}
}

// requireEqual stops the test when got is not want.
func requireEqual[T comparable](t testing.TB, want, got T, msgAndArgs ...any) {
	t.Helper()

	if got != want {
		t.Fatalf("%sgot %v, want %v", describe(msgAndArgs), got, want)
	}
}

// checkTrue reports an error when cond is false.
func checkTrue(t testing.TB, cond bool, msgAndArgs ...any) {
	t.Helper()

	if !cond {
		t.Errorf("%scondition is false", describe(msgAndArgs))
	}
}

// checkContains reports an error when s does not contain sub.
func checkContains(t testing.TB, s, sub string, msgAndArgs ...any) {
	t.Helper()

	if !strings.Contains(s, sub) {
		t.Errorf("%s%q does not contain %q", describe(msgAndArgs), abbreviate(s), sub)
	}
}

// checkNotContains reports an error when s contains sub.
func checkNotContains(t testing.TB, s, sub string, msgAndArgs ...any) {
	t.Helper()

	if strings.Contains(s, sub) {
		t.Errorf("%s%q contains %q", describe(msgAndArgs), abbreviate(s), sub)
	}
}

// requireNoError stops the test when err is not nil.
func requireNoError(t testing.TB, err error, msgAndArgs ...any) {
	t.Helper()

	if err != nil {
		t.Fatalf("%sunexpected error: %v", describe(msgAndArgs), err)
	}
}

// checkStatusIn reports an error when status is none of want.
func checkStatusIn(t testing.TB, status int, want []int, msgAndArgs ...any) {
	t.Helper()

	if slices.Contains(want, status) {
		return
	}

	t.Errorf("%sstatus %d, want one of %v", describe(msgAndArgs), status, want)
}

// sameServices reports whether a and b hold the same services, in any order.
func sameServices(a, b []doger.ServiceDescription) bool {
	if len(a) != len(b) {
		return false
	}

	count := map[doger.ServiceDescription]int{}
	for _, s := range a {
		count[s]++
	}
	for _, s := range b {
		count[s]--
		if count[s] < 0 {
			return false
		}
	}

	return true
}

// describe formats msgAndArgs as a message prefix.
func describe(msgAndArgs []any) string {
	if len(msgAndArgs) == 0 {
		return ""
	}

	format, ok := msgAndArgs[0].(string)
	if !ok {
		return fmt.Sprint(msgAndArgs...) + ": "
	}

	return fmt.Sprintf(format, msgAndArgs[1:]...) + ": "
}

// abbreviate shortens a page quoted in a failure message.
func abbreviate(s string) string {
	const limit = 2048
	if len(s) <= limit {
		return s
	}

	return s[:limit] + "…"
}
