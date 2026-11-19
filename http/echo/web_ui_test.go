package dogerecho

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"

	"github.com/kartaladev/doger"
	dogerhttp "github.com/kartaladev/doger/http"
	"github.com/kartaladev/doger/http/internal/webuitest"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mount mounts the UI for root on the echo group webuitest.BasePath.
func mount(t *testing.T, root doger.Injector) webuitest.Doer {
	t.Helper()

	e := echo.New()
	Use(e.Group(webuitest.BasePath), webuitest.BasePath, root)

	return webuitest.HandlerDoer(e)
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
	c := echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/", http.NoBody), rec)
	require.NoError(t, response(c, "", errors.New(webuitest.RenderFailure), nil))

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.NotContains(t, rec.Body.String(), webuitest.RenderFailurePath)
	assert.Equal(t, webuitest.NoSniff, rec.Header().Get("X-Content-Type-Options"))
}

// mountWith mounts the UI for root on the echo group webuitest.BasePath with opts.
func mountWith(opts ...dogerhttp.Option) webuitest.Mount {
	return func(t *testing.T, root doger.Injector) webuitest.Doer {
		t.Helper()

		e := echo.New()
		Use(e.Group(webuitest.BasePath), webuitest.BasePath, root, opts...)

		return webuitest.HandlerDoer(e)
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
	webuitest.SourcePathDisplay(t, mount, mountWith(dogerhttp.WithFullSourcePaths()), source, "http/echo/web_ui_test.go")
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
			handler := echo.New()
			use(handler.Group(webuitest.BasePath), webuitest.BasePath, doger.New(), failing, tt.opts(record.Record, other.Record))

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

		e := echo.New()
		Use(e.Group(""), "/", root)

		return webuitest.HandlerDoer(e)
	})
}
