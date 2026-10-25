---
title: From Google Wire
description: Migrate your dependency injection from Google Wire to doger
sidebar_position: 2
---

# Migrating from Google Wire

This guide will help you migrate your dependency injection setup from Google Wire to `doger`.

## Overview {#overview}

Google Wire and `doger` are both dependency injection libraries for Go, but they have different approaches:

- **Google Wire**: Uses code generation with `//go:build wire` directives
- **doger**: Uses runtime dependency injection with a fluent API

## Key Differences {#key-differences}

| Feature               | Google Wire             | doger                     |
| --------------------- | ----------------------- | ------------------------- |
| **Approach**          | Code generation         | Runtime injection         |
| **Build time**        | Requires `wire` command | No build tools needed     |
| **Flexibility**       | Static, compile-time    | Dynamic, runtime          |
| **Error handling**    | Compile-time errors     | Runtime errors            |
| **Service lifecycle** | Limited                 | Full lifecycle management |

## Migration Steps {#migration-steps}

### 1. Remove Wire Dependencies {#1-remove-wire-dependencies}

Remove Wire from your `go.mod`:

```bash
go mod edit -droprequire github.com/google/wire
```

Remove any Wire-related build tags and imports from your code.

### 2. Replace Wire Provider Functions {#2-replace-wire-provider-functions}

**Before (Wire):**

```go
// +build wireinject

package main

import (
    "github.com/google/wire"
    "myapp/services"
)

func InitializeApp() (*App, error) {
    wire.Build(
        services.NewDatabase,
        services.NewUserService,
        services.NewApp,
    )
    return &App{}, nil
}
```

**After (doger):**

```go
package main

import (
    "github.com/kartaladev/doger"
    "myapp/services"
)

func InitializeApp() (*App, error) {
    injector := doger.New()

    // Register services
    doger.Provide(injector, services.NewDatabase)
    doger.Provide(injector, services.NewUserService)
    doger.Provide(injector, services.NewApp)

    // Invoke the app
    app, err := doger.Invoke[*App](injector)
    if err != nil {
        return nil, err
    }

    return app, nil
}
```

### 3. Update Service Constructors {#3-update-service-constructors}

Wire provider functions need to be updated for `doger`. Service constructors receive `doger.Injector` as the first parameter and return an additional error:

**Before (Wire):**

```go
func NewUserService(db *Database) *UserService {
    return &UserService{db: db}
}

func NewDatabase(config *Config) *Database {
    return &Database{config: config}
}
```

**After (doger):**

```go
func NewUserService(i doger.Injector) (*UserService, error) {
    db := doger.MustInvoke[*Database](i)
    return &UserService{db: db}, nil
}

func NewDatabase(i doger.Injector) (*Database, error) {
    config := doger.MustInvoke[*Config](i)
    return &Database{config: config}, nil
}
```

### 4. Handle Interface Bindings {#4-handle-interface-bindings}

**Before (Wire):**

```go
var Set = wire.NewSet(
    wire.Bind(new(Repository), new(*UserRepository)),
    NewUserRepository,
    NewUserService,
)
```

**After (doger):**

```go
injector := doger.New()

doger.Provide(injector, func(i doger.Injector) (Repository, error) {
    return &UserRepository{}, nil
})
```

Or declare an explicit binding:

```go
// Register the concrete implementation
doger.Provide(injector, func(i doger.Injector) (*UserRepository, error) {
    return &UserRepository{}, nil
})

// Register the interface binding
doger.As[*UserRepository, Repository](i)
```

Or bind on service loading:

```go
// Register the concrete implementation
doger.Provide(injector, func(i doger.Injector) (*UserRepository, error) {
    repo := doger.MustInvoke[*UserRepository](i)
    return repo, nil
})

// Find the matching concrete type during invocation
userRepository, err := doger.InvokeAs[Repository](i)
```

### 5. Update Main Function {#5-update-main-function}

**Before (Wire):**

```go
func main() {
    app, err := InitializeApp()
    if err != nil {
        log.Fatal(err)
    }
    app.Run()
}
```

**After (doger):**

```go
func main() {
    app, err := InitializeApp()
    if err != nil {
        log.Fatal(err)
    }

    // Optional: Graceful shutdown
    defer app.Shutdown()

    app.Run()
}
```

## Advanced Migration Patterns {#advanced-migration-patterns}

### Provider Sets {#provider-sets}

**Before (Wire):**

```go
var UserSet = wire.NewSet(
    NewUserRepository,
    NewUserService,
)

var AppSet = wire.NewSet(
    UserSet,
    NewDatabase,
    NewApp,
)
```

**After (doger):**

```go
var UserPackage = doger.Package(
    doger.Lazy(NewUserRepository),
    doger.Lazy(NewUserService),
)

var AppPackage = doger.Package(
    doger.Lazy(UserPackage),
    doger.Lazy(NewDatabase),
    doger.Lazy(NewApp),
)
```

### Conditional Providers {#conditional-providers}

**Before (Wire):**

```go
func InitializeApp(env string) (*App, error) {
    if env == "test" {
        wire.Build(TestSet)
    } else {
        wire.Build(ProdSet)
    }
    return &App{}, nil
}
```

**After (doger):**

```go
func InitializeApp(env string) (*App, error) {
    injector := doger.New()

    if env == "test" {
        RegisterTestServices(injector)
    } else {
        RegisterProdServices(injector)
    }

    app := doger.MustInvoke[*App](injector)
    return app, nil
}
```

## Benefits of Migration {#benefits-of-migration}

1. **No build tools required** - No need to run `wire` command
2. **Runtime flexibility** - Can change dependencies at runtime
3. **Better error handling** - More detailed error messages
4. **Service lifecycle management** - Built-in shutdown and health checks
5. **Scoped injection** - Support for request-scoped services
6. **Lazy loading** - Services are created only when needed

## Common Pitfalls {#common-pitfalls}

1. **Missing dependencies** - Ensure all required services are registered
2. **Circular dependencies** - `doger` will detect and report these
3. **Interface bindings** - Use `doger.As` for interface implementations
4. **Panics vs errors** - Prefer `doger.MustInvoke` over `doger.Invoke` when a missing service should stop the program immediately instead of being handled as a recoverable error

## Testing {#testing}

Your existing tests should work with minimal changes:

```go
func TestUserService(t *testing.T) {
    injector := testInjector.Clone()

    // NewMockUserService is a provider returning *UserService wired with test doubles
    doger.Override(injector, NewMockUserService)

    service := doger.MustInvoke[*UserService](injector)

    // Your test logic here
}
```

## Next Steps {#next-steps}

After migration, consider exploring these `doger` features:

- [Interface binding](/docs/service-invocation/accept-interfaces-return-structs.md)
- [Scoped Services](/docs/container/scope.md)
- [Health Checks](/docs/service-lifecycle/healthchecker.md)
- [Graceful Shutdown](/docs/service-lifecycle/shutdowner.md)
- [Dependency resolution troubleshooting](/docs/troubleshooting/scope-tree.md)
