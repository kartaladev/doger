package main

import (
	"fmt"

	"github.com/kartaladev/doger"
)

func main() {
	injector := doger.New()

	fmt.Println("=== Event-Driven Architecture Example ===")
	fmt.Println("This example demonstrates how to build an event-driven system")
	fmt.Printf("using the do library for dependency injection.\n\n")

	// Step 1: Register core infrastructure services
	fmt.Println("Step 1: Registering core infrastructure services")
	doger.Provide(injector, func(i doger.Injector) (*Logger, error) {
		return &Logger{Level: "INFO"}, nil
	})

	doger.Provide(injector, func(i doger.Injector) (*EventBus, error) {
		return NewEventBus(), nil
	})

	// Step 2: Register event handlers
	fmt.Println("Step 2: Registering event handlers")
	doger.Provide(injector, func(i doger.Injector) (*UserEventHandler, error) {
		return &UserEventHandler{
			Logger: doger.MustInvoke[*Logger](i),
		}, nil
	})

	doger.Provide(injector, func(i doger.Injector) (*OrderEventHandler, error) {
		return &OrderEventHandler{
			Logger: doger.MustInvoke[*Logger](i),
		}, nil
	})

	doger.Provide(injector, func(i doger.Injector) (*PaymentEventHandler, error) {
		return &PaymentEventHandler{
			Logger: doger.MustInvoke[*Logger](i),
		}, nil
	})

	// Step 3: Register business services that publish events
	fmt.Println("Step 3: Registering business services that publish events")
	doger.Provide(injector, func(i doger.Injector) (*UserService, error) {
		return &UserService{
			EventBus: doger.MustInvoke[*EventBus](i),
			Logger:   doger.MustInvoke[*Logger](i),
		}, nil
	})

	doger.Provide(injector, func(i doger.Injector) (*OrderService, error) {
		return &OrderService{
			EventBus: doger.MustInvoke[*EventBus](i),
			Logger:   doger.MustInvoke[*Logger](i),
		}, nil
	})

	doger.Provide(injector, func(i doger.Injector) (*PaymentService, error) {
		return &PaymentService{
			EventBus: doger.MustInvoke[*EventBus](i),
			Logger:   doger.MustInvoke[*Logger](i),
		}, nil
	})

	// Step 4: Register main application
	fmt.Println("Step 4: Registering main application")
	doger.Provide(injector, func(i doger.Injector) (*Application, error) {
		return &Application{
			EventBus:       doger.MustInvoke[*EventBus](i),
			UserService:    doger.MustInvoke[*UserService](i),
			OrderService:   doger.MustInvoke[*OrderService](i),
			PaymentService: doger.MustInvoke[*PaymentService](i),
			Logger:         doger.MustInvoke[*Logger](i),
		}, nil
	})

	fmt.Println("\n=== Setting up Event Handlers ===")
	fmt.Println("Subscribing event handlers to the event bus:")

	// Get the event bus and register handlers
	eventBus := doger.MustInvoke[*EventBus](injector)
	userHandler := doger.MustInvoke[*UserEventHandler](injector)
	orderHandler := doger.MustInvoke[*OrderEventHandler](injector)
	paymentHandler := doger.MustInvoke[*PaymentEventHandler](injector)

	// Subscribe handlers to events
	fmt.Println("  - UserEventHandler subscribed to 'user.created' events")
	eventBus.Subscribe(userHandler)

	fmt.Println("  - OrderEventHandler subscribed to 'order.created' events")
	eventBus.Subscribe(orderHandler)

	fmt.Println("  - PaymentEventHandler subscribed to 'payment.processed' events")
	eventBus.Subscribe(paymentHandler)

	fmt.Println("Event handlers registered successfully")

	fmt.Println("\n=== Service Information ===")
	fmt.Println("Available services:", injector.ListProvidedServices())

	// Run the application
	fmt.Println("\n=== Running Application ===")
	fmt.Println("Simulating a complete user journey with event publishing:")
	app := doger.MustInvoke[*Application](injector)
	app.Run()

	fmt.Println("\n=== Event-Driven Architecture Benefits ===")
	fmt.Println("1. Loose coupling - services don't directly depend on each other")
	fmt.Println("2. Scalable event processing - easy to add new handlers")
	fmt.Println("3. Asynchronous processing - events can be processed independently")
	fmt.Println("4. Clear separation of concerns - publishers vs subscribers")
	fmt.Println("5. Testability - easy to mock event bus for testing")
	fmt.Println("6. Extensibility - new features can be added via event handlers")
}
