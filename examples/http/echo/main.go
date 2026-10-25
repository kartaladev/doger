package main

import (
	dogerecho "github.com/kartaladev/doger/http/echo"
	"github.com/labstack/echo/v4"
)

func main() {
	injector := startProgram()

	router := echo.New()
	dogerecho.Use(router.Group("/debug/do"), "/debug/do", injector)

	router.Start(":8080")
}
