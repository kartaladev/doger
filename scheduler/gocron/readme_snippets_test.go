package dogergocron_test

import (
	"testing"

	"github.com/kartaladev/doger"
	dogergocron "github.com/kartaladev/doger/scheduler/gocron"
)

// readmeScheduledHealth is the scheduled health snippet of the root
// README.md, compiled; the last line is not in the README.
func readmeScheduledHealth() error {
	scheduler, err := dogergocron.New() // a round at once, then every 30s
	if err != nil {
		return err
	}
	injector := doger.NewWithOpts(&doger.InjectorOpts{HealthScheduler: scheduler})
	injector.Shutdown()

	return nil
}

func TestReadme_ScheduledHealth(t *testing.T) {
	t.Parallel()

	if err := readmeScheduledHealth(); err != nil {
		t.Fatal(err)
	}
}
