package doger

import (
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
