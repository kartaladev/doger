package main

import (
	"github.com/gofiber/fiber/v2"
	dogerfiber "github.com/kartaladev/doger/http/fiber"
)

func main() {
	injector := startProgram()

	router := fiber.New()
	dogerfiber.Use(router.Group("/debug/do"), "/debug/do", injector)

	router.Listen(":8080")
}
