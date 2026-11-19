package doger

import (
	"fmt"
	"time"

	"github.com/kartaladev/doger/stacktrace"
)

type explainTestService struct {
	Name  string
	Value int
}

func (s *explainTestService) HealthCheck() error {
	return nil
}

func (s *explainTestService) Shutdown() error {
	return nil
}

func newExplainTestService(i Injector) (*explainTestService, error) {
	return &explainTestService{
		Name:  "explain-test-service",
		Value: MustInvokeNamed[int](i, "value"),
	}, nil
}

func ExampleExplainService() {
	injector := New()

	ProvideNamedValue(injector, "value", 42)
	Provide(injector, newExplainTestService)
	_, _ = Invoke[*explainTestService](injector)

	explanation, found := ExplainService[*explainTestService](injector)

	// to prevent flakiness
	explanation.ScopeID = "980e2f60-d340-4776-86dd-6aa1d3c27860"
	explanation.ServiceBuildTime = 10 * time.Millisecond
	explanation.Invoked = &stacktrace.Frame{
		Function: "newExplainTestService",
		File:     "di_explain_example_test.go", // remove absolute path
		Line:     42,
	}

	fmt.Println(found)
	fmt.Println(explanation.ServiceName)
	fmt.Println(explanation.ServiceType)
	fmt.Println(explanation.String())
	// Output:
	// true
	// *github.com/kartaladev/doger.explainTestService
	// lazy
	//
	// Scope ID: 980e2f60-d340-4776-86dd-6aa1d3c27860
	// Scope name: [root]
	//
	// Service name: *github.com/kartaladev/doger.explainTestService
	// Service type: lazy
	// Service build time: 10ms
	// Invoked: di_explain_example_test.go:newExplainTestService:42
	//
	// Dependencies:
	// * value from scope [root]
	//
	// Dependents:
}

func ExampleExplainNamedService() {
	injector := New()

	ProvideNamedValue(injector, "value", 42)
	ProvideNamed(injector, "my-service", newExplainTestService)
	// "api" depends on "value" directly and through "my-service"
	ProvideNamed(injector, "api", func(i Injector) (string, error) {
		_ = MustInvokeNamed[*explainTestService](i, "my-service")
		return fmt.Sprint(MustInvokeNamed[int](i, "value")), nil
	})
	_, _ = InvokeNamed[string](injector, "api")

	explanation, found := ExplainNamedService(injector, "api")

	// to prevent flakiness
	explanation.ScopeID = "980e2f60-d340-4776-86dd-6aa1d3c27860"
	explanation.ServiceBuildTime = 10 * time.Millisecond
	explanation.Invoked = &stacktrace.Frame{
		Function: "ExampleExplainNamedService.func1",
		File:     "di_explain_example_test.go", // remove absolute path
		Line:     76,
	}

	fmt.Println(found)
	fmt.Println(explanation.ServiceName)
	fmt.Println(explanation.ServiceType)
	// "value" is expanded once: its second occurrence is a reference
	fmt.Println(explanation.String())
	// Output:
	// true
	// api
	// lazy
	//
	// Scope ID: 980e2f60-d340-4776-86dd-6aa1d3c27860
	// Scope name: [root]
	//
	// Service name: api
	// Service type: lazy
	// Service build time: 10ms
	// Invoked: di_explain_example_test.go:ExampleExplainNamedService.func1:76
	//
	// Dependencies:
	// * my-service from scope [root]
	//   * value from scope [root]
	// * value from scope [root] (ref)
	//
	// Dependents:
}

func ExampleExplainInjector() {
	injector := New()

	ProvideNamedValue(injector, "value", 42)
	ProvideNamed(injector, "service1", newExplainTestService)
	ProvideNamedValue(injector, "service2", "value")

	explanation := ExplainInjector(injector)
	fmt.Println(explanation.ScopeName)
	fmt.Println(len(explanation.DAG))
	// Output:
	// [root]
	// 1
}
