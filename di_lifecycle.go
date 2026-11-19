package doger

import (
	"context"
	"time"
)

// Healthchecker is an interface that services can implement to provide health checking capabilities.
// Services implementing this interface can be health-checked by the DI container to ensure they
// are functioning correctly.
//
// The HealthCheck method should perform a quick check to verify the service is healthy.
// It should return nil if the service is healthy, or an error describing the health issue.
//
// Example:
//
//	type Database struct {
//	    conn *sql.DB
//	}
//
//	func (db *Database) HealthCheck() error {
//	    return db.conn.Ping()
//	}
//
// A check may run while its service shuts down: root Shutdown cancels the
// context of a check in flight, with the cause [ErrShutdown], but
// does not wait for it, so a service whose Shutdown releases what its check
// uses must guard against a check still running. A caller still waiting
// for that check gets an error matching [ErrHealthCheckTimeout] and
// [context.Canceled].
type Healthchecker interface {
	HealthCheck() error
}

// HealthcheckerWithContext is an interface that services can implement to provide health checking
// capabilities with context support. This allows for timeout and cancellation control during
// health checks.
//
// The HealthCheck method should perform a quick check to verify the service is healthy.
// It should respect the provided context for cancellation and timeout.
// It should return nil if the service is healthy, or an error describing the health issue.
//
// Example:
//
//	type Database struct {
//	    conn *sql.DB
//	}
//
//	func (db *Database) HealthCheck(ctx context.Context) error {
//	    return db.conn.PingContext(ctx)
//	}
type HealthcheckerWithContext interface {
	HealthCheck(context.Context) error
}

// Shutdowner is an interface that services can implement to provide graceful shutdown capabilities.
// Services implementing this interface will be called during container shutdown to perform
// cleanup operations.
//
// The Shutdown method should perform any necessary cleanup, such as closing connections,
// flushing buffers, or stopping background processes.
//
// Example:
//
//	type Logger struct {
//	    file *os.File
//	}
//
//	func (l *Logger) Shutdown() {
//	    l.file.Close()
//	}
type Shutdowner interface {
	Shutdown()
}

// ShutdownerWithError is an interface that services can implement to provide graceful shutdown
// capabilities with error reporting. This allows services to report any errors that occur
// during shutdown.
//
// The Shutdown method should perform any necessary cleanup and return an error if the
// shutdown process fails.
//
// Example:
//
//	type Database struct {
//	    conn *sql.DB
//	}
//
//	func (db *Database) Shutdown() error {
//	    return db.conn.Close()
//	}
type ShutdownerWithError interface {
	Shutdown() error
}

// ShutdownerWithContext is an interface that services can implement to provide graceful shutdown
// capabilities with context support. This allows for timeout and cancellation control during
// shutdown operations.
//
// The Shutdown method should perform any necessary cleanup and respect the provided context
// for cancellation and timeout.
//
// Example:
//
//	type Server struct {
//	    srv *http.Server
//	}
//
//	func (s *Server) Shutdown(ctx context.Context) {
//	    s.srv.Shutdown(ctx)
//	}
type ShutdownerWithContext interface {
	Shutdown(context.Context)
}

// ShutdownerWithContextAndError is an interface that services can implement to provide graceful
// shutdown capabilities with both context support and error reporting. This is the most flexible
// shutdown interface, allowing for timeout control and error reporting.
//
// The Shutdown method should perform any necessary cleanup, respect the provided context
// for cancellation and timeout, and return an error if the shutdown process fails.
//
// Example:
//
//	type Server struct {
//	    srv *http.Server
//	}
//
//	func (s *Server) Shutdown(ctx context.Context) error {
//	    return s.srv.Shutdown(ctx)
//	}
type ShutdownerWithContextAndError interface {
	Shutdown(context.Context) error
}

// Starter is a service started by [RootScope.Start] with Start().
//
// A service is started only when it opts in: register it with [AsStarter].
// The instance must then implement one of the four Starter variants
// (Starter, StarterWithError, StarterWithContext, StarterWithContextAndError);
// for a service registered under an interface type, the instance is checked
// when Start runs. Transient services and aliases are never started.
//
// With [InjectorOpts.EnableStarterAutoDetection], every non-transient,
// non-alias service whose registered type implements a Starter variant is
// started too. Detection then uses the registered type, unlike the
// Shutdowner variants, which are detected on the built instance, so a lazy
// service is discovered without being built. It is off by default because
// it can surprise: standard-library types such as *exec.Cmd (Start() error)
// would be started, and code that already calls Start() by hand would start
// its services twice.
//
// There is no Stop counterpart: a started service is stopped by its
// Shutdowner method during Shutdown. A Starter without a Shutdowner method is
// allowed; its lifecycle ends with the process.
type Starter interface {
	Start()
}

// StarterWithError is a service started by [RootScope.Start] with Start() error.
type StarterWithError interface {
	Start() error
}

// StarterWithContext is a service started by [RootScope.Start] with Start(ctx).
type StarterWithContext interface {
	Start(context.Context)
}

// StarterWithContextAndError is a service started by [RootScope.Start] with Start(ctx) error.
type StarterWithContextAndError interface {
	Start(context.Context) error
}

// Ordered positions a member in the slices returned by InvokeAll: lower
// values come first, and a member that does not implement Ordered counts as
// 0. The sort is stable, so members with equal values keep the default
// order (ancestors first, then registration order). Ordered affects only
// collections: never construction, start or shutdown order. A WithOrder
// registration option takes precedence over it.
type Ordered interface {
	Order() int
}

// HealthCheck returns a service status, using type inference to determine the service name.
// This function performs a health check on a service by inferring its name from the type T.
// The service must implement either Healthchecker or HealthcheckerWithContext interface.
//
// Parameters:
//   - i: The injector containing the service
//
// Returns an error if the health check fails, or nil if the service is healthy.
//
// Example:
//
//	err := doger.HealthCheck[*Database](injector)
//	if err != nil {
//	    log.Printf("Database health check failed: %v", err)
//	}
func HealthCheck[T any](i Injector) error {
	name := inferServiceName[T]()
	return HealthCheckNamedWithContext(context.Background(), i, name)
}

// HealthCheckWithContext returns a service status, using type inference to determine the service name.
// This function performs a health check on a service with context support for timeout and cancellation.
// The service must implement either Healthchecker or HealthcheckerWithContext interface.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control
//   - i: The injector containing the service
//
// Returns an error if the health check fails, or nil if the service is healthy.
//
// Example:
//
//	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
//	defer cancel()
//
//	err := doger.HealthCheckWithContext[*Database](ctx, injector)
//	if err != nil {
//	    log.Printf("Database health check failed: %v", err)
//	}
func HealthCheckWithContext[T any](ctx context.Context, i Injector) error {
	name := inferServiceName[T]()
	return HealthCheckNamedWithContext(ctx, i, name)
}

// HealthCheckNamed returns a service status for a named service.
// This function performs a health check on a service with the specified name.
// The service must implement either Healthchecker or HealthcheckerWithContext interface.
//
// Parameters:
//   - i: The injector containing the service
//   - name: The name of the service to health check
//
// Returns an error if the health check fails, or nil if the service is healthy.
//
// Example:
//
//	err := doger.HealthCheckNamed(injector, "main-database")
//	if err != nil {
//	    log.Printf("Main database health check failed: %v", err)
//	}
func HealthCheckNamed(i Injector, name string) error {
	return HealthCheckNamedWithContext(context.Background(), i, name)
}

// HealthCheckNamedWithContext returns a service status for a named service with context support.
// This function performs a health check on a service with the specified name and context support
// for timeout and cancellation. The service must implement either Healthchecker or
// HealthcheckerWithContext interface.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control
//   - i: The injector containing the service
//   - name: The name of the service to health check
//
// Returns an error if the health check fails, or nil if the service is healthy.
//
// The check is bounded, on the injector's clock, by
// InjectorOpts.HealthCheckSingleTimeout when it is set (a negative value
// means no bound), and otherwise by HealthCheckTimeout as each check of
// HealthCheck() is: 0 means DefaultHealthReportCheckTimeout (10s) and a
// negative value no bound. HealthCheckGlobalTimeout does not apply. HealthCheck, HealthCheckWithContext
// and HealthCheckNamed go through this function and follow the same rule.
//
// A check of the service already in flight, started by any path, is shared:
// the call waits for it within its own context and bound instead of
// starting another, and the check runs with the context (values and
// deadline) of the caller that started it. A panicking check yields an error
// matching [ErrHealthCheckPanic]. The call takes no slot of
// InjectorOpts.HealthCheckParallelism: it runs the check at once, even one
// waiting for a slot.
//
// Example:
//
//	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
//	defer cancel()
//
//	err := doger.HealthCheckNamedWithContext(ctx, injector, "main-database")
//	if err != nil {
//	    log.Printf("Main database health check failed: %v", err)
//	}
func HealthCheckNamedWithContext(ctx context.Context, i Injector, name string) error {
	injector, err := getInjector(i)
	if err != nil {
		return err
	}

	opts := injector.RootScope().opts
	ctx, cancel := healthCheckContextWithin(ctx, opts.Clock, serviceBound(opts, boundSingle))
	defer cancel()

	return injector.serviceHealthCheck(ctx, name)
}

// healthCheckContextWithin bounds ctx by timeout on clock when timeout is
// greater than zero, and leaves it unbounded otherwise.
func healthCheckContextWithin(ctx context.Context, clock Clock, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return ctx, func() {}
	}

	ctx = withHealthCheckDeadline(ctx, clock.Now().Add(timeout))
	return withClockTimeout(ctx, clock, timeout)
}

// Shutdown stops a service, using type inference to determine the service name.
// This function performs a graceful shutdown on a service by inferring its name from the type T.
// The service must implement one of the Shutdowner interfaces.
//
// Parameters:
//   - i: The injector containing the service
//
// Returns an error if the shutdown fails, or nil if the shutdown was successful.
//
// Example:
//
//	err := doger.Shutdown[*Database](injector)
//	if err != nil {
//	    log.Printf("Database shutdown failed: %v", err)
//	}
func Shutdown[T any](i Injector) error {
	name := inferServiceName[T]()
	return ShutdownNamedWithContext(context.Background(), i, name)
}

// ShutdownWithContext stops a service, using type inference to determine the service name.
// This function performs a graceful shutdown on a service with context support for timeout
// and cancellation. The service must implement one of the Shutdowner interfaces.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control
//   - i: The injector containing the service
//
// Returns an error if the shutdown fails, or nil if the shutdown was successful.
//
// Example:
//
//	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
//	defer cancel()
//
//	err := doger.ShutdownWithContext[*Database](ctx, injector)
//	if err != nil {
//	    log.Printf("Database shutdown failed: %v", err)
//	}
func ShutdownWithContext[T any](ctx context.Context, i Injector) error {
	name := inferServiceName[T]()
	return ShutdownNamedWithContext(ctx, i, name)
}

// ShutdownNamed stops a named service.
// This function performs a graceful shutdown on a service with the specified name.
// The service must implement one of the Shutdowner interfaces.
//
// Parameters:
//   - i: The injector containing the service
//   - name: The name of the service to shutdown
//
// Returns an error if the shutdown fails, or nil if the shutdown was successful.
//
// Example:
//
//	err := doger.ShutdownNamed(injector, "main-database")
//	if err != nil {
//	    log.Printf("Main database shutdown failed: %v", err)
//	}
func ShutdownNamed(i Injector, name string) error {
	return ShutdownNamedWithContext(context.Background(), i, name)
}

// ShutdownNamedWithContext stops a named service with context support.
// This function performs a graceful shutdown on a service with the specified name and context
// support for timeout and cancellation. The service must implement one of the Shutdowner interfaces.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control
//   - i: The injector containing the service
//   - name: The name of the service to shutdown
//
// Returns an error if the shutdown fails, or nil if the shutdown was successful.
//
// Example:
//
//	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
//	defer cancel()
//
//	err := doger.ShutdownNamedWithContext(ctx, injector, "main-database")
//	if err != nil {
//	    log.Printf("Main database shutdown failed: %v", err)
//	}
func ShutdownNamedWithContext(ctx context.Context, i Injector, name string) error {
	injector, err := getInjector(i)
	if err != nil {
		return err
	}

	return injector.shutdownService(ctx, name)
}

// MustShutdown stops a service, using type inference to determine the service name. It panics on error.
// This function performs a graceful shutdown on a service by inferring its name from the type T.
// If the shutdown fails, this function will panic.
//
// Parameters:
//   - i: The injector containing the service
//
// Panics if the shutdown fails.
//
// Example:
//
//	doger.MustShutdown[*Database](injector)
func MustShutdown[T any](i Injector) {
	must0(Shutdown[T](i))
}

// MustShutdownWithContext stops a service, using type inference to determine the service name. It panics on error.
// This function performs a graceful shutdown on a service with context support for timeout
// and cancellation. If the shutdown fails, this function will panic.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control
//   - i: The injector containing the service
//
// Panics if the shutdown fails.
//
// Example:
//
//	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
//	defer cancel()
//
//	doger.MustShutdownWithContext[*Database](ctx, injector)
func MustShutdownWithContext[T any](ctx context.Context, i Injector) {
	must0(ShutdownWithContext[T](ctx, i))
}

// MustShutdownNamed stops a named service. It panics on error.
// This function performs a graceful shutdown on a service with the specified name.
// If the shutdown fails, this function will panic.
//
// Parameters:
//   - i: The injector containing the service
//   - name: The name of the service to shutdown
//
// Panics if the shutdown fails.
//
// Example:
//
//	doger.MustShutdownNamed(injector, "main-database")
func MustShutdownNamed(i Injector, name string) {
	must0(ShutdownNamed(i, name))
}

// MustShutdownNamedWithContext stops a named service. It panics on error.
// This function performs a graceful shutdown on a service with the specified name and context
// support for timeout and cancellation. If the shutdown fails, this function will panic.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control
//   - i: The injector containing the service
//   - name: The name of the service to shutdown
//
// Panics if the shutdown fails.
//
// Example:
//
//	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
//	defer cancel()
//
//	doger.MustShutdownNamedWithContext(ctx, injector, "main-database")
func MustShutdownNamedWithContext(ctx context.Context, i Injector, name string) {
	must0(ShutdownNamedWithContext(ctx, i, name))
}
