# Proposal

## Why

This repository is now an independent fork that is allowed to diverge from upstream `samber/do`, but it still publishes itself as `github.com/samber/do/v2` with package name `do`. Consumers cannot depend on the fork without `replace` directives, and every later change (Go 1.27, start lifecycle, component kinds) would otherwise ship under someone else's module path. Renaming first keeps the feature diffs that follow free of mechanical noise.

## What Changes

- **BREAKING**: Root module path changes from `github.com/samber/do/v2` to `github.com/kartaladev/doger`. The `/v2` major suffix is dropped; versioning restarts at v0.
- **BREAKING**: Root package name changes from `do` to `doger`, so the package name matches the last import path element (Go idiom). Consumer code changes from `do.Provide(...)` to `doger.Provide(...)`.
- **BREAKING**: Sub-module paths and package names change: `http` (`dohttp` → `dogerhttp`), `http/{chi,echo,fiber,gin,std}` (`github.com/samber/do/http/<x>/v2` → `github.com/kartaladev/doger/http/<x>`, packages `dogerchi`, `dogerecho`, `dogerfiber`, `dogergin`, `dogerhttpstd`), and every `examples/*` module.
- **BREAKING (observable output)**: Service names the container infers from types declared in the library embed the import path, so they change, e.g. `*github.com/samber/do/v2.RootScope` → `*github.com/kartaladev/doger.RootScope`. The same names appear in error messages and explain output. Names of consumer types and explicitly named services do not change.
- **BREAKING (observable output)**: The debug UI shows `doger` branding and links to this repository, with a single attribution link to upstream.
- Unchanged on purpose: the default struct tag key `do`, the `DI: ` prefix of every error sentinel, the debug UI routes (relative to the consumer's base path), and every exported identifier other than package names.
- All 25 `go.mod` files are rewritten, including their `require`/`replace` wiring. `go.work` keeps its `use` entries. There are 78 import specs in Go files (79 once `fix-stacktrace-frame-filter` has landed) and 127 test lines with library type names (3 of them in Example outputs). Godoc comments, README, the docs site (28 files) and the repository metadata are updated as well.
- Attribution is kept: the original `LICENSE` notice stays, a fork notice is added to README and the docs site, and upstream history links stay as upstream references.
- The docs site stops reporting to upstream's accounts: upstream analytics, search and sponsor links are removed.

## Capabilities

### New Capabilities

- `module-identity`: the module and package import paths, the package names, the service names inferred from library types, the debug UI branding, and the identifiers that stay unchanged for compatibility.

### Modified Capabilities

None at planning time. `service-source-tracking` (from `fix-stacktrace-frame-filter`, archived before this change is applied) names the old module path and `do.` qualifiers in its scenarios; task 7.3 adds its MODIFIED delta once that main spec exists, so the archived specs agree with `module-identity`.

## Impact

- Every consumer import path and package qualifier. The fork has no external consumers yet, so the cost is internal only.
- All workspace modules, CI (`release.yml`), lint configuration, README, the docs site, `CLAUDE.md` and `openspec/config.yaml`.
- Tests: the expected library type names in 127 test lines (3 of them in Example outputs) change. That is the RED step of the `module-identity` capability, not a refactor. Every other test edit is a mechanical import-path or qualifier rewrite.
- Depends on `fix-stacktrace-frame-filter`. Without it the baseline is red in any checkout not named `do`. Because that fix derives the stacktrace package path at runtime, the rename needs no change to frame filtering.
- Must land before `upgrade-go-1-27`, `add-start-lifecycle-and-kinds`, `add-container-lifecycle-hooks`, and `add-collection-invocation`.

## Prerequisites

- The GitHub repository `github.com/kartaladev/doger` must exist (transfer of `zakyalvan/doger` to the `kartaladev` organization) before this change is merged, tagged, or its links published. Today it does not resolve (`gh repo view kartaladev/doger` fails, and `proxy.golang.org` returns 404), so `go get`, the pkg.go.dev links and the docs `editUrl` would all 404. Code tasks can proceed before the transfer. Merging cannot.

## Defaults & Control

- Default: consumers import `github.com/kartaladev/doger` and write `doger.X`. Override: alias the import (`do "github.com/kartaladev/doger"`) to keep `do.X` call sites.
- Default: inferred names of library types use the new path. Override: register and invoke such services under explicit names (`ProvideNamed*` / `InvokeNamed`), or compute names with `doger.NameOf[T]()` instead of hard-coding the string.
- Default: struct injection keeps reading the `do` tag. Override, unchanged: `InjectorOpts.StructTagKey`.
- Default: the debug UI is mounted at whatever base path the consumer passes; there is no library default to change.
