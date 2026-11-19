package dogerhttp

import (
	"net/http"

	"github.com/kartaladev/doger/http/internal/webui"
)

// WriteHeaders sets the defensive headers of the debug UI on h:
//
//	Content-Type: text/html; charset=utf-8
//	X-Content-Type-Options: nosniff
//	Content-Security-Policy: default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'
//	Cache-Control: no-store
//
// Every adapter calls it before writing a page, a redirect or an error, so
// pages are never sniffed, framed, cached or allowed to run a script. A
// consumer middleware can still add its own headers, such as
// Strict-Transport-Security.
func WriteHeaders(h http.Header) {
	webui.WriteHeaders(h)
}
