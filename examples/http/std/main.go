package main

import (
	"net/http"

	dogerhttpstd "github.com/kartaladev/doger/http/std"
)

func main() {
	injector := startProgram()

	mux := http.NewServeMux()
	mux.Handle("/debug/do/", dogerhttpstd.Use("/debug/do", injector))

	http.ListenAndServe(":8080", mux)
}
