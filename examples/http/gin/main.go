package main

import (
	"github.com/gin-gonic/gin"
	dogergin "github.com/kartaladev/doger/http/gin"
)

func main() {
	injector := startProgram()

	router := gin.New()
	dogergin.Use(router.Group("/debug/do"), injector)

	router.Run(":8088")
}
