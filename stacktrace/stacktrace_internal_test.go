package stacktrace

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSkipFrame_Layouts(t *testing.T) {
	t.Parallel()

	const goroot = "/usr/local/go"

	tests := []struct {
		name     string
		lib      string
		function string
		file     string
		skipped  bool
	}{
		{name: "own_checkout_do", lib: "github.com/samber/do/v2", function: "github.com/samber/do/v2/stacktrace.NewFrameFromCaller", file: "/home/u/src/do/stacktrace/stacktrace.go", skipped: true},
		{name: "own_checkout_doger", lib: "github.com/samber/do/v2", function: "github.com/samber/do/v2/stacktrace.NewFrameFromCaller", file: "/home/u/src/doger/stacktrace/stacktrace.go", skipped: true},
		{name: "own_checkout_my_fork", lib: "github.com/samber/do/v2", function: "github.com/samber/do/v2/stacktrace.NewFrameFromCaller", file: "/home/u/src/my-fork/stacktrace/stacktrace.go", skipped: true},
		{name: "own_module_cache", lib: "github.com/samber/do/v2", function: "github.com/samber/do/v2/stacktrace.NewFrameFromCaller", file: "/home/u/go/pkg/mod/github.com/samber/do/v2@v2.1.0/stacktrace/stacktrace.go", skipped: true},
		{name: "own_module_cache_renamed", lib: "github.com/kartaladev/doger", function: "github.com/kartaladev/doger/stacktrace.NewFrameFromCaller", file: "/home/u/go/pkg/mod/github.com/kartaladev/doger@v0.1.0/stacktrace/stacktrace.go", skipped: true},
		{name: "own_vendored", lib: "github.com/samber/do/v2", function: "github.com/samber/do/v2/stacktrace.NewFrameFromCaller", file: "/home/u/app/vendor/github.com/samber/do/v2/stacktrace/stacktrace.go", skipped: true},
		{name: "own_closure", lib: "github.com/samber/do/v2", function: "github.com/samber/do/v2/stacktrace.NewFrameFromCaller.func1", file: "/home/u/src/doger/stacktrace/stacktrace.go", skipped: true},
		{name: "own_test_helper_doger", lib: "github.com/samber/do/v2", function: "github.com/samber/do/v2/stacktrace.example1", file: "/home/u/src/doger/stacktrace/stacktrace_test.go", skipped: false},
		{name: "own_test_helper_my_fork", lib: "github.com/samber/do/v2", function: "github.com/samber/do/v2/stacktrace.example1", file: "/home/u/src/my-fork/stacktrace/stacktrace_test.go", skipped: false},
		{name: "lib_generic_func", lib: "github.com/samber/do/v2", function: "github.com/samber/do/v2.InvokeNamed[...]", file: "/home/u/src/doger/di.go", skipped: false},
		{name: "lib_generic_method", lib: "github.com/samber/do/v2", function: "github.com/samber/do/v2.(*serviceLazy[...]).getInstance", file: "/home/u/src/doger/service_lazy.go", skipped: false},
		{name: "external_test_package", lib: "github.com/samber/do/v2", function: "github.com/samber/do/v2/stacktrace_test.TestCaller", file: "/home/u/src/doger/stacktrace/caller_test.go", skipped: false},
		{name: "lookalike_palantir", lib: "github.com/samber/do/v2", function: "github.com/palantir/stacktrace.Propagate", file: "/home/u/go/pkg/mod/github.com/palantir/stacktrace@v0.0.0/stacktrace.go", skipped: false},
		{name: "lookalike_acme_do", lib: "github.com/samber/do/v2", function: "github.com/acme/do/stacktrace.Capture", file: "/home/u/src/acme/do/stacktrace/capture.go", skipped: false},
		{name: "sibling_stacktracex", lib: "github.com/samber/do/v2", function: "github.com/samber/do/v2/stacktracex.Helper", file: "/home/u/src/doger/stacktracex/helper.go", skipped: false},
		{name: "consumer_main", lib: "github.com/samber/do/v2", function: "main.main", file: "/home/u/app/main.go", skipped: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			is := assert.New(t)

			is.Equal(tt.skipped, skipFrame(tt.lib+"/stacktrace", goroot, tt.function, tt.file))
		})
	}
}
