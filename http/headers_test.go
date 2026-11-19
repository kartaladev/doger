package dogerhttp

import (
	"net/http"
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
