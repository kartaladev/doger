package dogergin

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/kartaladev/doger"
	"github.com/kartaladev/doger/http/internal/webuitest"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// mount mounts the UI for root on the gin group webuitest.BasePath.
func mount(t *testing.T, root doger.Injector) webuitest.Doer {
	t.Helper()

	r := gin.New()
	Use(r.Group(webuitest.BasePath), root)

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
