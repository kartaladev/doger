package main

import (
	"log"
	"net/http"
	"time"

	dogerecho "github.com/kartaladev/doger/http/echo"
	"github.com/labstack/echo/v4"
)

func main() {
	injector := startProgram()

	router := echo.New()
	dogerecho.Use(router.Group("/debug/do"), "/debug/do", injector)

	// a server with a read-header timeout, so slow clients cannot hold connections
	server := &http.Server{Addr: ":8080", Handler: router, ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(server.ListenAndServe())
}
