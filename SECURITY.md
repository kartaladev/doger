# Security policy

## Supported versions

doger is pre-1.0. Security fixes land on `main` and ship in the next release
of every module (the root module and each adapter are tagged together).
Only the latest release receives fixes.

## Reporting a vulnerability

Please report vulnerabilities privately, never in a public issue or pull
request. Use GitHub's private vulnerability reporting:
<https://github.com/kartaladev/doger/security/advisories/new>.

Include the affected module (for example `github.com/kartaladev/doger` or
`github.com/kartaladev/doger/http/echo`), the version, a description of the
impact and, if you can, a minimal reproduction. We acknowledge a report
within a few days, keep you informed while we work on a fix, and credit you
in the advisory unless you prefer otherwise.

## Vulnerability gate

`make vuln` runs `govulncheck` in every module of the workspace
(`scripts/govulncheck.sh`), each module on its own (`GOWORK=off`) so it is
scanned with the requirements its consumers get. It fails when a module's
code reaches a known vulnerability, unless that advisory is accepted for
that module below. CI runs it on every push, pull request and weekly; the
release workflow runs it before tagging.

## Accepted advisories

An advisory is accepted only when it has no fixed version and the module
does not use the affected functionality. Each row accepts one advisory for
one module directory (as `make modules` prints it); `make vuln` reports it
without failing for that module only. Remove a row as soon as a fixed
version exists, and raise the module's requirement instead.

| Advisory | Module | Reason |
|---|---|---|
| [GO-2026-6875](https://pkg.go.dev/vuln/GO-2026-6875) | `./http/echo` | Echo's reverse proxy forwards a client-supplied `X-Real-IP`. No fixed version exists. The adapter only registers read-only routes on the consumer's `*echo.Group`; it uses neither the proxy middleware nor the client IP. govulncheck reports it as reachable only through route registration (`Group.GET`) and package initialization. |
| [GO-2026-6872](https://pkg.go.dev/vuln/GO-2026-6872) | `./http/echo` | Echo's rate limiter trusts a spoofed `X-Forwarded-For` in its default configuration. No fixed version exists. The adapter does not use the rate limiter or the client IP; the reported path is the same route registration and package initialization. |
| [GO-2026-6875](https://pkg.go.dev/vuln/GO-2026-6875) | `./examples/http/echo` | The example mounts the `./http/echo` adapter on a plain `echo.New()` router; same reason as above. Examples are not released. |
| [GO-2026-6872](https://pkg.go.dev/vuln/GO-2026-6872) | `./examples/http/echo` | Same as above: the example uses neither the rate limiter nor the client IP. |
