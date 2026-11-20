package main

import (
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	dogergin "github.com/kartaladev/doger/http/gin"
)

func main() {
	injector := startProgram()

	router := gin.New()
	dogergin.Use(router.Group("/debug/do"), injector)

	// a server with a read-header timeout, so slow clients cannot hold connections
	server := &http.Server{Addr: ":8088", Handler: router, ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(server.ListenAndServe())
}
