package dogergocron_test

import (
	"fmt"

	"github.com/go-co-op/gocron/v2"
	"github.com/jonboulle/clockwork"
	"github.com/kartaladev/doger"
	dogerclockwork "github.com/kartaladev/doger/clock/clockwork"
	dogergocron "github.com/kartaladev/doger/scheduler/gocron"
)

type database struct{}

func (database) HealthCheck() error { return nil }

func ExampleNew() {
	// Defaults: a round at once, then every 30s, singleton mode.
	scheduler, err := dogergocron.New()
	if err != nil {
		panic(err)
	}

	// The rounds run on the container clock; a wrapped clockwork fake clock
	// lets a test drive them with fake.Advance. A nil Clock is the real
	// clock.
	fake := clockwork.NewFakeClock()
	injector := doger.NewWithOpts(&doger.InjectorOpts{
		Clock:           dogerclockwork.Wrap(fake),
		HealthScheduler: scheduler,
	})
	doger.ProvideNamedValue(injector, "db", database{})
	if err := injector.Start(); err != nil {
		panic(err)
	}
	defer injector.Shutdown()

	fmt.Println("monitored:", injector.HealthReport().Monitored)

	// Output:
	// monitored: true
}

func ExampleWithJobDefinition() {
	// A round every 5 minutes, on the minute; the first one still runs at
	// once (see WithStartAt).
	scheduler, err := dogergocron.New(dogergocron.WithJobDefinition(gocron.CronJob("*/5 * * * *", false)))
	if err != nil {
		panic(err)
	}

	injector := doger.NewWithOpts(&doger.InjectorOpts{HealthScheduler: scheduler})
	doger.ProvideNamedValue(injector, "db", database{})
	if err := injector.Start(); err != nil {
		panic(err)
	}
	defer injector.Shutdown()

	fmt.Println("monitored:", injector.HealthReport().Monitored)

	// Output:
	// monitored: true
}
