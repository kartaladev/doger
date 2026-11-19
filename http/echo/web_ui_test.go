package dogerecho

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kartaladev/doger"
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
	require.NoError(t, response(c, "", errors.New(webuitest.RenderFailure)))

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.NotContains(t, rec.Body.String(), webuitest.RenderFailurePath)
	assert.Equal(t, webuitest.NoSniff, rec.Header().Get("X-Content-Type-Options"))
}
