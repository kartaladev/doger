// Package doger is a generics-based dependency injection container: services
// are registered with typed providers or values, resolved by type or by name,
// started and health-checked as a container, and shut down in reverse
// dependency order.
//
// # Quickstart
//
// Create a container, register providers and resolve services. A provider
// receives the injector, resolves its own dependencies from it, and runs once,
// on the first invocation:
//
//	type Config struct{ DSN string }
//
//	type DB struct{ dsn string }
//
//	func NewDB(i doger.Injector) (*DB, error) {
//		cfg := doger.MustInvoke[*Config](i)
//		return &DB{dsn: cfg.DSN}, nil
//	}
//
//	func main() {
//		injector := doger.New()
//		doger.ProvideValue(injector, &Config{DSN: "postgres://localhost/app"})
//		doger.Provide(injector, NewDB)
//
//		db, err := doger.Invoke[*DB](injector)
//		if err != nil {
//			log.Fatal(err)
//		}
//		_ = db
//
//		injector.ShutdownOnSignals()
//	}
//
// Services are lazy by default ([Provide], [ProvideNamed]). [ProvideValue]
// registers an eager value, [ProvideTransient] a provider called on every
// invocation, and [Package] groups registrations for reuse. [Invoke] resolves
// by the type's name, [InvokeNamed] by name, [InvokeAs] by the first service
// assignable to a type, and [InvokeAll] collects every service of a type.
// [Alias] and [Bind] publish a service under an interface, and
// [InvokeStruct] fills the tagged fields of a struct.
//
// # Scopes
//
// [RootScope.Scope] creates a child scope. A child resolves its own services
// first, then its ancestors'; a parent never sees a child's services unless
// the child publishes them with [Export]. A scope is shut down with its
// descendants, by its own Shutdown or by its root's; once its shutdown has
// begun, invoking or registering on it fails with [ErrShutdown].
// [RootScope.Clone] copies a container's registrations, without the instances
// already built.
//
// # Lifecycle
//
// [RootScope.Start] builds the services registered with [AsStarter] and calls
// their Start method; [RootScope.Shutdown] (or [RootScope.ShutdownOnSignals])
// shuts every service down in reverse dependency order and returns a
// [ShutdownReport]. A service shuts down through one of the Shutdowner
// interfaces it implements. Dependencies recorded while providers resolve each
// other order start and shutdown; [DependsOn] and [DeclareDependency] add
// edges that are not resolutions. [RootScope.Phase] reports where the
// container is, and InjectorOpts carries the container hooks, a Logger and the
// Clock.
//
// # Health
//
// A service implementing [Healthchecker] or [HealthcheckerWithContext] is
// checked by [RootScope.HealthCheck], by the single-service [HealthCheck] and
// [HealthCheckNamed], and by [RootScope.HealthReport], which classifies each
// service ([WithHealthTags], [AsOptionalHealth]) into a [HealthReport] that
// marshals to JSON. Checks are bounded by default (10s,
// [DefaultHealthReportCheckTimeout]); InjectorOpts.HealthCheckTimeout changes
// the bound, and a negative value removes it. A [HealthScheduler] set
// in InjectorOpts keeps the report fresh in the background once the container
// is ready.
//
// # Extensions
//
// An [Extension] packages the integration of a library: [Extend] installs it
// into a container, in a private scope of its own, and it publishes the
// services the application may use with [ExtensionExport]. An extension reads
// its configuration from [ExtensionContext.Properties] and installs the
// extensions it depends on with [ExtensionContext.Require].
//
// # Errors
//
// Errors carry the "doger: " prefix and match the package's sentinels with
// errors.Is, such as [ErrServiceNotFound], [ErrCircularDependency] and
// [ErrHealthCheckTimeout]; [ServiceError] names the scope and service of a
// build that failed.
package doger
