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

## What the UI guarantees {#what-the-ui-guarantees}

- **Escaped output.** Scope and service names are rendered as text and every link encodes its query values, so a name such as `<script>` or `a&b#c` can neither inject markup nor break its own link.
- **Defensive headers.** Every page, redirect and error carries `X-Content-Type-Options: nosniff`, `Content-Security-Policy: default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'` and `Cache-Control: no-store`. The UI cannot be framed. Your middleware can add more headers, such as `Strict-Transport-Security`.
- **Bounded dependency trees.** A service page expands each service of its dependency and dependent trees once. A service met again is shown as a link to its own page, marked as a reference, with no children, so the page grows with the number of edges, not with the number of paths.
- **Generic errors.** A page that fails to render gets a plain `500 Internal Server Error` that reveals nothing of the error.

## Options {#options}

Every adapter's `Use` and every page function of `github.com/kartaladev/doger/http` accept `...dogerhttp.Option`:

| Option                                                             | Default              | Effect                                                                                                                                                                    |
| ------------------------------------------------------------------ | -------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `dogerhttp.WithFullSourcePaths()`                                  | off                  | Shows the full recorded path where a service was provided. By default the service page shows `<file>:<line>`, without directories.                                        |
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

## Protecting the UI {#protecting-the-ui}

Each adapter section below defines a `debugAuth` function that builds basic-auth middleware from `DEBUG_USER` and `DEBUG_PASSWORD`. It refuses empty credentials, so an unset variable stops the program instead of opening the UI to anyone, and it compares in constant time. The same functions are compiled and run as each adapter's `ExampleUse`, so the snippets stay correct.

## Without framework {#without-framework}

```bash
go get github.com/kartaladev/doger/http/std
```

`Use` returns an `http.Handler`; wrap it in your middleware.

```go
import dogerhttpstd "github.com/kartaladev/doger/http/std"

// debugAuth returns a basic-auth middleware for the debug UI. It refuses
// empty credentials, so an unset variable never opens the UI, and compares
// in constant time.
func debugAuth(user, password string) (func(http.Handler) http.Handler, error) {
    if user == "" || password == "" {
        return nil, errors.New("debug UI: set DEBUG_USER and DEBUG_PASSWORD")
    }

    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            u, p, ok := r.BasicAuth()
            if !ok ||
                subtle.ConstantTimeCompare([]byte(u), []byte(user)) != 1 ||
                subtle.ConstantTimeCompare([]byte(p), []byte(password)) != 1 {
                w.Header().Set("WWW-Authenticate", `Basic realm="debug"`)
                http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)

                return
            }

            next.ServeHTTP(w, r)
        })
    }, nil
}

func main() {
    injector := startProgram()

    auth, err := debugAuth(os.Getenv("DEBUG_USER"), os.Getenv("DEBUG_PASSWORD"))
    if err != nil {
        log.Fatal(err)
    }

    mux := http.NewServeMux()
    mux.Handle("/debug/doger/", auth(dogerhttpstd.Use("/debug/doger", injector)))

    log.Fatal(http.ListenAndServe("127.0.0.1:8081", mux))
}
```

## Gin {#gin}

```bash
go get github.com/kartaladev/doger/http/gin
```

Pass the middleware to the group the UI is mounted on. `gin.BasicAuth` compares in constant time.

```go
import dogergin "github.com/kartaladev/doger/http/gin"

// debugAuth returns a basic-auth middleware for the debug UI. It refuses
// empty credentials, so an unset variable never opens the UI.
func debugAuth(user, password string) (gin.HandlerFunc, error) {
    if user == "" || password == "" {
        return nil, errors.New("debug UI: set DEBUG_USER and DEBUG_PASSWORD")
    }

    return gin.BasicAuth(gin.Accounts{user: password}), nil
}

func main() {
    injector := startProgram()

    auth, err := debugAuth(os.Getenv("DEBUG_USER"), os.Getenv("DEBUG_PASSWORD"))
    if err != nil {
        log.Fatal(err)
    }

    router := gin.New()
    dogergin.Use(router.Group("/debug/doger", auth), injector)

    log.Fatal(router.Run("127.0.0.1:8081"))
}
```

## Fiber {#fiber}

```bash
go get github.com/kartaladev/doger/http/fiber
```

```go
import (
    "github.com/gofiber/fiber/v2"
    "github.com/gofiber/fiber/v2/middleware/basicauth"
    dogerfiber "github.com/kartaladev/doger/http/fiber"
)

// debugAuth returns a basic-auth middleware for the debug UI. It refuses
// empty credentials, so an unset variable never opens the UI, and compares
// in constant time.
func debugAuth(user, password string) (fiber.Handler, error) {
    if user == "" || password == "" {
        return nil, errors.New("debug UI: set DEBUG_USER and DEBUG_PASSWORD")
    }

    return basicauth.New(basicauth.Config{
        Authorizer: func(u, p string) bool {
            return subtle.ConstantTimeCompare([]byte(u), []byte(user)) == 1 &&
                subtle.ConstantTimeCompare([]byte(p), []byte(password)) == 1
        },
    }), nil
}

func main() {
    injector := startProgram()

    auth, err := debugAuth(os.Getenv("DEBUG_USER"), os.Getenv("DEBUG_PASSWORD"))
    if err != nil {
        log.Fatal(err)
    }

    app := fiber.New()
    dogerfiber.Use(app.Group("/debug/doger", auth), "/debug/doger", injector)

    log.Fatal(app.Listen("127.0.0.1:8081"))
}
```

## Echo {#echo}

```bash
go get github.com/kartaladev/doger/http/echo
```

```go
import (
    "github.com/labstack/echo/v4"
    "github.com/labstack/echo/v4/middleware"
    dogerecho "github.com/kartaladev/doger/http/echo"
)

// debugAuth returns a basic-auth middleware for the debug UI. It refuses
// empty credentials, so an unset variable never opens the UI, and compares
// in constant time.
func debugAuth(user, password string) (echo.MiddlewareFunc, error) {
    if user == "" || password == "" {
        return nil, errors.New("debug UI: set DEBUG_USER and DEBUG_PASSWORD")
    }

    return middleware.BasicAuth(func(u, p string, _ echo.Context) (bool, error) {
        return subtle.ConstantTimeCompare([]byte(u), []byte(user)) == 1 &&
            subtle.ConstantTimeCompare([]byte(p), []byte(password)) == 1, nil
    }), nil
}

func main() {
    injector := startProgram()

    auth, err := debugAuth(os.Getenv("DEBUG_USER"), os.Getenv("DEBUG_PASSWORD"))
    if err != nil {
        log.Fatal(err)
    }

    e := echo.New()
    dogerecho.Use(e.Group("/debug/doger", auth), "/debug/doger", injector)

    log.Fatal(e.Start("127.0.0.1:8081"))
}
```

## Chi {#chi}

```bash
go get github.com/kartaladev/doger/http/chi
```

`dogerchi.Use` accepts any `chi.Router`, so mount it on `r.With(auth)` or inside `r.Group`. `debugAuth` is the same `net/http` middleware as in [Without framework](#without-framework).

```go
import (
    "github.com/go-chi/chi/v5"
    dogerchi "github.com/kartaladev/doger/http/chi"
)

func main() {
    injector := startProgram()

    auth, err := debugAuth(os.Getenv("DEBUG_USER"), os.Getenv("DEBUG_PASSWORD"))
    if err != nil {
        log.Fatal(err)
    }

    r := chi.NewRouter()
    dogerchi.Use(r.With(auth), "/debug/doger", injector)

    log.Fatal(http.ListenAndServe("127.0.0.1:8081", r))
}
```
