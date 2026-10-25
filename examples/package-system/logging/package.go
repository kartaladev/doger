package logging

import (
	"github.com/kartaladev/doger"
)

// LoggingPackage is the global package for logging-related services.
var LoggingPackage = doger.Package(
	doger.Lazy(func(i doger.Injector) (*Logger, error) {
		config := doger.MustInvokeAs[Configuration](i)
		level := "INFO"
		if config.GetDebug() {
			level = "DEBUG"
		}
		return &Logger{Level: level}, nil
	}),
)
