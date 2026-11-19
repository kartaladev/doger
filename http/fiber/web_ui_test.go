package dogerfiber

import (
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/kartaladev/doger"
	"github.com/kartaladev/doger/http/internal/webuitest"
	"github.com/stretchr/testify/require"
)

// appDoer returns a Doer that serves each request with app.
func appDoer(t *testing.T, app *fiber.App) webuitest.Doer {
	t.Helper()

	return func(req *http.Request) *http.Response {
		resp, err := app.Test(req, -1)
		require.NoError(t, err)

		return resp
	}
}

// mount mounts the UI for root on the fiber group webuitest.BasePath.
func mount(t *testing.T, root doger.Injector) webuitest.Doer {
	t.Helper()

	app := fiber.New()
	Use(app.Group(webuitest.BasePath), webuitest.BasePath, root)

	return appDoer(t, app)
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
