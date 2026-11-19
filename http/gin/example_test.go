package dogergin_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	"github.com/kartaladev/doger"
	dogergin "github.com/kartaladev/doger/http/gin"
)

// debugAuth returns a basic-auth middleware for the debug UI. It refuses
// empty credentials, so an unset variable never opens the UI; gin.BasicAuth
// compares in constant time. The Web UI docs show this function.
func debugAuth(user, password string) (gin.HandlerFunc, error) {
	if user == "" || password == "" {
		return nil, errors.New("debug UI: set DEBUG_USER and DEBUG_PASSWORD")
	}

	return gin.BasicAuth(gin.Accounts{user: password}), nil
}

// Pass your middleware to the group the UI is mounted on.
func ExampleUse() {
	gin.SetMode(gin.ReleaseMode)

	injector := doger.New()
	doger.ProvideNamedValue(injector, "db", "postgres://localhost")

	auth, err := debugAuth("admin", "s3cret") // os.Getenv("DEBUG_USER"), os.Getenv("DEBUG_PASSWORD")
	if err != nil {
		panic(err)
	}

	r := gin.New()
	dogergin.Use(r.Group("/debug/doger", auth), injector)

	anonymous := httptest.NewRecorder()
	r.ServeHTTP(anonymous, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/debug/doger/service", http.NoBody))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/debug/doger/service", http.NoBody)
	req.SetBasicAuth("admin", "s3cret")
	admin := httptest.NewRecorder()
	r.ServeHTTP(admin, req)

	fmt.Println(anonymous.Code, admin.Code)
	// Output: 401 200
}

// Unset credentials refuse to mount the UI instead of opening it.
func ExampleUse_emptyCredentials() {
	_, err := debugAuth("", "")
	fmt.Println(err)
	// Output: debug UI: set DEBUG_USER and DEBUG_PASSWORD
}
