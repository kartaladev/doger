---
title: Web UI
description: Mount doger's debug Web UI (std, Gin, Fiber, Echo, or Chi) to inspect scopes, services, and dependency graphs from a browser.
sidebar_position: 4
---

# Web UI

The debug Web UI is an HTTP handler that renders the same scope tree and dependency graph as [`doger.ExplainInjector`](./scope-tree.md), but as a browsable page instead of a text dump. Mount it on any router — the standard library, Gin, Fiber, Echo, or Chi are all supported.

Once mounted, the handler serves the scope tree, per-service dependency details, and health-check status under the path prefix you choose (`/debug/do` in the examples below). It's a thin read-only layer over the same [`ExplainInjector`](./scope-tree.md) / [`ExplainService`](./service-dependencies.md) APIs used for text-based debugging — useful when you'd rather click through a running instance than reproduce the issue with a script.

> Caution
>
> Do not expose the debug Web UI publicly in production. It reveals internal
> information about your DI graph (service names, dependencies, etc.). Protect
> the routes with authentication (for example, Basic Auth) and/or network
> restrictions (IP allowlist, VPN). Apply your auth middleware to the router
> group before mounting the debug handlers.

## Without framework {#without-framework}

```bash
go get github.com/kartaladev/doger/http/std
```

```go
import dogerhttpstd "github.com/kartaladev/doger/http/std"

injector := startProgram()

mux := http.NewServeMux()
// Protect with your own middleware (e.g., Basic Auth) before mounting
// the debug handler in production.
mux.Handle("/debug/do/", dogerhttpstd.Use("/debug/do", injector))

http.ListenAndServe(":8080", mux)
```

## Gin {#gin}

```bash
go get github.com/kartaladev/doger/http/gin
```

```go
import dogergin "github.com/kartaladev/doger/http/gin"

injector := startProgram()

router := gin.New()
// Attach auth middleware to the group to protect debug UI in production.
dogergin.Use(router.Group("/debug/do"), injector)

router.Run(":8080")
```

## Fiber {#fiber}

```bash
go get github.com/kartaladev/doger/http/fiber
```

```go
import dogerfiber "github.com/kartaladev/doger/http/fiber"

injector := startProgram()

router := fiber.New()
// Attach auth middleware to the group to protect debug UI in production.
dogerfiber.Use(router.Group("/debug/do"), "/debug/do", injector)

router.Listen(":8080")
```

## Echo {#echo}

```bash
go get github.com/kartaladev/doger/http/echo
```

```go
import dogerecho "github.com/kartaladev/doger/http/echo"

injector := startProgram()

router := echo.New()
// Attach auth middleware to the group to protect debug UI in production.
dogerecho.Use(router.Group("/debug/do"), "/debug/do", injector)

router.Start(":8080")
```

## Chi {#chi}

```bash
go get github.com/kartaladev/doger/http/chi
```

```go
import dogerchi "github.com/kartaladev/doger/http/chi"

injector := startProgram()

router := chi.NewRouter()
// Protect with your own middleware (e.g., Basic Auth) before mounting
// the debug handler in production.
dogerchi.Use(router, "/debug/do", injector)

http.ListenAndServe(":8080", router)
```
