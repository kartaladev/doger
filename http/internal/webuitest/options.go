package webuitest

import (
	"context"
	"html"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/kartaladev/doger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var invokedAtRe = regexp.MustCompile(`Invoked at:([^\n<]*)`)

// InvokedAt returns the "Invoked at" value of a service page, unescaped.
func InvokedAt(t *testing.T, page string) string {
	t.Helper()

	m := invokedAtRe.FindStringSubmatch(page)
	require.Len(t, m, 2, "page has no \"Invoked at\" value")

	return html.UnescapeString(strings.TrimSpace(m[1]))
}

// Source returns a root holding a lazy service "db", provided and invoked
// in the caller's test file, with the file and line of its provider.
type Source func(t *testing.T) (root *doger.RootScope, file string, line int)

// SourcePathDisplay runs scenario "Source path display follows the option":
// mount shows the shortened location, mountFull the full one, ending in
// "/<suffix>:<line>".
func SourcePathDisplay(t *testing.T, mount, mountFull Mount, source Source, suffix string) {
	t.Helper()

	tests := []struct {
		name  string
		mount Mount
		full  bool
	}{
		{name: "default", mount: mount},
		{name: "full_source_paths", mount: mountFull, full: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root, file, line := source(t)
			resp := Get(t, tt.mount(t, root), ServicePath(root.ID(), "db"))
			require.Equal(t, http.StatusOK, resp.StatusCode)

			shown := InvokedAt(t, resp.Body)
			location := ":" + strconv.Itoa(line)
			if !tt.full {
				assert.Equal(t, path.Base(file)+location, shown)
				return
			}

			assert.True(t, strings.HasSuffix(shown, "/"+suffix+location), "%q does not end in %q", shown, suffix+location)
			assert.True(t, strings.HasSuffix(file, strings.TrimSuffix(shown, location)), "%q is not the path of %q", shown, file)
		})
	}
}

// RequestValueKey is the context key of RequestValue.
type RequestValueKey struct{}

// RequestValue is the value the error scenarios carry in the request
// context.
const RequestValue = "req-42"

// WithRequestValue returns req with RequestValue in its context.
func WithRequestValue(req *http.Request) *http.Request {
	return req.WithContext(context.WithValue(req.Context(), RequestValueKey{}, RequestValue))
}

// RecordedError is one call of an ErrorRecorder.
type RecordedError struct {
	Err          error
	RequestValue any
}

// ErrorRecorder records the calls of an error handler.
type ErrorRecorder struct {
	mu    sync.Mutex
	calls []RecordedError
}

// Record is the error handler.
func (r *ErrorRecorder) Record(ctx context.Context, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.calls = append(r.calls, RecordedError{Err: err, RequestValue: ctx.Value(RequestValueKey{})})
}

// Calls returns the recorded calls.
func (r *ErrorRecorder) Calls() []RecordedError {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]RecordedError(nil), r.calls...)
}

// AssertRecorded asserts that r received calls render failures, each with
// the request's context.
func AssertRecorded(t *testing.T, r *ErrorRecorder, calls int) {
	t.Helper()

	recorded := r.Calls()
	require.Len(t, recorded, calls)

	for _, call := range recorded {
		require.Error(t, call.Err)
		assert.Contains(t, call.Err.Error(), RenderFailurePath)
		assert.Equal(t, RequestValue, call.RequestValue)
	}
}

// AssertGeneric500 asserts that resp is a generic 500 revealing nothing of
// the render failure.
func AssertGeneric500(t *testing.T, resp Response) {
	t.Helper()

	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	assert.NotContains(t, resp.Body, RenderFailurePath)
	assert.Equal(t, NoSniff, resp.Header.Get("X-Content-Type-Options"))
	assert.Equal(t, ContentTypeText, resp.Header.Get("Content-Type"))
}
