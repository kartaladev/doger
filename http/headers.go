package dogerhttp

import (
	"io"
	"net/http"
)

// WritePage writes page to w with the headers of [WriteHeaders] and status
// 200, or, when err is not nil, a generic 500 Internal Server Error whose
// body reveals nothing of err.
//
// Adapters for frameworks that expose an [http.ResponseWriter] write every
// page with it.
func WritePage(w http.ResponseWriter, page string, err error) {
	WriteHeaders(w.Header())

	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	// The status line is sent: a failed write cannot be reported to the
	// client any more.
	_, _ = io.WriteString(w, page)
}

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
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'")
	h.Set("Cache-Control", "no-store")
}
