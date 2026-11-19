package dogerhttp

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kartaladev/doger/http/internal/webuitest"
	"github.com/stretchr/testify/assert"
)

func TestWriteHeaders(t *testing.T) {
	t.Parallel()

	h := http.Header{}
	WriteHeaders(h)

	assert.Equal(t, webuitest.ContentTypeHTML, h.Get("Content-Type"))
	webuitest.AssertDefensiveHeaders(t, h)
}

func TestWritePage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		err    error
		status int
		body   string
	}{
		{name: "page", status: http.StatusOK, body: "<p>page</p>"},
		{name: "render_failure", err: errors.New(webuitest.RenderFailure), status: http.StatusInternalServerError, body: "Internal Server Error\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			WritePage(rec, httptest.NewRequest(http.MethodGet, "/", http.NoBody), "<p>page</p>", tt.err)

			assert.Equal(t, tt.status, rec.Code)
			assert.Equal(t, tt.body, rec.Body.String())
			webuitest.AssertDefensiveHeaders(t, rec.Header())
		})
	}
}
