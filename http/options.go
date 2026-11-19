package dogerhttp

import (
	"context"
	"strings"
)

// Option configures the debug UI. Pass options to an adapter's Use or to a
// page function. Without options the UI shows shortened source paths and
// discards render errors.
type Option func(*pageOptions)

type pageOptions struct {
	fullSourcePaths bool
	errorHandler    func(ctx context.Context, err error)
}

func newPageOptions(opts []Option) pageOptions {
	o := pageOptions{}
	for _, opt := range opts {
		opt(&o)
	}

	return o
}

// WithFullSourcePaths shows the full recorded path of the file where a
// service was provided, instead of the default "<file base name>:<line>",
// which reveals no directory.
func WithFullSourcePaths() Option {
	return func(o *pageOptions) {
		o.fullSourcePaths = true
	}
}

// WithErrorHandler sets the handler that receives each error of a page that
// fails to render, once, with the request's context (Fiber passes
// c.UserContext()). The client still gets a generic 500 that reveals nothing
// of the error.
//
// By default the error is discarded: the UI writes nothing to stdout, stderr
// or a global logger. A nil handler restores the default, and the last
// WithErrorHandler given wins. A panicking handler propagates to the
// framework's recovery.
func WithErrorHandler(handler func(ctx context.Context, err error)) Option {
	return func(o *pageOptions) {
		o.errorHandler = handler
	}
}

// ReportError passes err to the handler set with [WithErrorHandler] in opts,
// if any. Adapters call it once for each page that fails to render, before
// writing the generic 500.
func ReportError(ctx context.Context, err error, opts ...Option) {
	o := newPageOptions(opts)
	if err == nil || o.errorHandler == nil {
		return
	}

	o.errorHandler(ctx, err)
}

// shortenSourcePath returns location, a "<path>:<line>" source location,
// without its directory. It handles both "/" and "\" separators.
func shortenSourcePath(location string) string {
	return location[strings.LastIndexAny(location, `/\`)+1:]
}
