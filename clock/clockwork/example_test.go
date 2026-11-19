package dogerclockwork_test

import (
	"context"
	"fmt"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/kartaladev/doger"
	dogerclockwork "github.com/kartaladev/doger/clock/clockwork"
)

// exampleDatabase is a health check that hangs until released.
type exampleDatabase struct{ release chan struct{} }

func (db *exampleDatabase) HealthCheck() error {
	<-db.release
	return nil
}

func ExampleWrap() {
	// A clockwork fake clock lets a test reach a health-check timeout
	// without waiting for it.
	fake := clockwork.NewFakeClock()
	injector := doger.NewWithOpts(&doger.InjectorOpts{
		Clock:              dogerclockwork.Wrap(fake),
		HealthCheckTimeout: 5 * time.Second,
	})

	db := &exampleDatabase{release: make(chan struct{})}
	defer close(db.release)
	doger.ProvideNamedValue(injector, "db", db)

	result := make(chan map[string]error, 1)
	go func() { result <- injector.HealthCheck() }()

	_ = fake.BlockUntilContext(context.Background(), 1) // the 5s timeout is armed
	fake.Advance(5 * time.Second)

	fmt.Println((<-result)["db"])
	// Output:
	// doger: health check timeout: context deadline exceeded
}

func ExampleUnwrap() {
	fake := clockwork.NewFakeClock()

	clock, ok := dogerclockwork.Unwrap(dogerclockwork.Wrap(fake))
	fmt.Println(clock == fake, ok)

	clock, ok = dogerclockwork.Unwrap(doger.RealClock())
	fmt.Println(clock, ok)
	// Output:
	// true true
	// <nil> false
}
