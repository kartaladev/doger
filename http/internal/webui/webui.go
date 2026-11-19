// Package webui holds what package dogerhttp and its adapter modules share
// to serve the debug UI: the resolved options, the response helpers and the
// link builder. It is internal, so none of it is part of the public API.
package webui

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Options are the resolved options of the debug UI.
type Options struct {
	// FullSourcePaths shows full source paths instead of "<file>:<line>".
	FullSourcePaths bool
	// ErrorHandler receives each render failure; nil discards it.
	ErrorHandler func(ctx context.Context, err error)
}

// Resolve applies opts in order and returns the result.
func Resolve[O ~func(*Options)](opts []O) Options {
	o := Options{}
	for _, opt := range opts {
		opt(&o)
	}

	return o
}

// WriteHeaders sets the defensive headers of the debug UI on h.
func WriteHeaders(h http.Header) {
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'")
	h.Set("Cache-Control", "no-store")
}

// WritePage writes page to w with the headers of WriteHeaders and status
// 200, or, when err is not nil, reports err with the context of r and writes
// a generic 500 Internal Server Error whose body reveals nothing of err.
func WritePage(w http.ResponseWriter, r *http.Request, page string, err error, o Options) {
	WriteHeaders(w.Header())

	if err != nil {
		ReportError(r.Context(), err, o)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)

		return
	}

	w.WriteHeader(http.StatusOK)
	// The status line is sent: a failed write cannot be reported to the
	// client any more.
	_, _ = io.WriteString(w, page)
}

// ReportError passes err to the error handler of o, if any.
func ReportError(ctx context.Context, err error, o Options) {
	if err == nil || o.ErrorHandler == nil {
		return
	}

	o.ErrorHandler(ctx, err)
}

// Base returns basePath without one trailing "/", so that "/" mounts the UI
// at the root of the server and every link built on it starts with a single
// "/".
func Base(basePath string) string {
	return strings.TrimSuffix(basePath, "/")
}

// HomeRoute returns the route of the home page under base, a result of
// Base: base itself, or "/" at the root of the server.
func HomeRoute(base string) string {
	if base == "" {
		return "/"
	}

	return base
}

// Link returns the link to page under basePath with query. The path is
// escaped as a URL path and the query values with URL query escaping.
func Link(basePath, page string, query url.Values) string {
	u := url.URL{Path: HomeRoute(Base(basePath) + page), RawQuery: query.Encode()}

	return u.String()
}

// ScopeLink returns the link to the scope tree page of scopeID.
func ScopeLink(basePath, scopeID string) string {
	return Link(basePath, "/scope", url.Values{"scope_id": {scopeID}})
}

// ServiceLink returns the link to the page of the service serviceName of
// scopeID.
func ServiceLink(basePath, scopeID, serviceName string) string {
	return Link(basePath, "/service", url.Values{
		"scope_id":     {scopeID},
		"service_name": {serviceName},
	})
}
