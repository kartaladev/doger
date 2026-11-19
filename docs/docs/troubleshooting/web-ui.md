---
title: Web UI
description: Mount doger's debug Web UI (std, Gin, Fiber, Echo, or Chi) behind your own middleware to inspect scopes, services, and dependency graphs from a browser.
sidebar_position: 4
---

# Web UI

The debug Web UI is an HTTP handler that renders the same scope tree and dependency graph as [`doger.ExplainInjector`](./scope-tree.md), but as a browsable page instead of a text dump. Mount it on any router — the standard library, Gin, Fiber, Echo, or Chi are all supported.

Once mounted, the handler serves a home page, the scope tree, the list of services and a page per service with its dependencies and dependents, under the path prefix you choose (`/debug/doger` in the examples below). It's a thin read-only layer over the same [`ExplainInjector`](./scope-tree.md) / [`ExplainService`](./service-dependencies.md) APIs used for text-based debugging — useful when you'd rather click through a running instance than reproduce the issue with a script.

Viewing a page never builds, invokes, health-checks or shuts down a service. Only `GET` (and `HEAD`) requests are served.

> Caution
>
> Do not expose the debug Web UI publicly in production. It reveals internal
> information about your DI graph (service names, dependencies, source files).
> Bind it to a private listener (for example `127.0.0.1`) and put it behind
> your own authentication middleware, as every example below does.

## What the UI guarantees

- **Escaped output.** Scope and service names are rendered as text and every link encodes its query values, so a name such as `<script>` or `a&b#c` can neither inject markup nor break its own link.
- **Defensive headers.** Every page, redirect and error carries `X-Content-Type-Options: nosniff`, `Content-Security-Policy: default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'` and `Cache-Control: no-store`. The UI cannot be framed. Your middleware can add more headers, such as `Strict-Transport-Security`.
- **Bounded dependency trees.** A service page expands each service of its dependency and dependent trees once. A service met again is shown as a link to its own page, marked as a reference, with no children, so the page grows with the number of edges, not with the number of paths.
- **Generic errors.** A page that fails to render gets a plain `500 Internal Server Error` that reveals nothing of the error.

## Options

Every adapter's `Use` and every page function of `github.com/kartaladev/doger/http` accept `...dogerhttp.Option`:

| Option | Default | Effect |
| --- | --- | --- |
| `dogerhttp.WithFullSourcePaths()` | off | Shows the full recorded path where a service was provided. By default the service page shows `<file>:<line>`, without directories. |
| `dogerhttp.WithErrorHandler(func(ctx context.Context, err error))` | errors are discarded | Receives each render failure once, with the request's context (Fiber passes `c.UserContext()`), before the generic 500. The last option wins; `nil` restores the default. |

```go
import dogerhttp "github.com/kartaladev/doger/http"

handler := dogerhttpstd.Use("/debug/doger", injector,
    dogerhttp.WithFullSourcePaths(),
    dogerhttp.WithErrorHandler(func(ctx context.Context, err error) {
        slog.ErrorContext(ctx, "debug UI page failed", "error", err)
    }),
)
```

## Without framework {#without-framework}

```bash
go get github.com/kartaladev/doger/http/std
```

`Use` returns an `http.Handler`; wrap it in your middleware.

```go
import dogerhttpstd "github.com/kartaladev/doger/http/std"

injector := startProgram()

mux := http.NewServeMux()
mux.Handle("/debug/doger/", basicAuth(dogerhttpstd.Use("/debug/doger", injector)))

http.ListenAndServe("127.0.0.1:8081", mux)
```

## Gin {#gin}

```bash
go get github.com/kartaladev/doger/http/gin
```

Pass the middleware to the group the UI is mounted on.

```go
import dogergin "github.com/kartaladev/doger/http/gin"

injector := startProgram()

router := gin.New()
auth := gin.BasicAuth(gin.Accounts{os.Getenv("DEBUG_USER"): os.Getenv("DEBUG_PASSWORD")})
dogergin.Use(router.Group("/debug/doger", auth), injector)

router.Run("127.0.0.1:8081")
```

## Fiber {#fiber}

```bash
go get github.com/kartaladev/doger/http/fiber
```

```go
import (
    "github.com/gofiber/fiber/v2/middleware/basicauth"
    dogerfiber "github.com/kartaladev/doger/http/fiber"
)

injector := startProgram()

app := fiber.New()
auth := basicauth.New(basicauth.Config{Users: map[string]string{
    os.Getenv("DEBUG_USER"): os.Getenv("DEBUG_PASSWORD"),
}})
dogerfiber.Use(app.Group("/debug/doger", auth), "/debug/doger", injector)

app.Listen("127.0.0.1:8081")
```

## Echo {#echo}

```bash
go get github.com/kartaladev/doger/http/echo
```

```go
import (
    "github.com/labstack/echo/v4/middleware"
    dogerecho "github.com/kartaladev/doger/http/echo"
)

injector := startProgram()

e := echo.New()
auth := middleware.BasicAuth(func(user, password string, _ echo.Context) (bool, error) {
    return checkDebugCredentials(user, password), nil
})
dogerecho.Use(e.Group("/debug/doger", auth), "/debug/doger", injector)

e.Start("127.0.0.1:8081")
```

## Chi {#chi}

```bash
go get github.com/kartaladev/doger/http/chi
```

`dogerchi.Use` accepts any `chi.Router`, so mount it on `r.With(auth)` or inside `r.Group`.

```go
import (
    "github.com/go-chi/chi/v5/middleware"
    dogerchi "github.com/kartaladev/doger/http/chi"
)

injector := startProgram()

r := chi.NewRouter()
auth := middleware.BasicAuth("debug", map[string]string{
    os.Getenv("DEBUG_USER"): os.Getenv("DEBUG_PASSWORD"),
})
dogerchi.Use(r.With(auth), "/debug/doger", injector)

http.ListenAndServe("127.0.0.1:8081", r)
```
