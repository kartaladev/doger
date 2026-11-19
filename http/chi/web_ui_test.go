package dogerchi

import (
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/kartaladev/doger"
	"github.com/kartaladev/doger/http/internal/webuitest"
)

// mount mounts the UI for root at webuitest.BasePath on a chi router.
func mount(t *testing.T, root doger.Injector) webuitest.Doer {
	t.Helper()

	r := chi.NewRouter()
	Use(r, webuitest.BasePath, root)

	return webuitest.HandlerDoer(r)
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
