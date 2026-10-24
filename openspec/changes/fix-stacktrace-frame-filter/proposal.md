# Proposal

## Why

The container records where each service was provided and invoked, and uses those frames for `ExplainService`, `ExplainInjector` and debugging output. The code that picks those frames decides which frames belong to the `stacktrace` package itself, and so must be skipped, by looking for substrings in the **file path**, so it works only when the source sits under a directory literally named `do`.

In this repository (checked out as `doger`) the suite is red on unmodified code. That blocks `rename-module-doger`, which needs a green baseline, and every later change.

## What Changes

- `stacktrace.NewFrameFromCaller` identifies its own frames (frames of the `stacktrace` package) by the **package import path of the frame's function**, derived at runtime from that package (e.g. via `reflect.TypeOf(...).PkgPath()`). It no longer matches substrings of the file path against a hard-coded name (`packageName = "do"`).
  - It keeps the contract the existing tests pin: report the first frame outside the `stacktrace` package and GOROOT, even when that frame is other library code. For example, the eager provider frame stays `newServiceEager` in `service_eager.go` (`service_test.go:46-47`).
  - Frames from `_test.go` files are still reported, as today.
  - Skipping all library frames to reach user code would be a behavior change and is out of scope.
  - Frames in GOROOT are still skipped.
- Detection of the `stacktrace` package's own frames becomes independent of:
  - the checkout directory name
  - GOPATH versus module-cache layout
  - the module path itself, so `rename-module-doger` needs no change to this logic
- Test assertions that hard-code the `do/` directory prefix (`stacktrace_test.go:31,44,57`) assert the package-relative suffix instead (`stacktrace/stacktrace_test.go`).
- No exported identifier or signature changes. The observable change: frames returned by `NewFrameFromCaller`, and so the invocation frames shown by the explain functions, point at the real caller in every checkout and install layout.

## Capabilities

### New Capabilities

- `service-source-tracking`: how the container determines and reports where a service was provided and invoked. Covers which stack frames are skipped (the `stacktrace` package's own frames and GOROOT) and which frame is reported as the caller. Must hold regardless of checkout directory, GOPATH or module-cache layout, and module path.

### Modified Capabilities

None (no existing specs).

## Impact

- `stacktrace/stacktrace.go` (frame classification) and `stacktrace/stacktrace_test.go` (directory-dependent assertions).
- Indirectly: `service_*.go` invocation-frame tracking and `di_explain.go` output. Their existing tests (`service_test.go`, `service_alias_test.go`) become green without modification.
- Must land **before** `rename-module-doger`. It is written against the current module path, and the rename then carries it over mechanically.

## Defaults & Control

No new knob. Frame classification is internal diagnostic behavior with one correct answer, "the first frame outside the `stacktrace` package", so there is nothing to override. Consumers who want different provenance can already read `ServiceDescription` / explain output and filter it themselves.

## Defect

**Observed.** In a checkout whose directory is not named `do` (here `.../RND/doger`), `make test` fails on unmodified code with 6 failing tests:
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

**Cause.** `stacktrace.go:19-21,60-61` matches `strings.Contains(file, "do/stacktrace/")`. The path `.../doger/stacktrace/stacktrace.go` does not contain it, so the `stacktrace` package's own frame (`NewFrameFromCaller`) is reported as the caller.

**Minimal reproduction.**
1. Copy the repository unchanged into a directory named `do` and into one named `doger`.
2. Run `go test -race -count=1 . ./stacktrace/` in each.
3. Result: `ok` in `do`, the 6 failures above in `doger`. Reproduced in this session.

**Unverified, to be proven by a test before it is claimed.** Library code installed in a consumer's module cache lives at `.../pkg/mod/github.com/<owner>/<module>@<version>/stacktrace/...`. That path likely does not match either, so consumers may get wrong invocation frames too. The fix starts with a RED regression test that classifies such a module-cache path. That test establishes whether this case is affected.
