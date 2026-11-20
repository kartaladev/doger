package doger

import (
	"log/slog"
	"os"
)

// A Logger at Info level shows the container's lifecycle; registrations and
// invocations are Debug records. The time attribute is dropped here only to
// keep the output stable.
func ExampleInjectorOpts_logger() {
	handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
		ReplaceAttr: func(groups []string, attr slog.Attr) slog.Attr {
			if len(groups) == 0 && attr.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return attr
		},
	})
	injector := NewWithOpts(&InjectorOpts{Logger: slog.New(handler)})

	ProvideNamedValue(injector, "PG_URI", "postgres://user:pass@host:5432/db")
	MustInvokeNamed[string](injector, "PG_URI")
	_ = injector.Start()
	_ = injector.Shutdown()

	// Output:
	// level=INFO msg="container started" scope=[root]
	// level=INFO msg="container ready" scope=[root]
	// level=INFO msg="container closing" scope=[root]
	// level=INFO msg="container closed" scope=[root]
}
