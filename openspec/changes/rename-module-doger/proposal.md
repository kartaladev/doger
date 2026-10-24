# Proposal

## Why

This repository is now an independent fork that is allowed to diverge from upstream `samber/do`, but it still publishes itself as `github.com/samber/do/v2` with package name `do`. Consumers cannot depend on the fork without `replace` directives, and every later change (Go 1.27, start lifecycle, component kinds) would otherwise ship under someone else's module path. Renaming first keeps the feature diffs that follow free of mechanical noise.

## What Changes

- **BREAKING**: Root module path changes from `github.com/samber/do/v2` to `github.com/kartaladev/doger`. The `/v2` major suffix is dropped; versioning restarts at v0/v1.
- **BREAKING**: Root package name changes from `do` to `doger`, so the package name matches the last import path element (Go idiom). Consumer code changes from `do.Provide(...)` to `doger.Provide(...)`.
- Sub-packages and sub-modules move under the new path: `stacktrace`, `http` (`dohttp`), `http/{chi,echo,fiber,gin,std}`, `examples/*`, `tests/fixtures`, `bench`.
- Sub-module package names keep a prefix to avoid clashing with the frameworks they adapt (e.g. `http/chi` becomes `dogerchi`, not `chi`). The directory/package-name mismatch is deliberate and documented.
- All 25 `go.mod` files, their `replace` directives, `go.work`, import statements (~273 occurrences), and docs references (~21 pages) are updated.
- Links that point at upstream issues or history (e.g. `samber/do/issues/114`) stay as upstream references. They are not rewritten.
- No exported identifier, signature, or runtime behavior changes. This is a mechanical refactor.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None. No spec-level behavior changes. This change should set `skip_specs: true` in its `.openspec.yaml` when the specs artifact is reached.

## Impact

- Every consumer import path and package qualifier (the fork has no external consumers yet, so the cost is internal only).
- All workspace modules (`go.work`), CI and lint configuration that reference the module path, README, and the docs site.
- Test files change only in import lines and package qualifiers. Strict-TDD note: these edits are mechanical renames, not behavior changes, and the full suite (`make test`) must pass unchanged in behavior before and after.
- Depends on `fix-stacktrace-frame-filter`. Without it the baseline is red in any checkout not named `do`. Because that fix derives the stacktrace package path at runtime, the rename needs no change to frame filtering.
- Must land before `upgrade-go-1-27`, `add-start-lifecycle-and-kinds`, `add-container-lifecycle-hooks`, and `add-collection-invocation`.

## Defaults & Control

No new behavior or knobs. Consumers who need the old identifier can alias the import (`do "github.com/kartaladev/doger"`). No other override is needed because nothing about runtime behavior changes.
