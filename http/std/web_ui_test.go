package dogerhttpstd

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kartaladev/doger"
	"github.com/kartaladev/doger/http/internal/webuitest"
	"github.com/stretchr/testify/assert"
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
	response(rec, nil, errors.New(webuitest.RenderFailure))

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.NotContains(t, rec.Body.String(), webuitest.RenderFailurePath)
	assert.Equal(t, webuitest.NoSniff, rec.Header().Get("X-Content-Type-Options"))
}
