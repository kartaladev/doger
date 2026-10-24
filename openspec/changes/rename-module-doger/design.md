# Design

## Context

See proposal.md for the motivation. The current layout has 25 modules joined by `go.work`. Their paths are inconsistent:

| Module | Current path |
|---|---|
| root | `github.com/samber/do/v2` (package `do`) |
| `stacktrace`, `http`, `tests`, `bench` | packages inside the root module |
| `http/{chi,echo,fiber,std}` | `github.com/samber/do/http/<x>/v2` (sibling of the root path, not nested under it) |
| `http/gin` | `github.com/samber/do/http/gin/v2`, package `gin`, which clashes with the framework's own package name |
| `examples/*` (except `http/gin`) | `github.com/samber/do/v2/examples/<x>` |
| `examples/http/gin` | `github.com/samber/do/examples/http/gin/v2` (outlier) |

Other facts that shape the approach:
- Sub-modules reach the root through `replace github.com/samber/do/v2 => ../../`, and `examples/web-application` also replaces the `http/chi` module.
- 49 Go files outside the root package import the root package. Two of them alias it explicitly as `do`.
- No root package file uses an external `_test` package, so root tests are unaffected by the qualifier change.
- `make test` runs `go test -race ./...` from the root, which covers the root module only. Sub-modules are not exercised by it.
- The repository has no tags yet, so no published version has to stay compatible.
- `LICENSE` is MIT, copyright Samuel Berthe. Its notice must be preserved in a fork.

## Goals / Non-Goals

**Goals:**
- One consistent path scheme: `github.com/kartaladev/doger[/<subdir>]`, with no major-version suffix.
- Root package name `doger`. Sub-package names keep a `doger` prefix so they do not clash with the frameworks they adapt.
- Zero behavior change: the same tests exist and pass before and after.
- Every workspace module builds, vets, tests and stays `go mod tidy`-clean.

**Non-Goals:**
- Changing any exported identifier other than package names.
- Rewriting upstream history references: issue links such as `samber/do/issues/114`, the docs "v1" link, and releases history.
- Replacing `go.dev/play` links in doc comments (5 files). They run upstream code. Tracked as a follow-up because removing them is a docs decision, not part of the rename.
- Go version changes (`upgrade-go-1-27`).
- Changing release-workflow ownership gating (see Open Questions).

## Decisions

**D1. Path scheme `github.com/kartaladev/doger/<subdir>`, no `/v2`.**
Sub-module paths mirror their directories: `.../doger/http/chi`, `.../doger/examples/http/gin`, and so on. This removes both the sibling-path quirk and the gin outlier.
- *Alternative:* keep `/v2` to signal continuity. Rejected: there is no v1 of this module, and Go only needs a suffix from v2 onward.

**D2. Package names.** The root package becomes `doger`. Sub-packages are renamed with a consistent `doger` prefix:

| Directory | Old package | New package |
|---|---|---|
| `http` | `dohttp` | `dogerhttp` |
| `http/chi` | `dochi` | `dogerchi` |
| `http/echo` | `doecho` | `dogerecho` |
| `http/fiber` | `dofiber` | `dogerfiber` |
| `http/gin` | `gin` | `dogergin` |
| `http/std` | `dohttpstd` | `dogerhttpstd` |

`stacktrace`, `tests`, `fixtures` and `bench` keep their names because they match their directories.
- *Alternative:* keep the `do*` prefixes. Rejected: they would no longer relate to the module name.
- *Alternative:* use bare directory names (`chi`, `gin`). Rejected: they clash with the adapted frameworks, forcing import aliases everywhere.

**D3. Mechanical tools, applied in this order:**
1. `go mod edit -module` for each module's own path.
2. `go mod edit -replace` / `-dropreplace` / `-require` for the inter-module wiring. Each local `replace` keeps pointing at the same directory, under the new path.
3. Rewrite import paths in `.go` files: a scripted exact string substitution limited to import specs, then `gofmt`.
4. Rewrite package qualifiers syntactically with `gofmt -r 'do.a -> doger.a'`, and likewise `dochi.a -> dogerchi.a` and so on. In gofmt rewrite rules, single-letter lowercase identifiers are wildcards. The tool is AST-based, so strings and comments are untouched. The two files that alias the import as `do` have their alias removed first.
5. Change package clauses with `gopls` rename of the package name, or by direct edit where gopls does not cover the case, followed by `gopls` diagnostics.
- *Alternative:* a blind `sed` over every file. Rejected: it would also rewrite upstream links in comments and strings that must stay.

**D4. Docs and metadata.**
- In `docs/docusaurus.config.ts`, `README.md` and the `bench` imports, update the module path, pkg.go.dev links, repository URL, `editUrl` (to `https://github.com/kartaladev/doger/tree/main/docs/`; the default branch is `main`) and site title.
- Keep upstream history links (v1 tree, upstream issues).
- **Attribution is never removed.**
  - In `LICENSE`, keep `Copyright (c) 2022 Samuel Berthe` and the MIT text unchanged, and add `Copyright (c) 2026 kartaladev` directly below it. MIT requires keeping the original notice.
  - In `README.md` and the docs site, add a visible statement that doger is a fork of `samber/do` by Samuel Berthe, linking the upstream repository. Existing credits to the upstream author and to third-party sources (e.g. the palantir/stacktrace note in `stacktrace/stacktrace.go`) stay as they are. Only module-identity references are rewritten: import paths, pkg.go.dev links and this repository's URL.

**D5. One change, one commit series, no shims.** Nothing has been published, so no redirect module or deprecated alias package is needed.

## Risks / Trade-offs

- [The qualifier rewrite misses a selector, e.g. inside generated code or build-tagged files] → Compile every workspace module, including all build tags used in the repo. Any `do.` left behind fails to compile.
- [A test is silently dropped, e.g. a file is excluded by a mistaken package clause] → Diff the test inventory before and after (see Test Strategy).
- [An upstream link is rewritten by mistake] → Use an allow-list grep: after the change, `samber/do` may appear only in the listed upstream-reference locations.
- [Sub-modules are not covered by `make test`] → Run tests per workspace module during verification. Changing `make test` itself is out of scope here and noted as a follow-up.
- [Consumer code breaks] → Accepted. This is the stated breaking change, with no external consumers yet.

## Public API & Defaults

- **BREAKING:**
  - Import path `github.com/samber/do/v2` becomes `github.com/kartaladev/doger`, and package `do` becomes `doger`.
  - Sub-module paths and package names change as in D1 and D2.
- No exported identifier, signature, default or option changes.
- Override: consumers who prefer the old qualifier can alias the import, e.g. `do "github.com/kartaladev/doger"`.

## Test Strategy

This change has no spec scenarios (`skip_specs: true`). Its contract is "no behavior change", so the existing suite is the safety net. Every hot path is covered by the tests that already cover it, provided they still exist and still pass:
- public API: `di_test.go`, `di_example_test.go`
- resolution and invocation: `invoke_test.go`, the `service_*_test.go` files
- lifecycle: `di_lifecycle_test.go`
- scope traversal: `scope_test.go`, `root_scope_test.go`
- concurrency: the race-enabled suite with goleak in `main_test.go`

| Check | How | Kind |
|---|---|---|
| Test inventory unchanged | Before and after: `go test -list '.*' ./...` in each workspace module. The sorted lists are identical, apart from package-qualified names. | regression |
| Root suite green under race and goleak | `make test` | unit / race / goleak |
| Every workspace module builds and passes its tests | For each `dir` in `go list -m -f '{{.Dir}}'`: `go vet ./...` and `go test -race ./...` | unit / race |
| Example tests still document the API | `go test -run Example ./...` (root and http modules) | example |
| Modules tidy | `go mod tidy` in each module leaves no diff | tooling |
| Lint clean | `make lint` | lint |
| No stale module references | `grep -rn 'samber/do'` matches only the allow-listed upstream references (issue links, v1 tree, releases, LICENSE copyright) | tooling |
| Package clauses consistent | `go list -f '{{.ImportPath}} {{.Name}}' ./...` in each module matches the D2 table | tooling |
| No compiler errors after the rename | `gopls` diagnostics on touched packages | tooling |

## Migration Plan

1. Record the baseline: the test inventory per module, and `make test` / `make lint` green.
2. Apply D3 steps 1 and 2 to all `go.mod` files. Update `go.work` only if a path changed; directories are unchanged, so `use` entries should stay as they are.
3. Apply D3 steps 3 to 5 for the root package, then each sub-package. Build after each package.
4. Apply D4 (docs, README, LICENSE notice).
5. Run every Test Strategy check.

Rollback: revert the commit series. No published artifact depends on the new path.

## Open Questions

- `release.yml` gates on `github.triggering_actor == 'samber'`, and other workflows may carry upstream ownership assumptions. Updating them is fork administration, not part of the rename. It can be decided independently without affecting this design.
- Whether `make test` should cover every workspace module. It is a follow-up tooling change and does not affect this rename.
