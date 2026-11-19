package dogerhttpstd

import (
	"net/http"
	"testing"

	"github.com/kartaladev/doger"
	"github.com/kartaladev/doger/http/internal/webuitest"
)

// mount mounts the UI for root at webuitest.BasePath on a ServeMux.
func mount(t *testing.T, root doger.Injector) webuitest.Doer {
	t.Helper()

	mux := http.NewServeMux()
	mux.Handle(webuitest.BasePath+"/", Use(webuitest.BasePath, root))

	return webuitest.HandlerDoer(mux)
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
