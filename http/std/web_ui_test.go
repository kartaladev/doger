package dogerhttpstd

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"github.com/kartaladev/doger"
	dogerhttp "github.com/kartaladev/doger/http"
	"github.com/kartaladev/doger/http/internal/webuitest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

// mount serves the UI for root at webuitest.BasePath.
func mount(t *testing.T, root doger.Injector) webuitest.Doer {
	t.Helper()

	return webuitest.HandlerDoer(Use(webuitest.BasePath, root))
}

func TestWebUI_MiddlewareAddsHeaders(t *testing.T) {
	t.Parallel()

	const hsts = "max-age=63072000"
	root := doger.New()
	hstsMiddleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Strict-Transport-Security", hsts)
			next.ServeHTTP(w, r)
		})
	}

	resp := webuitest.Get(t, webuitest.HandlerDoer(hstsMiddleware(Use(webuitest.BasePath, root))),
		webuitest.BasePath+"/service")

	assert.Equal(t, hsts, resp.Header.Get("Strict-Transport-Security"))
	assert.Equal(t, webuitest.NoSniff, resp.Header.Get("X-Content-Type-Options"))
}

func TestWebUI_HostileNamesEscaped(t *testing.T) {
	t.Parallel()
	webuitest.HostileNamesEscaped(t, mount)
}

func TestWebUI_LinkOpensNamedService(t *testing.T) {
	t.Parallel()
	webuitest.LinkOpensNamedService(t, mount)
}

func TestWebUI_LinksResolve(t *testing.T) {
	t.Parallel()
	webuitest.LinksResolve(t, mount)
}

func TestWebUI_NoReflection(t *testing.T) {
	t.Parallel()
	webuitest.NoReflection(t, mount)
}

func TestWebUI_DefensiveHeaders(t *testing.T) {
	t.Parallel()
	webuitest.DefensiveHeaders(t, mount)
}

func TestWebUI_NonGETRejected(t *testing.T) {
	t.Parallel()
	webuitest.NonGETRejected(t, mount)
}

func TestWebUI_ReadOnly(t *testing.T) {
	t.Parallel()
	webuitest.ReadOnly(t, mount)
}

func TestWebUI_Fallbacks(t *testing.T) {
	t.Parallel()
	webuitest.Fallbacks(t, mount)
}

func TestWebUI_RenderFailureGeneric500(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	response(rec, httptest.NewRequest(http.MethodGet, "/", http.NoBody), "", errors.New(webuitest.RenderFailure), nil)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.NotContains(t, rec.Body.String(), webuitest.RenderFailurePath)
	assert.Equal(t, webuitest.NoSniff, rec.Header().Get("X-Content-Type-Options"))
}

// mountWith serves the UI for root at webuitest.BasePath with opts.
func mountWith(opts ...dogerhttp.Option) webuitest.Mount {
	return func(t *testing.T, root doger.Injector) webuitest.Doer {
		t.Helper()

		return webuitest.HandlerDoer(Use(webuitest.BasePath, root, opts...))
	}
}

// source returns a root with a lazy service "db" provided and invoked in
// this file, with the file and line of its provider.
func source(t *testing.T) (*doger.RootScope, string, int) {
	t.Helper()

	root := doger.New()
	_, file, line, _ := runtime.Caller(0)
	doger.ProvideNamed(root, "db", func(doger.Injector) (string, error) { return "v", nil })

	_, err := doger.InvokeNamed[string](root, "db")
	require.NoError(t, err)

	return root, file, line + 1
}

func TestWebUI_SourcePathDisplay(t *testing.T) {
	t.Parallel()
	webuitest.SourcePathDisplay(t, mount, mountWith(dogerhttp.WithFullSourcePaths()), source, "http/std/web_ui_test.go")
}

func TestWebUI_ErrorHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		opts  func(record, other func(context.Context, error)) []dogerhttp.Option
		calls int
	}{
		{name: "record", opts: func(record, _ func(context.Context, error)) []dogerhttp.Option {
			return []dogerhttp.Option{dogerhttp.WithErrorHandler(record)}
		}, calls: 1},
		{name: "none", opts: func(_, _ func(context.Context, error)) []dogerhttp.Option { return nil }},
		{name: "nil", opts: func(_, _ func(context.Context, error)) []dogerhttp.Option {
			return []dogerhttp.Option{dogerhttp.WithErrorHandler(nil)}
		}},
		{name: "last_wins", opts: func(record, other func(context.Context, error)) []dogerhttp.Option {
			return []dogerhttp.Option{dogerhttp.WithErrorHandler(other), dogerhttp.WithErrorHandler(record)}
		}, calls: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var record, other webuitest.ErrorRecorder
			failing := defaultRenderer
			failing.serviceList = func(string, doger.Injector, ...dogerhttp.Option) (string, error) {
				return "", errors.New(webuitest.RenderFailure)
			}
			handler := use(webuitest.BasePath, doger.New(), failing, tt.opts(record.Record, other.Record))

			req := webuitest.WithRequestValue(webuitest.NewRequest(t, http.MethodGet, webuitest.BasePath+"/service"))
			resp := webuitest.Do(t, webuitest.HandlerDoer(handler), req)

			webuitest.AssertGeneric500(t, resp)
			webuitest.AssertRecorded(t, &record, tt.calls)
			webuitest.AssertRecorded(t, &other, 0)
		})
	}
}

func TestWebUI_RootBasePath(t *testing.T) {
	t.Parallel()
	webuitest.RootBasePath(t, func(t *testing.T, root doger.Injector) webuitest.Doer {
		t.Helper()

		return webuitest.HandlerDoer(Use("/", root))
	})
}

func TestWebUI_RedirectsStayUnderBasePath(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		webuitest.BasePath + "//scope",
		webuitest.BasePath + "/../x",
		webuitest.BasePath + "X",
	} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			resp := webuitest.Get(t, mount(t, doger.New()), path)
			if resp.StatusCode >= 300 && resp.StatusCode < 400 {
				assert.True(t, strings.HasPrefix(resp.Header.Get("Location"), webuitest.BasePath+"/"),
					"%d redirect to %q leaves the base path", resp.StatusCode, resp.Header.Get("Location"))
				return
			}

			assert.Equal(t, http.StatusNotFound, resp.StatusCode)
		})
	}
}

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func TestWebUI_BehindAuthMiddleware(t *testing.T) {
	t.Parallel()
	webuitest.BehindAuthMiddleware(t, func(t *testing.T, root doger.Injector) webuitest.Doer {
		t.Helper()

		return webuitest.HandlerDoer(webuitest.BasicAuth(Use(webuitest.BasePath, root)))
	})
}

func TestWebUI_ConcurrentViewing(t *testing.T) {
	t.Parallel()
	webuitest.ConcurrentViewing(t, mount)
}

func TestWebUI_LayeredGraphBounded(t *testing.T) {
	t.Parallel()
	webuitest.LayeredGraphBounded(t, mount)
}
