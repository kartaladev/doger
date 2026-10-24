# Proposal

## Why

The module declares `go 1.18`, which rules out language and standard-library features the upcoming lifecycle work depends on:
- generic methods (1.27), for ergonomic registries and builders on concrete types
- `testing/synctest` (1.25), for deterministic tests of start/shutdown timeouts and concurrency
- `slices`/`maps` (1.21) and per-iteration loop variables (1.22), which simplify existing helpers

Divergence from upstream is allowed, so the floor can move to the current release.

## What Changes

- **BREAKING**: Minimum supported Go version becomes 1.27 (`go` directive in every `go.mod` and in `go.work`). Consumers on Go 1.26, which is still supported by the Go team, can no longer build the module.
- Replace hand-written helpers in `utils.go` with standard-library equivalents where behavior is identical (e.g. `keys`, `filter`, `contains`, `reverseSlice` → `maps`/`slices`), keeping unexported wrappers only where semantics differ.
- Remove per-iteration capture workarounds that Go 1.22 loop-variable semantics make unnecessary (e.g. `go func(n string){...}(name)` in `scope.go`).
- Adopt `sync.WaitGroup.Go` and other modernizations flagged by `gopls` modernize analyzers / `golangci-lint`, where they do not change behavior.
- Update CI matrix and lint configuration to Go 1.27.
- No exported API or runtime behavior changes beyond the version floor.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None. Tooling and refactor only. This change should set `skip_specs: true` in its `.openspec.yaml` when the specs artifact is reached.

## Impact

- `go.mod` ×25, `go.work`, CI workflows, `.golangci.yml`.
- `utils.go`, `scope.go`, and other files touched by modernization. Each modernization is a behavior-preserving refactor step with `make test` and `make lint` run after it.
- Unlocks `testing/synctest` and generic methods for `add-start-lifecycle-and-kinds` and `add-container-lifecycle-hooks`.
- Depends on `rename-module-doger`.

## Defaults & Control

No new runtime behavior or knobs. The only consumer-visible effect is the Go 1.27 floor. It has no override because the following changes use 1.27-only language features. This is a deliberate trade-off, recorded as a breaking change.
