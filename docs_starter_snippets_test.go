package doger_test

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/kartaladev/doger"
)

// The Go blocks of docs/docs/service-lifecycle/starter.md, compiled. The
// text between a "// docs:<name>" marker and "// docs:end" is the doc's Go
// block preceded by <!-- snippet: <name> -->, character for character after
// dedenting (see TestDocs_StarterSnippetsCompiled).

// starterDB is a Starter with Start(ctx) error.
type starterDB struct{}

func (*starterDB) Start(context.Context) error { return nil }

// starterHTTPServer invokes *starterDB, so it is started after it.
type starterHTTPServer struct{ db *starterDB }

func (*starterHTTPServer) Start(context.Context) error { return nil }

func NewDB(doger.Injector) (*starterDB, error) { return &starterDB{}, nil }

func NewHTTPServer(i doger.Injector) (*starterHTTPServer, error) {
	db, err := doger.Invoke[*starterDB](i)
	return &starterHTTPServer{db: db}, err
}

func docsStarterQuickstart(ctx context.Context) error {
	// docs:starter-quickstart
	root := doger.New()
	doger.Provide(root, NewDB, doger.AsStarter())         // Start(ctx) error, opted in
	doger.Provide(root, NewHTTPServer, doger.AsStarter()) // invokes *DB -> started after DB

	if err := root.StartWithContext(ctx); err != nil {
		_ = root.Shutdown() // the caller owns cleanup
		return err
	}
	// docs:end
	return nil
}

// docs:starter-interfaces
type (
	Starter                    interface{ Start() }
	StarterWithError           interface{ Start() error }
	StarterWithContext         interface{ Start(context.Context) }
	StarterWithContextAndError interface{ Start(context.Context) error }
)

// docs:end

// Lifecycle is an interface without a Start method.
type Lifecycle interface{ Shutdown() error }

type starterLifecycle struct{}

func (*starterLifecycle) Start() error    { return nil }
func (*starterLifecycle) Shutdown() error { return nil }

func NewLifecycle(doger.Injector) (Lifecycle, error) { return &starterLifecycle{}, nil }

func docsStarterInterfaceOptIn(root *doger.RootScope) {
	// docs:starter-interface-opt-in
	// The interface has no Start method: the instance is checked at Start.
	doger.Provide(root, NewLifecycle, doger.AsStarter())
	// docs:end
}

func docsStarterAutoDetection() *doger.RootScope {
	// docs:starter-auto-detection
	root := doger.NewWithOpts(&doger.InjectorOpts{
		EnableStarterAutoDetection: true, // also start every type with a Start method
	})
	// docs:end
	return root
}

type (
	starterA struct{}
	starterB struct{}
	starterZ struct{}
)

func NewA(doger.Injector) (*starterA, error) { return &starterA{}, nil }
func NewB(doger.Injector) (*starterB, error) { return &starterB{}, nil }
func NewZ(doger.Injector) (*starterZ, error) { return &starterZ{}, nil }

func docsStarterTies(root *doger.RootScope) {
	// docs:starter-ties
	request := root.Scope("request")
	doger.ProvideNamed(request, "z", NewZ) // registered first, but in a child scope
	doger.ProvideNamed(root, "b", NewB)
	doger.ProvideNamed(root, "a", NewA)
	_ = root.Start() // starts b, a, z
	// docs:end
}

func docsStarterContext(root *doger.RootScope) error {
	// docs:starter-context
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := root.StartWithContext(ctx); err != nil {
		_ = root.Shutdown()
		return err
	}
	// docs:end
	return nil
}

func docsStarterErrors(root *doger.RootScope) error {
	// docs:starter-errors
	if err := root.Start(); err != nil {
		_ = root.Shutdown() // shuts down every built service, started or not
		return err
	}
	// docs:end
	return nil
}

func docsStarterStartError(err error) {
	// docs:starter-start-error
	if startErr, ok := errors.AsType[*doger.StartError](err); ok {
		log.Printf("%s failed at %s in scope %s: %v", startErr.Service, startErr.Op, startErr.Scope.Name(), startErr.Cause)
	}
	// docs:end
}

type (
	starterReportsWorker struct{}
	starterTenantWorker  struct{}
	starterTenantSync    struct{}
)

func NewReportsWorker(doger.Injector) (*starterReportsWorker, error) {
	return &starterReportsWorker{}, nil
}

func NewTenantWorker(doger.Injector) (*starterTenantWorker, error) {
	return &starterTenantWorker{}, nil
}
func NewTenantSync(doger.Injector) (*starterTenantSync, error) { return &starterTenantSync{}, nil }

func docsStarterRefresh(root *doger.RootScope) error {
	// docs:starter-refresh
	_ = root.Start()

	doger.Provide(root, NewReportsWorker) // registered after Start
	tenant := root.Scope("tenant")        // created after Start
	doger.Provide(tenant, NewTenantWorker)

	err := root.Refresh() // starts both workers
	// docs:end
	return err
}

func docsStarterScopeStart(root *doger.RootScope) error {
	// docs:starter-scope-start
	tenant := root.Scope("tenant-a")
	doger.Provide(tenant, NewTenantSync)

	err := tenant.Start() // starts tenant-a and its child scopes only
	// docs:end
	return err
}

func docsStarterParallel() *doger.RootScope {
	// docs:starter-parallel
	root := doger.NewWithOpts(&doger.InjectorOpts{StartParallelism: 4})
	// docs:end
	return root
}

// BlockingServer has the wrong shape: its Start serves until Shutdown.
type BlockingServer struct{ srv *http.Server }

// docs:starter-blocking-server
// Wrong: Start blocks until Shutdown, so root.Start never returns.
func (s *BlockingServer) Start(ctx context.Context) error {
	return s.srv.ListenAndServe()
}

// docs:end

// ListeningServer has the right shape: its Start returns once listening.
type ListeningServer struct{ srv *http.Server }

// docs:starter-listening-server
// Right: Start returns once the server is listening.
func (s *ListeningServer) Start(ctx context.Context) error {
	addr := s.srv.Addr
	if addr == "" {
		addr = ":http" // as ListenAndServe does
	}
	ln, err := new(net.ListenConfig).Listen(ctx, "tcp", addr)
	if err != nil {
		return err // fails root.Start, naming this service
	}
	go func() { _ = s.srv.Serve(ln) }() // http.ErrServerClosed after Shutdown
	return nil
}

func (s *ListeningServer) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}

// docs:end

// The snippets above are compiled, not run.
var (
	_ = docsStarterQuickstart
	_ = docsStarterInterfaceOptIn
	_ = docsStarterAutoDetection
	_ = docsStarterTies
	_ = docsStarterContext
	_ = docsStarterErrors
	_ = docsStarterStartError
	_ = docsStarterRefresh
	_ = docsStarterScopeStart
	_ = docsStarterParallel
	_ = []any{(*Starter)(nil), (*StarterWithError)(nil), (*StarterWithContext)(nil), (*StarterWithContextAndError)(nil)}
	_ = []any{(*BlockingServer)(nil), (*ListeningServer)(nil)}
	_ = errors.New
	_ = log.Printf
	_ = time.Second
)
