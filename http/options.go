package dogerhttp

import (
	"context"
	"strings"

	"github.com/kartaladev/doger/http/internal/webui"
)

// Option configures the debug UI. Pass options to an adapter's Use or to a
// page function. Without options the UI shows shortened source paths and
// discards render errors.
type Option func(*webui.Options)

// pageOptions are the resolved options the page builders read.
type pageOptions = webui.Options

func newPageOptions(opts []Option) pageOptions {
	return webui.Resolve(opts)
}

// WithFullSourcePaths shows the full recorded path of the file where a
// service was provided, instead of the default "<file base name>:<line>",
// which reveals no directory.
func WithFullSourcePaths() Option {
	return func(o *webui.Options) {
		o.FullSourcePaths = true
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
	return func(o *webui.Options) {
		o.ErrorHandler = handler
	}
}

// shortenSourcePath returns location, a "<path>:<line>" source location,
// without its directory. It handles both "/" and "\" separators.
func shortenSourcePath(location string) string {
	return location[strings.LastIndexAny(location, `/\`)+1:]
}
