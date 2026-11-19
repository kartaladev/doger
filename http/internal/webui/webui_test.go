package webui

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kartaladev/doger/http/internal/webuitest"
	"github.com/stretchr/testify/assert"
)

func TestWritePage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		err    error
		status int
		body   string
		calls  int
	}{
		{name: "page", status: http.StatusOK, body: "<p>page</p>"},
		{name: "render_failure", err: errors.New(webuitest.RenderFailure), status: http.StatusInternalServerError, body: "Internal Server Error\n", calls: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			calls := 0
			o := Options{ErrorHandler: func(context.Context, error) { calls++ }}
			rec := httptest.NewRecorder()
			WritePage(rec, httptest.NewRequest(http.MethodGet, "/", http.NoBody), "<p>page</p>", tt.err, o)

			assert.Equal(t, tt.status, rec.Code)
			assert.Equal(t, tt.body, rec.Body.String())
			assert.Equal(t, tt.calls, calls)
			webuitest.AssertDefensiveHeaders(t, rec.Header())
		})
	}
}

func TestLink(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		base string
		page string
		want string
	}{
		{name: "root_home", base: "/", page: "", want: "/"},
		{name: "root_page", base: "/", page: "/scope", want: "/scope"},
		{name: "empty_home", base: "", page: "", want: "/"},
		{name: "base_home", base: "/debug/doger", page: "", want: "/debug/doger"},
		{name: "trailing_slash", base: "/debug/doger/", page: "/service", want: "/debug/doger/service"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, Link(tt.base, tt.page, nil))
		})
	}
}
