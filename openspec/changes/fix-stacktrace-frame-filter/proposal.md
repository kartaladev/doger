# Proposal

## Why

The container records the frame where each service was provided (shown as `Invoked:` by `ExplainService` / `ExplainNamedService`) and the frames where it was invoked (recorded internally). The code that picks those frames decides which frames belong to the `stacktrace` package itself, and so must be skipped, by looking for the substring `do/stacktrace/` in the **file path**. It also decides which frames belong to GOROOT by a substring match against `runtime.GOROOT()`. Both rules break outside one specific layout.

In this repository (checked out as `doger`) the suite is red on unmodified code. That blocks `rename-module-doger`, which needs a green baseline, and every later change. The same defect hits every upstream v2 consumer that builds from the module cache or a vendor directory, and every `-trimpath` build.

## What Changes

- `stacktrace.NewFrameFromCaller` identifies its own frames (frames of the `stacktrace` package) by the **package import path of the frame's function**, derived at runtime from that package (via `reflect.TypeOf(...).PkgPath()`). It no longer matches substrings of the file path against a hard-coded name (`packageName = "do"`).
  - It keeps the contract the existing tests pin: report the first frame outside the `stacktrace` package and GOROOT, even when that frame is other library code. For example, the eager provider frame stays `newServiceEager` in `service_eager.go` (`service_test.go:46-47`).
  - Frames from `_test.go` files are still reported, as today.
  - Skipping all library frames to reach user code would be a behavior change and is out of scope.
- GOROOT frames are identified by a **path-prefix** match: the file path starts with GOROOT followed by `/`. When `runtime.GOROOT()` is empty (a `-trimpath` build run without a `GOROOT` environment variable), no frame is treated as a GOROOT frame. Today an empty GOROOT makes every frame a GOROOT frame, and a sibling directory such as `/usr/local/gopath` counts as inside `/usr/local/go`.
- Detection of the `stacktrace` package's own frames becomes independent of:
  - the checkout directory name
  - GOPATH, module-cache or vendor layout
  - the module path itself, so `rename-module-doger` needs no change to this logic
- Test assertions that hard-code the `do/` directory prefix (`stacktrace_test.go:31,44,57`) assert the package-relative suffix instead (`stacktrace/stacktrace_test.go`).
- New regression tests: a provider-frame test through the exported `ExplainService` API, classifier tables for layouts, GOROOT prefix and empty GOROOT, and a search-depth table (see design.md, Test Strategy).
- No exported identifier or signature changes. The observable change: the `Frame` values returned by `NewFrameFromCaller`, and so the `Invoked:` frame shown by `ExplainService` / `ExplainNamedService`, point at the real caller in every checkout, install layout and `-trimpath` build.

## Capabilities

### New Capabilities

- `service-source-tracking`: how the container determines and reports where a service was provided and invoked. Covers which stack frames are skipped (the `stacktrace` package's own frames and GOROOT frames) and which frame is reported as the caller. Must hold regardless of checkout directory, GOPATH, module-cache or vendor layout, `-trimpath`, and module path.

### Modified Capabilities

None (no existing specs).

## Impact

- `stacktrace/stacktrace.go` (frame classification) and `stacktrace/stacktrace_test.go` (directory-dependent assertions).
- New test files: `stacktrace/stacktrace_internal_test.go` (classifier and search-depth tables, symbol-prefix guard) and `di_explain_frame_test.go` (package `do_test`, provider frame through `ExplainService`).
- Indirectly: `service_eager.go`, `service_alias.go` and `service_lazy.go` frame tracking, and the `Invoked:` line of `di_explain.go` output. Their existing tests (`service_test.go`, `service_alias_test.go`) become green; `service_alias_test.go` gains one file assertion.
- Must land **before** `rename-module-doger`. It is written against the current module path, and the rename then carries it over mechanically.

## Defaults & Control

No new knob. Frame classification is internal diagnostic behavior with one correct answer, "the first frame outside the `stacktrace` package and GOROOT", so there is nothing to override. Consumers who want different provenance can already read `ExplainServiceOutput.Invoked` (a `stacktrace.Frame`) and filter it themselves.

## Defect

**1. Checkout directory (reproduced).** In a checkout whose directory is not named `do` (here `.../RND/doger`), `make test` fails on unmodified code with 6 failing tests:
- `stacktrace`: `TestStacktrace`, `TestNewFrameFromPtr`, `TestFrame_String`
- root package: `TestInferServiceProviderStacktrace`, `TestServiceAlias_getInstance`, `TestServiceAlias_getInstanceAny`

For example, `NewFrameFromCaller()` called from `example1` returns:

```
File:     .../doger/stacktrace/stacktrace.go
Function: NewFrameFromCaller
Line:     47
```

**Expected:**

```
File:     .../stacktrace/stacktrace_test.go
Function: example1
Line:     12
```

*Cause.* `stacktrace.go:19-21,60-61` matches `strings.Contains(file, "do/stacktrace/")`. The path `.../doger/stacktrace/stacktrace.go` does not contain it, so the `stacktrace` package's own frame (`NewFrameFromCaller`) is reported as the caller. The rule only holds when the checkout directory name ends in `do`.

*Minimal reproduction.*
1. Copy the repository unchanged into a directory named `do` and into one named `doger`.
2. Run `go test -race -count=1 . ./stacktrace/` in each.
3. Result: `ok` in `do`, the 6 failures above in `doger`. Reproduced.

**2. Module cache and vendor directory (reproduced).** Upstream module `github.com/samber/do/v2` always lives at `.../pkg/mod/github.com/samber/do/v2@<version>/stacktrace/`, or at `vendor/github.com/samber/do/v2/stacktrace/`. Neither path can contain `do/stacktrace/`. So every upstream v2 consumer that builds from the module cache or vendors this filter gets the `stacktrace` package's own frame as the provider and invocation frame. Reproduction:
- A scratch consumer requiring `github.com/samber/do/v2 v2.1.0` from the module cache calls `do.ProvideValue(i, &DB{})`, `do.MustInvoke[*DB](i)` and prints `do.ExplainService[*DB](i)`. Output: `Invoked: .../pkg/mod/github.com/samber/do/v2@v2.1.0/stacktrace/stacktrace.go:NewFrameFromCaller:47`.
- The same consumer, with `replace` pointing at an unmodified copy in a directory named `do` and built with `go mod vendor` / `-mod=vendor`, prints `Invoked: .../vendor/github.com/samber/do/v2/stacktrace/stacktrace.go:NewFrameFromCaller:47`. So vendoring breaks even a `do` checkout.
- Expected in both: `Invoked: .../service_eager.go:newServiceEager[...]:31`, the value a `do` checkout gives today.

**3. Empty GOROOT under `-trimpath` (reproduced).** `runtime.GOROOT()` returns `""` in a `-trimpath` binary run without a `GOROOT` environment variable, and `strings.Contains(file, "")` is always true (`stacktrace.go:59`). Every frame then counts as a GOROOT frame and nothing is found. Reproduction:
- In an unmodified copy in a directory named `do`, `env -u GOROOT go test -trimpath -count=1 -run 'TestStacktrace$' ./stacktrace/` fails: found is `false`, function `""`, line `0`. Without `-trimpath` it passes.
- The v2.1.0 consumer above, built with `-trimpath` and run without `GOROOT`, prints `Invoked: ::0`.

**4. GOROOT sibling directory (string fact; proven by a failing test before it is fixed).** `strings.Contains("/usr/local/gopath/src/app/main.go", "/usr/local/go")` is true, so with GOROOT `/usr/local/go` a frame in `/usr/local/gopath/...` is skipped as a GOROOT frame. This was not reproduced end to end. The classifier row `goroot_sibling_gopath` proves it by failing on its assertion against the substring rule (tasks.md 2.2) before the prefix fix (2.3) lands.
