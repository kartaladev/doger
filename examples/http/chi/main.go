package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	dogerchi "github.com/kartaladev/doger/http/chi"
)

func main() {
	injector := startProgram()

	router := chi.NewRouter()
	dogerchi.Use(router, "/debug/do", injector)

	http.ListenAndServe(":8080", router)
}
