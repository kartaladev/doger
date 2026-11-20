package doger

import (
	"encoding/json"
	"errors"
	"fmt"
)

func ExampleServiceError() {
	root := New()

	// "api" depends on "db", which is not registered.
	ProvideNamed(root, "api", func(i Injector) (string, error) {
		db, err := InvokeNamed[string](i, "db")
		return "api on " + db, err
	})

	_, err := InvokeNamed[string](root, "api")

	// errors.As finds the outermost service whose build failed.
	var serviceErr *ServiceError
	if errors.As(err, &serviceErr) {
		fmt.Printf("failing service: %s in %s\n", serviceErr.Service, serviceErr.Scope)
	}

	// errors.Is still matches the cause.
	fmt.Println("missing dependency:", errors.Is(err, ErrServiceNotFound))

	// A requested name that is not registered at all is not a ServiceError.
	_, err = InvokeNamed[string](root, "cache")
	fmt.Println("cache is a ServiceError:", errors.As(err, &serviceErr))

	// Output:
	// failing service: api in [root]
	// missing dependency: true
	// cache is a ServiceError: false
}

// exampleClosingPool fails to close.
type exampleClosingPool struct{}

func (exampleClosingPool) Shutdown() error { return errors.New("closing failed") }

func ExampleShutdownReport_Err() {
	injector := New()
	ProvideNamedValue(injector, "pool", exampleClosingPool{})

	if err := injector.Shutdown().Err(); err != nil {
		fmt.Println("shutdown failed:", err)
	}
	// Output:
	// shutdown failed: doger: service "pool" in scope "[root]": closing failed
}

func ExampleShutdownReport_MarshalJSON() {
	injector := New()
	ProvideNamedValue(injector, "pool", exampleClosingPool{})

	report := injector.Shutdown()
	report.ShutdownTime = 0 // fixed for the example
	out, _ := json.Marshal(report)
	fmt.Println(string(out))
	// Output:
	// {"succeed":false,"shutdownTime":"0s","services":[{"scope":"[root]","service":"pool"}],"errors":[{"scope":"[root]","service":"pool","error":"closing failed"}]}
}
