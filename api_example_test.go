package doger

import (
	"context"
	"errors"
	"fmt"

	"github.com/jonboulle/clockwork"
)

// apiExampleDB is the service of the package examples.
type apiExampleDB struct{ dsn string }

// apiExampleStore is an interface apiExampleDB implements.
type apiExampleStore interface{ DSN() string }

func (db *apiExampleDB) DSN() string { return db.dsn }

func ExampleLazy() {
	// Lazy returns a registration for a package: the provider runs on the
	// first invocation, once.
	builds := 0
	injector := New(Lazy(func(Injector) (*apiExampleDB, error) {
		builds++
		return &apiExampleDB{dsn: "postgres://db"}, nil
	}))
	fmt.Println("builds before invoke:", builds)

	db := MustInvoke[*apiExampleDB](injector)
	_ = MustInvoke[*apiExampleDB](injector)
	fmt.Println(db.dsn, "builds:", builds)

	// Output:
	// builds before invoke: 0
	// postgres://db builds: 1
}

func ExampleEager() {
	// Eager registers a value that exists already.
	injector := New(Eager(&apiExampleDB{dsn: "postgres://db"}))

	fmt.Println(MustInvoke[*apiExampleDB](injector).dsn)

	// Output:
	// postgres://db
}

func ExampleTransient() {
	// Transient runs the provider on every invocation.
	n := 0
	injector := New(Transient(func(Injector) (*apiExampleDB, error) {
		n++
		return &apiExampleDB{dsn: fmt.Sprintf("conn-%d", n)}, nil
	}))

	fmt.Println(MustInvoke[*apiExampleDB](injector).dsn)
	fmt.Println(MustInvoke[*apiExampleDB](injector).dsn)

	// Output:
	// conn-1
	// conn-2
}

// apiExamplePackage is how a package exposes its services: a value the
// application passes to New.
var apiExamplePackage = Package(
	Lazy(func(Injector) (*apiExampleDB, error) { return &apiExampleDB{dsn: "postgres://db"}, nil }),
	Bind[*apiExampleDB, apiExampleStore](),
)

func ExamplePackage() {
	injector := New(apiExamplePackage)

	for _, service := range injector.ListProvidedServices() {
		fmt.Println(service.Service)
	}

	// Unordered output:
	// *github.com/kartaladev/doger.apiExampleDB
	// github.com/kartaladev/doger.apiExampleStore
}

func ExampleBind() {
	// Bind aliases a concrete type to an interface it implements, inside a
	// Package; the alias resolves to the same instance.
	injector := New(Package(
		Lazy(func(Injector) (*apiExampleDB, error) { return &apiExampleDB{dsn: "postgres://db"}, nil }),
		Bind[*apiExampleDB, apiExampleStore](),
	))

	store := MustInvoke[apiExampleStore](injector)
	fmt.Println(store.DSN(), store == MustInvoke[*apiExampleDB](injector))

	// Output:
	// postgres://db true
}

func ExampleExportNamed() {
	root := New()
	tenant := root.Scope("tenant")
	ProvideNamedValue(tenant, "tenant-db", &apiExampleDB{dsn: "postgres://tenant"})

	// Publish the child's named service in the root, under the same name
	// unless WithExportName gives another one.
	if err := ExportNamed[*apiExampleDB](tenant, "tenant-db"); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(MustInvokeNamed[*apiExampleDB](root, "tenant-db").dsn)

	// a name the child does not register cannot be exported
	err := ExportNamed[*apiExampleDB](tenant, "missing")
	fmt.Println(errors.Is(err, ErrServiceNotFound))

	// Output:
	// postgres://tenant
	// true
}

func ExampleMustExport() {
	root := New()
	tenant := root.Scope("tenant")
	ProvideValue(tenant, &apiExampleDB{dsn: "postgres://tenant"})

	// MustExport publishes the service of type T, and panics on error.
	MustExport[*apiExampleDB](tenant)
	fmt.Println(MustInvoke[*apiExampleDB](root).dsn)

	// Output:
	// postgres://tenant
}

func ExampleExtendScopeName() {
	root := New()

	// Two installs of the same extension need distinct private scopes; the
	// default name is "ext:<name>".
	err := Extend(root, &exampleKVExtension{Addr: "cache:6379", InstanceKey: "cache"}, ExtendScopeName("kv-cache"))
	fmt.Println("installed:", err == nil)
	for _, child := range root.Children() {
		fmt.Println(child.Name())
	}

	// Output:
	// installed: true
	// kv-cache
}

// apiExampleCheck is a health check that fails with err, when set.
type apiExampleCheck struct{ err error }

func (c apiExampleCheck) HealthCheck() error { return c.err }

func ExampleWithHealthTags() {
	injector := New()
	ProvideNamedValue(injector, "db", apiExampleCheck{})
	// A liveness check is not a readiness check: it is left out of
	// readiness reports.
	ProvideNamedValue(injector, "process", apiExampleCheck{}, WithHealthTags(HealthTagLiveness))

	for _, entry := range injector.HealthReport(MatchHealthTags(HealthTagReadiness)).Services {
		fmt.Println("readiness:", entry.Service.Service)
	}
	for _, entry := range injector.HealthReport(MatchHealthTags(HealthTagLiveness)).Services {
		fmt.Println("liveness:", entry.Service.Service)
	}

	// Output:
	// readiness: db
	// liveness: process
}

func ExampleAsOptionalHealth() {
	injector := New()
	ProvideNamedValue(injector, "db", apiExampleCheck{})
	// A failing optional check degrades the report instead of making it
	// unhealthy.
	ProvideNamedValue(injector, "cache", apiExampleCheck{err: errors.New("cache down")}, AsOptionalHealth())

	report := injector.HealthReport()
	fmt.Println(report.State)
	for _, entry := range report.Services {
		fmt.Println(entry.Service.Service, entry.State, entry.Optional)
	}

	// Output:
	// degraded
	// cache degraded true
	// db healthy false
}

func ExampleIgnoreContainerPhase() {
	injector := New()
	ProvideNamedValue(injector, "db", apiExampleCheck{})
	_ = injector.Start()
	fmt.Println("ready:", injector.HealthReport().State) // records a result

	// While the container shuts down, readiness reports are gated to
	// unhealthy so traffic drains; IgnoreContainerPhase reads the checks
	// themselves.
	injector.AddContainerClosingHook(func(context.Context, *RootScope) {
		fmt.Println("gated:", injector.HealthReport().State)
		fmt.Println("ignoring the phase:", injector.HealthReport(IgnoreContainerPhase()).State)
	})
	_ = injector.Shutdown()

	// Output:
	// ready: healthy
	// gated: unhealthy
	// ignoring the phase: healthy
}

// apiExampleSettingsExtension reads its settings from the container's
// property source in its providers.
type apiExampleSettingsExtension struct{}

func (apiExampleSettingsExtension) Name() string { return "settings" }

func (apiExampleSettingsExtension) Extend(x *ExtensionContext) error {
	ProvideNamed(x.Scope(), "settings", func(i Injector) (string, error) {
		properties, ok := ExtensionProperties(i)
		if !ok {
			return "", errors.New("not inside an extension")
		}
		mode, _ := properties.Lookup("mode")
		return "mode=" + mode, nil
	})

	return ExtensionExportNamed[string](x, "settings")
}

// apiExampleMapSource is a PropertySource backed by a map.
type apiExampleMapSource map[string]string

func (s apiExampleMapSource) Lookup(key string) (string, bool) {
	value, ok := s[key]
	return value, ok
}

func ExampleExtensionProperties() {
	root := NewWithOpts(&InjectorOpts{PropertySource: apiExampleMapSource{"settings.mode": "fast"}})
	if err := Extend(root, apiExampleSettingsExtension{}); err != nil {
		fmt.Println(err)
		return
	}

	// The extension's view reads "settings.<key>".
	fmt.Println(MustInvokeNamed[string](root, "settings"))

	// Outside an extension scope the view misses every key.
	_, ok := ExtensionProperties(root)
	fmt.Println("in the root:", ok)

	// Output:
	// mode=fast
	// in the root: false
}

func ExamplePropertySource() {
	// By default extensions read environment variables prefixed DOGER_;
	// InjectorOpts.PropertySource replaces the source for the container.
	source := apiExampleMapSource{"example-kv.addr": "kv.internal:6379"}
	root := NewWithOpts(&InjectorOpts{PropertySource: source})

	// exampleKVExtension reads "addr" when no address is given in code.
	_ = Extend(root, &exampleKVExtension{})
	fmt.Println(MustInvoke[*exampleKV](root).addr)

	// Output:
	// kv.internal:6379
}

// apiExampleScheduler is a HealthScheduler that runs one round as soon as
// the container is ready, then never again.
type apiExampleScheduler struct{}

func (apiExampleScheduler) ScheduleHealthRounds(_ clockwork.Clock, round func(context.Context)) (func(context.Context) error, error) {
	round(context.Background())
	return func(context.Context) error { return nil }, nil
}

func ExampleHealthScheduler() {
	// A HealthScheduler runs the rounds that keep reports fresh; the
	// gocron adapter (dogergocron.New()) runs one every 30s.
	injector := NewWithOpts(&InjectorOpts{HealthScheduler: apiExampleScheduler{}})
	ProvideNamedValue(injector, "db", apiExampleCheck{})

	_ = injector.Start()
	report := injector.HealthReport()
	fmt.Println(report.Monitored, report.State)
	_ = injector.Shutdown()

	// Output:
	// true healthy
}

// apiExampleMigration is a one-shot Runner: Run(ctx) error.
type apiExampleMigration struct{}

func (apiExampleMigration) Run(context.Context) error {
	fmt.Println("migrated")
	return nil
}

func ExampleInjectorOpts_enableRunnerAutoDetection() {
	// With EnableRunnerAutoDetection, every registered type with
	// Run(ctx) error runs once at Start, without AsRunnable.
	injector := NewWithOpts(&InjectorOpts{EnableRunnerAutoDetection: true})
	ProvideValue(injector, apiExampleMigration{})

	_ = injector.Start()

	// Output:
	// migrated
}

// apiExampleApp is injected with a custom struct tag key.
type apiExampleApp struct {
	DB *apiExampleDB `inject:""`
}

func ExampleInjectorOpts_structTagKey() {
	// StructTagKey replaces the default "do" tag of InvokeStruct.
	injector := NewWithOpts(&InjectorOpts{StructTagKey: "inject"})
	ProvideValue(injector, &apiExampleDB{dsn: "postgres://db"})

	app := MustInvokeStruct[apiExampleApp](injector)
	fmt.Println(app.DB.dsn)

	// Output:
	// postgres://db
}
