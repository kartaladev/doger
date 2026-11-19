package dogergin_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	"github.com/kartaladev/doger"
	dogergin "github.com/kartaladev/doger/http/gin"
)

// Pass your middleware to the group the UI is mounted on.
func ExampleUse() {
	gin.SetMode(gin.ReleaseMode)

	injector := doger.New()
	doger.ProvideNamedValue(injector, "db", "postgres://localhost")

	r := gin.New()
	auth := gin.BasicAuth(gin.Accounts{"admin": "s3cret"})
	dogergin.Use(r.Group("/debug/doger", auth), injector)

	anonymous := httptest.NewRecorder()
	r.ServeHTTP(anonymous, httptest.NewRequest(http.MethodGet, "/debug/doger/service", http.NoBody))

	req := httptest.NewRequest(http.MethodGet, "/debug/doger/service", http.NoBody)
	req.SetBasicAuth("admin", "s3cret")
	admin := httptest.NewRecorder()
	r.ServeHTTP(admin, req)

	fmt.Println(anonymous.Code, admin.Code)
	// Output: 401 200
}
