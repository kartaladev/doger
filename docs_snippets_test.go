package doger_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"regexp"
	"runtime"
	"testing"
	"time"

	"github.com/kartaladev/doger"
)

// The "after" code of docs/docs/migrating/migrating-from-samber-do.md,
// compiled. The text between a "// docs:<name>" marker and "// docs:end" is
// the doc's Go block preceded by <!-- snippet: <name> -->, character for
// character after dedenting (see TestDocs_MigrationSnippetsCompiled).

type Database interface{ Ping() error }

type Postgres struct{}

func (*Postgres) Ping() error { return nil }

func NewDatabase(doger.Injector) (*Postgres, error) { return &Postgres{}, nil }

// Server is the Starter of the README quickstart (readme_snippets_test.go).
func NewServer(doger.Injector) (*Server, error) { return &Server{}, nil }

func docsBasic() {
	// docs:basic
	injector := doger.New()
	doger.Provide(injector, NewDatabase)
	// docs:end
	_ = injector
}

func docsPanicNil() bool {
	// docs:panic-nil
	injector := doger.New()
	doger.Provide(injector, func(i doger.Injector) (*Postgres, error) {
		panic(nil)
	})

	_, err := doger.Invoke[*Postgres](injector)

	var panicNilErr *runtime.PanicNilError
	errors.As(err, &panicNilErr)           // true
	errors.Is(err, doger.ErrProviderPanic) // true
	// docs:end
	return errors.As(err, &panicNilErr) && errors.Is(err, doger.ErrProviderPanic)
}

func docsHealthEnded(ctx context.Context, injector *doger.RootScope) {
	// docs:health-ended
	err := doger.HealthCheckNamedWithContext(ctx, injector, "db")
	if errors.Is(err, context.Canceled) {
		fmt.Println("the check's context was cancelled")
	}
	// docs:end
}

func docsAlias() error {
	injector := doger.New()
	// docs:alias
	doger.Provide(injector, NewDatabase)
	if err := doger.Alias[*Postgres, Database](injector); err != nil {
		return err // matches doger.ErrServiceAlreadyDeclared for a taken name
	}
	// docs:end
	return nil
}

func docsNilInjector() bool {
	// docs:nil-injector
	injector := doger.New() // pass it explicitly; there is no package-level container
	doger.Provide(injector, NewDatabase)

	_, err := doger.Invoke[*Postgres](nil)
	errors.Is(err, doger.ErrNilInjector) // true
	// docs:end
	return errors.Is(err, doger.ErrNilInjector)
}

func docsLogger() {
	// docs:logger
	injector := doger.NewWithOpts(&doger.InjectorOpts{
		Logger: slog.Default(), // nil (the default) logs nothing
	})
	// docs:end
	_ = injector
}

func docsMaxFrames() {
	// docs:max-frames
	injector := doger.NewWithOpts(&doger.InjectorOpts{
		MaxInvocationFrames: 10, // 0: 100 frames; negative: none recorded
	})
	// docs:end
	_ = injector
}

func docsHooks() {
	// docs:hooks
	injector := doger.NewWithOpts(&doger.InjectorOpts{
		HookAfterInvocation: []func(scope *doger.Scope, serviceName string, elapsed time.Duration, err error){
			func(scope *doger.Scope, serviceName string, elapsed time.Duration, err error) {
				fmt.Println("invoked", serviceName, "in", elapsed)
			},
		},
		HookBeforeShutdown: []func(ctx context.Context, scope *doger.Scope, serviceName string){
			func(ctx context.Context, scope *doger.Scope, serviceName string) {
				fmt.Println("shutting down", serviceName)
			},
		},
		HookAfterShutdown: []func(ctx context.Context, scope *doger.Scope, serviceName string, elapsed time.Duration, err error){
			func(ctx context.Context, scope *doger.Scope, serviceName string, elapsed time.Duration, err error) {
				fmt.Println("shut down", serviceName, "in", elapsed, "err:", err)
			},
		},
	})
	// docs:end
	_ = injector
}

func docsHealthTimeout() {
	// docs:health-timeout
	injector := doger.NewWithOpts(&doger.InjectorOpts{
		HealthCheckTimeout: -1, // no bound; 0 (the default) means 10s
	})
	// docs:end
	_ = injector
}

func docsProviderPanic(injector *doger.RootScope, errBroken error) {
	// docs:provider-panic
	_, err := doger.Invoke[*Postgres](injector) // its provider panicked with errBroken
	if errors.Is(err, errBroken) {              // still true: the panic value is wrapped
		fmt.Println("broken:", err)
	}
	if serviceErr, ok := errors.AsType[*doger.ServiceError](err); ok {
		fmt.Println("failed service:", serviceErr.Service)
	}
	// docs:end
}

func docsProvideOptions() {
	injector := doger.New()
	// docs:provide-options
	doger.Provide(injector, NewServer, doger.AsStarter()) // a trailing option

	// a registration function stored as a value has the options parameter:
	// func(doger.Injector, doger.Provider[*Postgres], ...doger.ProvideOption)
	register := doger.Provide[*Postgres]
	register(injector, NewDatabase)
	// docs:end
}

func docsShutdownReport(injector *doger.RootScope) error {
	// docs:shutdown-report
	if err := injector.Shutdown().Err(); err != nil {
		return err // matches each service's error with errors.Is and errors.As
	}
	// docs:end
	return nil
}

func docsBoundedShutdown(injector *doger.RootScope) {
	// docs:bounded-shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	report := injector.ShutdownWithContext(ctx)
	if err := report.Err(); errors.Is(err, context.DeadlineExceeded) {
		// services still shutting down at the deadline keep running in the
		// background; the report names them
		fmt.Println(err)
	}
	// docs:end
}

func docsDuplicateScope(injector *doger.RootScope) {
	// docs:duplicate-scope
	defer func() {
		if err, ok := recover().(error); ok && errors.Is(err, doger.ErrScopeAlreadyDeclared) {
			fmt.Println(err) // doger: scope `tenant` has already been declared
		}
	}()
	injector.Scope("tenant")
	injector.Scope("tenant")
	// docs:end
}

func docsAfterShutdown(injector *doger.RootScope) {
	// docs:after-shutdown
	injector.Shutdown()

	_, err := doger.Invoke[*Postgres](injector)
	errors.Is(err, doger.ErrShutdown) // true: build a new container instead
	// docs:end
}

func docsStarter() {
	// docs:starter
	injector := doger.New()
	doger.Provide(injector, NewServer, doger.AsStarter()) // started by injector.Start()

	// or start every registered type with a Start method, as before:
	auto := doger.NewWithOpts(&doger.InjectorOpts{EnableStarterAutoDetection: true})
	// docs:end
	_, _ = injector, auto
}

func docsPrerelease(root *doger.RootScope, ext doger.Extension) {
	// docs:prerelease
	_ = doger.Extend(root, ext, doger.ExtendIntoRoot()) // was Flatten()

	doger.Runnable().Handle(root, doger.StageAfterStart, func(ctx context.Context, c doger.Component[doger.Runner, struct{}]) error {
		return c.Instance.Run(ctx) // was Runnable.Handle
	})

	_ = root.HealthReport(doger.MatchHealthTags(doger.HealthTagReadiness), doger.IgnoreContainerPhase())

	errors.Is(root.Start(), doger.ErrShutdown) // was ErrStartAfterShutdown
	// docs:end
}

// docsExtension is an extension that registers nothing.
type docsExtension struct{}

func (docsExtension) Name() string                         { return "docs" }
func (docsExtension) Extend(*doger.ExtensionContext) error { return nil }

// docsSnippet matches a Go block of the migration guide and the comment
// before it: <!-- snippet: name -->, <!-- before --> (samber/do code, not
// compiled here) or <!-- text --> (a fragment, such as an import, that is
// not code on its own).
var docsSnippet = regexp.MustCompile("(?s)(?:<!-- (snippet: ([a-z-]+)|before|text) -->\\n(?:[ \\t]*\\n)*)?```go\\n(.*?)```")

// docsRegion matches a named region of a compiled snippet file.
var docsRegion = regexp.MustCompile(`(?s)// docs:([a-z-]+)\n(.*?)\n[\t ]*// docs:end`)

// docsSnippetFiles are the compiled snippet files, by whether a copy of the
// root module without its sibling modules may lack them.
var docsSnippetFiles = map[string]bool{
	"docs_snippets_test.go":                 false,
	"clock/clockwork/docs_snippets_test.go": true,
}

// docsRegions returns the regions of the compiled snippet files by name,
// and the names whose file is not in this copy of the module.
func docsRegions(t *testing.T) (regions map[string]string, optional map[string]bool) {
	t.Helper()

	regions, optional = map[string]string{}, map[string]bool{}
	for path, mayLack := range docsSnippetFiles {
		content, err := readLF(path)
		if mayLack && errors.Is(err, fs.ErrNotExist) {
			optional["clock"] = true
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range docsRegion.FindAllStringSubmatch(content, -1) {
			regions[match[1]] = dedent(match[2])
		}
	}

	return regions, optional
}

// TestDocs_MigrationSnippetsCompiled pins that every Go block of the
// migration guide is either marked as samber/do code or a fragment, or is,
// as a whole, the named region of a compiled snippet file.
func TestDocs_MigrationSnippetsCompiled(t *testing.T) {
	t.Parallel()

	doc, err := readLF("docs/docs/migrating/migrating-from-samber-do.md")
	if err != nil {
		t.Fatal(err)
	}

	regions, optional := docsRegions(t)

	blocks := docsSnippet.FindAllStringSubmatch(doc, -1)
	if len(blocks) == 0 {
		t.Fatal("the migration guide has no Go block")
	}
	compiled := 0
	for _, block := range blocks {
		marker, name, text := block[1], block[2], dedent(block[3])
		switch {
		case marker == "before" || marker == "text":
			continue
		case name == "":
			t.Errorf("Go block without a <!-- snippet: name -->, <!-- before --> or <!-- text --> comment:\n%s", text)
			continue
		case optional[name]:
			t.Logf("snippet %q: its compiled file is not in this copy of the module", name)
			continue
		}
		region, ok := regions[name]
		if !ok {
			t.Errorf("snippet %q has no // docs:%s region in a compiled file", name, name)
			continue
		}
		if text != region {
			t.Errorf("snippet %q differs from its compiled region\ndoc:\n%s\ncompiled:\n%s", name, text, region)
		}
		compiled++
	}
	if compiled == 0 {
		t.Error("no compiled snippet in the migration guide")
	}
}

// TestDocs_MigrationSnippetsRun runs the snippets whose outcome the guide
// states.
func TestDocs_MigrationSnippetsRun(t *testing.T) {
	t.Parallel()

	docsBasic()
	docsHealthEnded(t.Context(), doger.New())
	docsLogger()
	docsMaxFrames()
	docsHooks()
	docsHealthTimeout()
	docsProviderPanic(doger.New(), errors.New("broken"))
	docsBoundedShutdown(doger.New())
	docsPrerelease(doger.New(), docsExtension{})
	if !docsPanicNil() {
		t.Error("panic(nil) does not match *runtime.PanicNilError and ErrProviderPanic")
	}
	if err := docsAlias(); err != nil {
		t.Error(err)
	}
	if !docsNilInjector() {
		t.Error("a nil injector does not match ErrNilInjector")
	}
	docsProvideOptions()
	if err := docsShutdownReport(doger.New()); err != nil {
		t.Error(err)
	}
	docsDuplicateScope(doger.New())
	docsAfterShutdown(doger.New())
	docsStarter()
}
