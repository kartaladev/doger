package main

import (
	"fmt"
	"time"

	"github.com/kartaladev/doger"
)

func main() {
	injector := doger.New()

	fmt.Println("=== Transient Services Example ===")
	fmt.Println("Transient services are recreated each time they are requested")
	fmt.Println("This is useful for request-scoped services that should not share state")
	fmt.Printf("between different requests or operations.\n\n")

	// Step 1: Register RequestID (transient)
	fmt.Println("Step 1: Registering RequestID (transient)")
	doger.ProvideTransient(injector, func(i doger.Injector) (*RequestID, error) {
		return &RequestID{
			ID:        fmt.Sprintf("req-%d", time.Now().UnixNano()),
			CreatedAt: time.Now(),
		}, nil
	})

	// Step 2: Register RequestContext (transient with dependency)
	fmt.Println("Step 2: Registering RequestContext (transient with RequestID dependency)")
	doger.ProvideTransient(injector, func(i doger.Injector) (*RequestContext, error) {
		return &RequestContext{
			ID:      doger.MustInvoke[*RequestID](i),
			UserID:  "user-123",    // In real app, this would come from request
			Session: "session-456", // In real app, this would come from request
		}, nil
	})

	// Step 3: Register Business Services (transient)
	fmt.Println("Step 3: Registering Business Services (transient)")
	doger.ProvideTransient(injector, func(i doger.Injector) (*UserService, error) {
		return &UserService{
			Context: doger.MustInvoke[*RequestContext](i),
		}, nil
	})

	doger.ProvideTransient(injector, func(i doger.Injector) (*OrderService, error) {
		return &OrderService{
			Context: doger.MustInvoke[*RequestContext](i),
		}, nil
	})

	doger.ProvideTransient(injector, func(i doger.Injector) (*PaymentService, error) {
		return &PaymentService{
			Context: doger.MustInvoke[*RequestContext](i),
		}, nil
	})

	// Step 4: Register OrderProcessor (transient with multiple dependencies)
	fmt.Println("Step 4: Registering OrderProcessor (transient with multiple dependencies)")
	doger.ProvideTransient(injector, func(i doger.Injector) (*OrderProcessor, error) {
		return &OrderProcessor{
			UserService:    doger.MustInvoke[*UserService](i),
			OrderService:   doger.MustInvoke[*OrderService](i),
			PaymentService: doger.MustInvoke[*PaymentService](i),
		}, nil
	})

	// Step 5: Register RequestHandler (transient)
	fmt.Println("Step 5: Registering RequestHandler (transient)")
	doger.ProvideTransient(injector, func(i doger.Injector) (*RequestHandler, error) {
		return &RequestHandler{
			Processor: doger.MustInvoke[*OrderProcessor](i),
		}, nil
	})

	fmt.Println("\n=== Simulating Multiple Requests ===")
	fmt.Println("Each request gets completely fresh instances of all services")

	// Simulate multiple requests - each gets fresh instances
	for i := 1; i <= 3; i++ {
		handler := doger.MustInvoke[*RequestHandler](injector)
		handler.HandleRequest(fmt.Sprintf("user-%d", i), fmt.Sprintf("session-%d", i))
		time.Sleep(100 * time.Millisecond) // Small delay to see different timestamps
	}

	fmt.Println("\n=== Demonstrating Instance Uniqueness ===")
	fmt.Println("Notice how each RequestID has a unique timestamp:")

	// Access the request IDs to show they're different
	req1 := doger.MustInvoke[*RequestContext](injector)
	req2 := doger.MustInvoke[*RequestContext](injector)

	fmt.Printf("Request 1 ID: %s, Created: %s\n", req1.ID.ID, req1.ID.CreatedAt.Format("15:04:05.000"))
	fmt.Printf("Request 2 ID: %s, Created: %s\n", req2.ID.ID, req2.ID.CreatedAt.Format("15:04:05.000"))

	fmt.Println("\n=== Transient Services Benefits ===")
	fmt.Println("1. Request isolation - no shared state between requests")
	fmt.Println("2. Thread safety - each request has its own instances")
	fmt.Println("3. Predictable behavior - no side effects from previous requests")
	fmt.Println("4. Memory efficiency - instances are garbage collected after use")
}
