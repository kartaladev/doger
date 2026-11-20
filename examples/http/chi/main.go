package main

import (
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	dogerchi "github.com/kartaladev/doger/http/chi"
)

func main() {
	injector := startProgram()

	router := chi.NewRouter()
	dogerchi.Use(router, "/debug/do", injector)

	// a server with a read-header timeout, so slow clients cannot hold connections
	server := &http.Server{Addr: ":8080", Handler: router, ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(server.ListenAndServe())
}
