
# doger - Dependency Injection

[![tag](https://img.shields.io/github/tag/kartaladev/doger.svg)](https://github.com/kartaladev/doger/releases)
![Go Version](https://img.shields.io/badge/Go-%3E%3D%201.27-%23007d9c)
[![GoDoc](https://godoc.org/github.com/kartaladev/doger?status.svg)](https://pkg.go.dev/github.com/kartaladev/doger)
![Build Status](https://github.com/kartaladev/doger/actions/workflows/test.yml/badge.svg)
[![Go report](https://goreportcard.com/badge/github.com/kartaladev/doger)](https://goreportcard.com/report/github.com/kartaladev/doger)
[![Coverage](https://img.shields.io/codecov/c/github/kartaladev/doger)](https://codecov.io/gh/kartaladev/doger)
[![License](https://img.shields.io/github/license/kartaladev/doger)](./LICENSE)

**⚙️ A dependency injection toolkit based on Go generics (Go 1.27+).**

> doger is a fork of [samber/do](https://github.com/samber/do) by Samuel Berthe. Coming from `samber/do`? See [Migrating from samber/do](https://kartaladev.github.io/doger/docs/migrating/migrating-from-samber-do).

This library implements the Dependency Injection design pattern. It may replace the fantastic `uber/dig` package. `doger` uses Go generics (Go 1.27+) and therefore offers a type‑safe API.

![image](https://github.com/user-attachments/assets/81b91fa7-cdb4-4094-94ba-a0179abc6bf7)

----

**Why this name?**

Upstream's short name `do` is the sum of `DI` and `Go`. `doger` keeps it as its prefix.

## 💡 Features

- **📒 Service registration**
  - Register by type
  - Register by name
  - Register multiple services from a package at once
  - Replaceable defaults (`AsDefault()`) and declared dependencies (`DependsOn`)
- **🪃 Service invocation**
  - Eager loading
  - Lazy loading
  - Transient loading
  - Tag-based invocation (struct injection)
  - Collections: every service of a type (`InvokeAll`, `InvokeAllMap`, `do:"*"`)
  - Circular dependency detection
- **🧙‍♂️ Service aliasing**
  - Implicit (provide struct, invoke interface)
  - Explicit (provide struct, bind interface, invoke interface)
- **🔁 Service lifecycle**
  - Start lifecycle: `Start()` builds and starts the services opted in with `AsStarter()` in dependency order, then `Refresh()` and `Scope.Start()` start the ones added later
  - Component kinds (`NewKind`, the built-in `Runnable()`): typed handlers for opted-in services at Start
  - Health reports: cached, tagged (readiness, liveness), optional checks, gated by the container phase
  - Optional scheduled health rounds (see [Scheduled health checks](#-scheduled-health-checks))
  - Graceful unload (shutdown)
  - Dependency-aware parallel shutdown
  - Lifecycle hooks
- **🧩 Extensions**
  - Reusable modules installed with `Extend`, in a private scope with exported services
  - Properties: extensions read their configuration from a `PropertySource` (environment variables by default)
- **📦 Scope (a.k.a module) tree**
  - Visibility control
  - Dependency grouping
- **📤 Container**
  - Dependency graph resolution and visualization
  - Container cloning
  - Service override
- **🧪 Debugging & introspection**
  - Explain APIs: scope tree and service dependencies
  - Web UI & HTTP middleware (std, Gin, Fiber, Echo, Chi)
- **🌈 Lightweight, minimal dependencies**
- **🔅 No code generation**
- **😷 Type‑safe API**

## 🚀 Install

```sh
go get github.com/kartaladev/doger@latest
```

### ⚡ Quickstart

```go
type DB struct{ dsn string }

type Server struct{ db *DB }

func (s *Server) Start(ctx context.Context) error {
	fmt.Println("serving with", s.db.dsn)
	return nil
}

func main() {
	root := doger.New()
	doger.ProvideValue(root, &DB{dsn: "postgres://localhost/app"})
	doger.Provide(root, func(i doger.Injector) (*Server, error) {
		db, err := doger.Invoke[*DB](i)
		return &Server{db: db}, err
	}, doger.AsStarter()) // started by root.Start, after what it invokes

	if err := root.StartWithContext(context.Background()); err != nil {
		log.Fatal(err)
	}
	_ = root.Shutdown() // shuts services down in reverse dependency order
}
```

This library is v0 and follows SemVer: until v1.0.0, a minor release may contain breaking changes.

Besides the Go std lib, this library depends only on [`samber/go-type-to-string`](https://github.com/samber/go-type-to-string) and [`jonboulle/clockwork`](https://github.com/jonboulle/clockwork).

### 🩺 Scheduled health checks

`HealthReport()` serves health to many readers at the cost of one: results are cached (`HealthReportCacheTTL`, 1s by default) and each check in flight is shared. To keep them fresh in the background, set a `HealthScheduler`. The [`scheduler/gocron`](./scheduler/gocron) module provides one built on [go-co-op/gocron](https://github.com/go-co-op/gocron), in its own module so the root module gains no dependency:

```sh
go get github.com/kartaladev/doger/scheduler/gocron@latest
```

```go
scheduler, err := dogergocron.New() // a round at once, then every 30s
if err != nil {
	return err
}
injector := doger.NewWithOpts(&doger.InjectorOpts{HealthScheduler: scheduler})
```

See its [README](./scheduler/gocron/README.md) for the overrides and caveats.

### 🔥 Migration from samber/do

[Documentation here](https://kartaladev.github.io/doger/docs/migrating/migrating-from-samber-do). For upstream's own v1 to v2 upgrade, see [this page](https://kartaladev.github.io/doger/docs/upgrading/from-v1-x-to-v2).

## 🤠 Documentation

- [GoDoc: https://pkg.go.dev/github.com/kartaladev/doger](https://pkg.go.dev/github.com/kartaladev/doger)
- [Documentation](https://kartaladev.github.io/doger/docs/getting-started)
- [Examples](https://github.com/kartaladev/doger/tree/main/examples)

## 🤝 Contributing

- Fork the [project](https://github.com/kartaladev/doger)
- Fix [open issues](https://github.com/kartaladev/doger/issues) or request new features

Don't hesitate ;)

```bash
# Install some dev dependencies
make tools

# Run tests
make test
# or
make watch-test
```

## 👤 Contributors

![Contributors](https://contrib.rocks/image?repo=kartaladev/doger)

## 💫 Show your support

Give a ⭐️ if this project helped you!

## 📝 License

Copyright © 2022 [Samuel Berthe](https://github.com/samber) (upstream [samber/do](https://github.com/samber/do)).

Copyright © 2026 kartaladev.

This project is [MIT](./LICENSE) licensed.
