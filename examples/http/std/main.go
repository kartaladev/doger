package main

import (
	"log"
	"net/http"
	"time"

	dogerhttpstd "github.com/kartaladev/doger/http/std"
)

func main() {
	injector := startProgram()

	mux := http.NewServeMux()
	mux.Handle("/debug/do/", dogerhttpstd.Use("/debug/do", injector))

	// a server with a read-header timeout, so slow clients cannot hold connections
	server := &http.Server{Addr: ":8080", Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(server.ListenAndServe())
}
