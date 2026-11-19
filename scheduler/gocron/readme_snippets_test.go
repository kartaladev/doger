package dogergocron_test

import (
	"testing"

	"github.com/kartaladev/doger"
	dogergocron "github.com/kartaladev/doger/scheduler/gocron"
)

// readmeScheduledHealth is the scheduled health snippet of the root
// README.md, compiled: the text between the readme markers is the README
// block (see TestReadme_SnippetsCompiled in the root module).
func readmeScheduledHealth() error {
	// readme:gocron
	scheduler, err := dogergocron.New() // a round at once, then every 30s
	if err != nil {
		return err
	}
	injector := doger.NewWithOpts(&doger.InjectorOpts{HealthScheduler: scheduler})
	// readme:end

	return injector.Shutdown().Err()
}

func TestReadme_ScheduledHealth(t *testing.T) {
	t.Parallel()

	if err := readmeScheduledHealth(); err != nil {
		t.Fatal(err)
	}
}
