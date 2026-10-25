package main

import (
	"fmt"

	"github.com/kartaladev/doger"
	"github.com/kartaladev/doger/examples/package-system/application"
	"github.com/kartaladev/doger/examples/package-system/database"
	"github.com/kartaladev/doger/examples/package-system/logging"
	"github.com/kartaladev/doger/examples/package-system/services"
)

// Configuration represents application configuration.
type Configuration struct {
	AppName string
	Port    int
	Debug   bool
}

func (c *Configuration) GetAppName() string {
	return c.AppName
}

func (c *Configuration) GetDebug() bool {
	return c.Debug
}

func main() {
	// Create injector with all packages
	injector := doger.New(
		// Configuration is provided directly
		func(i doger.Injector) {
			doger.ProvideValue(i, &Configuration{
				AppName: "MyApp",
				Port:    8080,
				Debug:   true,
			})
		},
		// Apply modular packages
		database.DatabasePackage,
		logging.LoggingPackage,
		services.ServicesPackage,
		application.ApplicationPackage,
	)

	// Create aliases for Configuration to bridge interface types
	// This is needed for doger.InvokeAs to work across packages
	_ = doger.As[*Configuration, database.Configuration](injector)
	_ = doger.As[*Configuration, logging.Configuration](injector)
	_ = doger.As[*Configuration, application.Configuration](injector)

	fmt.Println("=== Package System Example ===")
	fmt.Println("Available services:", injector.ListProvidedServices())

	// Run the application
	app := doger.MustInvoke[*application.Application](injector)
	app.Run()

	fmt.Println("\n=== Package Benefits ===")
	fmt.Println("1. Modular service registration")
	fmt.Println("2. Reusable service packages")
	fmt.Println("3. Clear separation of concerns")
	fmt.Println("4. Easy testing and mocking")
}
