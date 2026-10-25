# Design

## Context

See proposal.md for the motivation and `specs/module-identity/spec.md` for the required behavior. The current layout has 25 modules joined by `go.work`. Their paths are inconsistent:

| Module | Current path |
|---|---|
| root | `github.com/samber/do/v2` (package `do`) |
| `stacktrace`, `http`, `tests`, `tests/fixtures`, `bench` | packages inside the root module (`http` is package `dohttp`) |
| `http/{chi,echo,fiber,std}` | `github.com/samber/do/http/<x>/v2` (a sibling of the root path, not nested under it) |
| `http/gin` | `github.com/samber/do/http/gin/v2`, package `gin`, which clashes with the framework's own package name |
| `examples/*` (except `http/gin`) | `github.com/samber/do/v2/examples/<x>` |
| `examples/http/gin` | `github.com/samber/do/examples/http/gin/v2` (outlier) |

Other facts that shape the approach:
- **Module wiring.** Every sub-module reaches the root through `replace github.com/samber/do/v2 => ../../` (or `../../../`) plus `require github.com/samber/do/v2 v2.0.0-00010101000000-000000000000`. `examples/http/*` and `examples/web-application` also replace and require their `http/<x>` module the same way. There are 85 `samber/do` lines across the 25 `go.mod` files.
- **Imports.** 78 import specs reference `samber/do` today, in 49 Go files outside the root package; `fix-stacktrace-frame-filter` adds a 79th (in `di_explain_frame_test.go`).
- **Import aliases.** The root import is aliased `do` in exactly 2 files: `http/pages_test.go:7` and `http/utils_test.go:7`. The sub-packages are imported under these aliases:

  | Alias | Files that use it |
  |---|---|
  | `dohttp` | `http/{chi,echo,fiber,gin,std}/controllers.go` |
  | `dochi` | `examples/http/chi/main.go`, `examples/web-application/main.go` |
  | `doecho` | `examples/http/echo/main.go` |
  | `dofiber` | `examples/http/fiber/main.go` |
  | `dogin` | `examples/http/gin/main.go` |
  | `ginhttpstd` | `examples/http/std/main.go` |

  Inside `http/gin` and `examples/http/gin`, the bare qualifier `gin.` belongs to `github.com/gin-gonic/gin`.
- **Package files.** All 37 root-package files declare `package do` today. `fix-stacktrace-frame-filter`, which lands first, adds one more root-directory file, `di_explain_frame_test.go`, declaring the external test package `package do_test` and importing the root unaliased (`do.ExplainService`, …); it is the only external `_test` package. The only `_test.go` files under `http/` sit in the root module's `http` package. The `http/*` and `examples/*` modules have none.
- **Library type names in output.** Service names inferred from types embed the import path: `NameOf[*RootScope]()` is `*github.com/samber/do/v2.RootScope`. These names appear in 127 lines across 10 root test files, 3 of them in the outputs of 2 Examples (`ExampleNameOf`, `ExampleExplainService`):
  - per file: `di_test.go` 40, `service_alias_test.go` 39, `invoke_test.go` 11, `service_test.go` 9, `di_lifecycle_test.go` 8, `service_transient_test.go` 8, `service_eager_test.go` 5, `service_lazy_test.go` 4, `di_explain_example_test.go` 2, `di_example_test.go` 1.

  A trial rename made 15+ root tests and both Examples fail with `expected: "github.com/samber/do/v2.eagerTest" actual: "github.com/kartaladev/doger.eagerTest"`. This is observable behavior, hence the `module-identity` spec.
- **Debug UI branding.** It lives in string literals:
  - `http/index.go:29-41`: title, the heading "Welcome to do UI", and links to `samber/do`, `do.samber.dev` and `pkg.go.dev/github.com/samber/do/v2`;
  - page titles in `http/service.go:54,178` and `http/scope.go:38`.
- **Godoc.** 86 doc-comment lines in non-test files of the root and `http` packages show usage with the `do.` qualifier (e.g. `di_lifecycle.go:139`). Adapter doc comments say "the do library".
- **Unchanged identifiers.** `DefaultStructTagKey = "do"` (`di.go:11`), and every error sentinel starts with `DI: ` (`errors.go:12-15`).
- **Tooling coverage.** `make test` covers the root module only.
- **Versions and hosting.**
  - There are no tags, locally or on the remote.
  - The repository lives at `github.com/kartaladev/doger` (created 2026-10-24 as a standalone repository with the full history of `zakyalvan/doger`, a fork of `samber/do`). `proxy.golang.org` returns 404 for the new path until the first tag.
- **Docs site.** Upstream identity and accounts:
  - `docs/docusaurus.config.ts`: `url: https://do.samber.dev`, `organizationName: 'samber'`, and the identity links;
  - upstream analytics and search keys: Ahrefs `data-key`, gtag `G-ZQ0MR5WG9T`, PostHog `apiKey` with `appUrl: https://hogpost.samber.dev`, Algolia `appId 9Q5MHPPFJM` with insights;
  - the sponsor entry DBOS (`utm_campaign=gh-smbr`) and `github.com/sponsors/samber` links;
  - `.github/FUNDING.yml` (`github: [samber]`).

  The site has 28 files with `samber/do`, about 200 occurrences, many of them product-name prose.
- **LICENSE.** It is MIT, copyright Samuel Berthe, and its notice must be preserved in a fork.

## Goals / Non-Goals

**Goals:**
- Satisfy every requirement of `specs/module-identity/spec.md`.
- One consistent path scheme: `github.com/kartaladev/doger[/<subdir>]`, with no major-version suffix.
- Every workspace module builds, vets, tests and stays `go mod tidy`-clean.
- The docs site and repository metadata identify this fork and stop reporting to upstream's accounts, while keeping attribution.

**Non-Goals:**
- Changing any exported identifier other than package names. Changing `DefaultStructTagKey`, the `DI: ` error prefix, or the debug UI routes (pinned unchanged by the spec).
- Rewriting upstream history references: issue links such as `samber/do/issues/114` (`invoke.go:160,270`), the docs "v1" link, upstream releases history, and `docs/docs/upgrading/from-v1-x-to-v2.md` (it stays as upstream history, with a pointer to the new migration page).
- Replacing `go.dev/play` links in doc comments (5 files). They run upstream code. This is a docs decision, tracked as a follow-up.
- Go version changes (`upgrade-go-1-27`).
- Publishing the `http/*` sub-modules (prefix tags, dropping the placeholder requires). See Open Questions.

## Decisions

**D1. Path scheme `github.com/kartaladev/doger/<subdir>`, no `/v2`, and v0 placeholder requires.**
- Sub-module paths mirror their directories: `.../doger/http/chi`, `.../doger/examples/http/gin`, and so on. This removes the sibling-path quirk and the gin outlier.
- Every `require` of `github.com/kartaladev/doger` or `github.com/kartaladev/doger/http/<x>` uses the placeholder `v0.0.0-00010101000000-000000000000`, paired with the same local `replace` directory as today.
- The old `v2.0.0-…` placeholder is invalid on a path without `/v2`: Go rejects the file with `require github.com/kartaladev/doger: version "v2.0.0-00010101000000-000000000000" invalid: should be v0 or v1, not v2`. `go mod edit -require` still writes it (exit 0), and afterwards no `go` command can parse that `go.mod`.
- Verification: `go mod edit -json` in all 25 modules, and `go vet ./...` in each. A trial with the v0 placeholder vetted clean in all 25.
- *Alternative:* keep `/v2` to signal continuity. Rejected: there is no v1 of this module, and Go only needs a suffix from v2 onward.

**D2. Package names and import aliases.**

| Directory | Old package | New package |
|---|---|---|
| `.` | `do` | `doger` |
| `http` | `dohttp` | `dogerhttp` |
| `http/chi` | `dochi` | `dogerchi` |
| `http/echo` | `doecho` | `dogerecho` |
| `http/fiber` | `dofiber` | `dogerfiber` |
| `http/gin` | `gin` | `dogergin` |
| `http/std` | `dohttpstd` | `dogerhttpstd` |

`stacktrace`, `tests`, `fixtures` and `bench` keep their names because they match their directories.

Import aliases are renamed together with their qualifiers, file by file:
- `dohttp`→`dogerhttp`, `dochi`→`dogerchi`, `doecho`→`dogerecho`, `dofiber`→`dogerfiber`, `dogin`→`dogergin`, `ginhttpstd`→`dogerhttpstd`.
- The two `do` aliases on the root import are dropped.
- The new aliases equal the package names. They stay explicit, because the directory name differs from the package name.
- **The bare `gin.` qualifier is never rewritten.** It is the gin framework.

Alternatives:
- Keep the `do*` prefixes. Rejected: they would no longer relate to the module name.
- Use bare directory names (`chi`, `gin`). Rejected: they clash with the adapted frameworks.

**D3. Mechanical tools, applied in this order:**
1. `go mod edit -module`, `-replace`/`-dropreplace` and `-require` (with the D1 placeholder) for each module.
2. Import paths in `.go` files: a scripted exact-string substitution limited to import specs, then `gofmt`.
3. **Package clauses by direct edit** of the `package` line (`do` → `doger`, and `do_test` → `doger_test` in `di_explain_frame_test.go`), followed by `gopls` diagnostics.
   - Do not use gopls package rename. On `http` it moves the directory: `gopls rename -l http/index.go:1:9 dogerhttp` lists `dogerhttp/const.go`, … as targets, which breaks D1.
   - On module-root packages it refuses: `cannot rename package: module path "…/http/chi" is the same as the package path`.
4. Qualifiers with `gofmt -r`:
   - `gofmt -r 'do.a -> doger.a' -w` only on files that import the root package without an alias (including `di_explain_frame_test.go`), after dropping the two `do` aliases;
   - `gofmt -r '<old>.a -> <new>.a' -w` only on the files of the D2 alias table, in the same edit as the alias rename in their import spec.

   `gofmt -r` is AST-based, so strings and comments are untouched.
- *Alternative:* a blind `sed` over every file. Rejected: it would rewrite upstream links, `gin.` selectors and `do:""` tags.

**D4. Expected library type names in tests are the RED step.** In the 127 lines of the 10 root test files listed in Context (3 of them in Example outputs), replace the literal prefix `github.com/samber/do/v2.` with `github.com/kartaladev/doger.`.
- The trailing `.` keeps import specs such as `"github.com/samber/do/v2/stacktrace"` out of the substitution.
- Before the rename, those assertions fail with the old path as the actual value. After it, they pass.
- The `shortFuncName("github.com/samber/do/v2.(*ServiceLazy…")` inputs in `stacktrace/*_test.go` (3 lines) are path-agnostic inputs to a pure function. They stay and are allow-listed.

**D5. Debug UI branding** (`http/index.go`, `http/service.go`, `http/scope.go`).
- Index page title: "Dependency injection UI - doger". Heading: "Welcome to doger UI ✌️".
- Index links:

  | Label | Target |
  |---|---|
  | Repository | `https://github.com/kartaladev/doger` |
  | New issue | `.../issues` |
  | Documentation | `https://kartaladev.github.io/doger/` (the interim docs URL, D7) |
  | Godoc | `https://pkg.go.dev/github.com/kartaladev/doger` |
  | Changelog | `.../releases` |
  | License | `.../blob/main/LICENSE` |
  | Upstream project | `https://github.com/samber/do` (new attribution link) |

- Page titles: "Inspect scope tree - doger", and "Inspect service - doger" (both service pages).
- Routes and base-path handling are untouched.

**D6. Godoc in comments.**
- In non-test `.go` files of the root, `http` and `stacktrace` packages, rewrite `\bdo\.([A-Z])` to `doger.$1` on comment lines only (scripted, then reviewed).
- Hand-edit the prose "the do library" / "the do package" to "the doger library" / "the doger package", including the `http/*` adapter doc comments.
- `do:""` struct tags and `/debug/do` paths in comments are not matched and stay.

**D7. Docs, README and site identity.**
- **Prose rule:**
  - Say "doger" wherever the text means this library: product name, how-to text, comparisons of this library against uber/dig, uber/fx and google/wire.
  - Keep "samber/do" where the text means upstream: attribution, upstream history (v1, v1→v2 upgrade page, releases), upstream links and templates, and any sentence contrasting doger with upstream.
  - Keep file names, slugs and explicit heading IDs (e.g. `comparison/samber-do-vs-uber-fx.md`, `{#samberdo-vs-uberdig-vs-uberfx-vs-googlewire}`) unchanged. The site uses `onBrokenLinks`/`onBrokenAnchors: 'throw'`, and renamed slugs would break inbound links.
- **Code snippets:** import paths, `go get` lines and the `do.` qualifier become `doger.`. The `do:""` tags and `/debug/do` routes stay.
- **Files:**
  - `README.md`, `docs/docusaurus.config.ts`, `docs/plugins/structured-data/index.ts` and `docs/static/llms.txt`;
  - `docs/static/robots.txt` (`Sitemap:`) and every file under `docs/docs` and `docs/src`;
  - a new page `docs/docs/migrating/migrating-from-samber-do.md` with the migration steps from "Public API & Defaults".
- **Interim docs URL** (final domain in Open Questions): GitHub Pages under the organization, `url: 'https://kartaladev.github.io'` and `baseUrl: '/doger/'`, with `organizationName: 'kartaladev'` and `projectName: 'doger'`.
  - Every absolute `https://do.samber.dev` reference moves to `https://kartaladev.github.io/doger/…`: the og:image tags, the sitemap filter, the `about.md` JSON-LD ids, `robots.txt`, the README links and the debug UI link.
  - Site title "doger - Go Dependency Injection", navbar title "⚙️ doger", `og:site_name` "doger", and `editUrl` `https://github.com/kartaladev/doger/tree/main/docs/`.
  - Repository, issues, releases, LICENSE and pkg.go.dev links point to `kartaladev/doger`.
  - The version dropdown keeps one link to the upstream v1 tree, labelled as upstream.
- **Upstream accounts removed.** Our site must not report to upstream's accounts or advertise upstream's sponsors. Remove:
  - the Ahrefs `headTags` script;
  - the preset `gtag` block;
  - the `posthog-docusaurus` plugin entry and its npm dependency (`npm uninstall posthog-docusaurus`);
  - the `algolia` search block, whose index and insights belong to upstream;
  - `customFields.sponsors`;
  - the navbar and footer "💖 Sponsor", Twitter and Stack Overflow (`samber+do`) links;
  - `.github/FUNDING.yml`.

  The theme components call `window.posthog?.capture`, which is a no-op without the plugin, so they stay. `@docusaurus/plugin-vercel-analytics` has no account key and reports to whichever Vercel project deploys the site, so it stays.
- **structured-data:**
  - Set `name: 'doger'`, `codeRepository` and `license` to `kartaladev/doger`.
  - Add `isBasedOn: 'https://github.com/samber/do'`.
  - Keep the upstream author credit.

**D8. Attribution is never removed.**
- In `LICENSE`, keep `Copyright (c) 2022 Samuel Berthe` and the MIT text unchanged, and add `Copyright (c) 2026 kartaladev` directly below it.
- `README.md` and the docs site get a visible fork notice: "doger is a fork of [samber/do](https://github.com/samber/do) by Samuel Berthe". The debug UI gets the "Upstream project" link (D5).
- The palantir/stacktrace credit in `stacktrace/stacktrace.go` stays.

**D9. Repository metadata.**
- `release.yml` gets a first step that fails unless `inputs.semver` matches `^v[01]\.[0-9]+\.[0-9]+$`. A `v2+` tag on a path without `/v2` is not a valid version of this module. The `triggering_actor == 'samber'` gate is out of scope (Open Questions).
- `CLAUDE.md` line 3 and the `openspec/config.yaml` `context` name the module `github.com/kartaladev/doger` (package `doger`), "a fork of `github.com/samber/do/v2`".

**D10. Hosting prerequisite.** The `kartaladev/doger` GitHub repository must exist before merge, tag or publication of links (proposal Prerequisites). Task 1.1 checks it.

**D11. Reference allow-list.** After the change, `samber/do` may appear only in these places:
- upstream issue links (`invoke.go:160,270`);
- the v1 tree, upstream releases, the upgrading page and the upstream templates (`do-template-*`);
- `go.dev/play` links;
- the `LICENSE` copyright;
- the fork notices, attribution links and the structured-data `isBasedOn`;
- prose that means upstream (D7) and file slugs or heading IDs kept for link stability;
- `openspec/`, plus `CLAUDE.md` and `openspec/config.yaml` attribution;
- the `shortFuncName` inputs (D4). The `stacktrace/stacktrace_internal_test.go` classification rows are not on this list: they move to `github.com/kartaladev/doger` together with the `service-source-tracking` MODIFIED delta (task 7.3), and `own_module_cache_renamed` uses `example.com/fork` as the other module path.

`github.com/samber/go-type-to-string` is a separate dependency and stays.

## Risks / Trade-offs

- [A qualifier rewrite hits a selector it should not, e.g. gin-gonic's `gin.`] → Rewrites are per alias and per file (D2/D3), never on `gin.`. Every module is compiled and vetted afterwards.
- [The gopls package rename moves a directory] → Not used (D3.3).
- [Sub-modules are not covered by `make test`] → Per-module `go vet ./... && go test -race ./...` in group 4 and group 8.
- [A test is silently dropped] → Diff the test inventory before and after; the only additions are the spec tests.
- [An upstream reference is rewritten by mistake, or a module reference is missed] → Allow-list sweep (D11) against the baseline from task 1.4.
- [`baseUrl` `/doger/` breaks absolute or internal docs links] → `npm run build` fails on broken links and anchors (`throw`); all absolute `do.samber.dev` URLs are listed in D7.
- [Removing Algolia leaves the site without search] → Accepted for the interim. A self-owned search is an Open Question.
- [Consumer code or persisted names break] → Accepted, flagged BREAKING, with migration below. There are no external consumers yet.

## Public API & Defaults

- **BREAKING: import path and package name.** `github.com/samber/do/v2` (package `do`) becomes `github.com/kartaladev/doger` (package `doger`).
  - Migration: replace the import path, then either rename `do.` to `doger.`, or alias the import as `do "github.com/kartaladev/doger"`.
- **BREAKING: sub-module paths and package names** (D1/D2). For example, `github.com/samber/do/http/chi/v2` (`dochi`) becomes `github.com/kartaladev/doger/http/chi` (`dogerchi`).
  - Migration: replace the paths. Existing import aliases keep working.
- **BREAKING: inferred names of library-declared types.** `*github.com/samber/do/v2.RootScope` becomes `*github.com/kartaladev/doger.RootScope`, in `NameOf`, service lists, explain output and error messages.
  - Migration: never hard-code these strings. Use `doger.NameOf[T]()`, or register library types under explicit names (`ProvideNamed*`).
  - Names of consumer types and explicit names are unchanged.
- **BREAKING (cosmetic): debug UI titles, heading and links** (D5).
- **Unchanged:**
  - every exported identifier, signature, option and default;
  - `DefaultStructTagKey` stays `"do"`, still overridable with `InjectorOpts.StructTagKey`;
  - the `DI: ` messages of `ErrServiceNotFound`, `ErrServiceNotMatch`, `ErrCircularDependency` and `ErrHealthCheckTimeout`;
  - debug UI routes, relative to the consumer's base path. The examples keep mounting at `/debug/do`.

## Test Strategy

Every `#### Scenario:` of `specs/module-identity/spec.md` maps to one Go test. Each `Examples:` row is a named subtest.

| Scenario | Go test | Kind |
|---|---|---|
| Root module packages have doger import paths and names | `module_identity_test.go: TestModuleIdentity_RootModulePackages/{root,http,stacktrace}`. Import path from the root `go.mod` `module` line (+ `/<dir>`), cross-checked for `.` with `reflect.TypeOf((*RootScope)(nil)).Elem().PkgPath()`; package name from `go/build.ImportDir(dir, 0).Name` | table |
| HTTP adapter modules have doger import paths and names | `module_identity_test.go: TestModuleIdentity_HTTPAdapterModules/{chi,echo,fiber,gin,std}`. Module path from `<dir>/go.mod`; package name from `go/build.ImportDir` | table |
| Workspace module paths mirror their directories | `module_identity_test.go: TestModuleIdentity_WorkspaceModules/<dir>` (25 rows). Iterates the `use` entries of `go.work` and fails on any entry without a row | table |
| Inferred names of library and non-library types | `di_test.go: TestNameOf_LibraryTypes/<row>` (7 rows) | table |
| Explicitly named services keep their names | `di_test.go: TestProvideNamed_NameUnaffectedByModulePath` | unit |
| Not-found errors for library types | `invoke_test.go: TestInvoke_NotFoundNamesLibraryTypes/{empty_scope,int_registered}`, including `errors.Is(err, ErrServiceNotFound)` | table |
| Index page links point to doger resources | `http/pages_test.go: TestIndexHTML_Branding/<label>` (7 rows) | table |
| Index page names the library | `http/pages_test.go: TestIndexHTML` (existing; the heading assertion changes to "Welcome to doger UI", and a title assertion is added) | unit |
| Inspection page titles name the library | `http/pages_test.go: TestPagesHTML_Title/{scope_tree,service_list,service_detail}` | table |
| Upstream branding appears only as attribution | `http/pages_test.go: TestPagesHTML_UpstreamAttributionOnly/{index,scope_tree,service_list,service_detail}` | table |
| Struct injection tag key keeps its default and override | `module_identity_test.go: TestCompat_StructTagKey/{default_do,override_inject,override_ignores_do}` | table |
| Error sentinels keep their messages | `module_identity_test.go: TestCompat_ErrorSentinelMessages/<sentinel>` (4 rows) | table |
| Debug UI links stay relative to the consumer's base path | `http/pages_test.go: TestIndexHTML_BasePathLinks/{debug_do,admin_di}` | table |

**Hot paths and their coverage:**
- **Name inference and resolution.** Covered by `TestNameOf_LibraryTypes`, the 127 updated lines in the existing resolution, alias and lifecycle tests (`di_test.go`, `invoke_test.go`, `service_*_test.go`, `di_lifecycle_test.go`), and the Examples.
- **Public API surface.** The existing suite and Examples compile against package `doger`.
- **Lifecycle, scope traversal and concurrency.** `di_lifecycle_test.go`, `scope_test.go`, `root_scope_test.go`, and the race suite with goleak (`main_test.go`) must pass unchanged apart from the D4 names.

**Behavior-preservation checks** (for everything outside the spec):

| Check | How | Kind |
|---|---|---|
| Compatibility guard green before and after the rename | Group 2 tests run before group 3 and in every later group | characterization |
| Test inventory | Before and after: `go test -list '.*' ./...` per workspace module, attributed by directory. The only additions are the spec tests | regression |
| Root suite green | `make test` | unit / race / goleak |
| Every workspace module builds and passes | For each `dir` in `go list -m -f '{{.Dir}}'`: `go vet ./... && go test -race ./...` | unit / race |
| Placeholder requires | `go mod edit -json` per module: all `github.com/kartaladev/doger*` requires are `v0.0.0-00010101000000-000000000000` | tooling |
| Example tests | `go test -run Example ./...` in the root module | example |
| Modules tidy | `go mod tidy` per module leaves no diff beyond the path rewrites and the re-sorted `require` lines | tooling |
| Lint | `make lint` | lint |
| Docs | `cd docs && npm ci && npm run lint && npm run build` | docs |
| References | Allow-list sweep (D11) | tooling |
| Package names | `go list -f '{{.ImportPath}} {{.Name}}' ./...` per module matches D2 | tooling |

## Migration Plan

1. Group 1: check hosting and record the baselines.
2. Group 2: add the compatibility guard tests before anything changes.
3. Groups 3 to 5: RED, GREEN, REFACTOR per requirement cluster: the root module, then the sub-modules, then the debug UI. Each group carries its own godoc and docs-page updates.
4. Group 6 (docs site identity and upstream accounts), then group 7 (repository metadata).
5. Group 8: integration checks.

Rollback: revert the commit series. No published artifact depends on the new path.

## Open Questions

- **Final docs domain and hosting.** Interim default: GitHub Pages at `https://kartaladev.github.io/doger/` (D7). Upstream's `vercel.json` stays untouched until a host is chosen. Changing the domain later is a config-only edit.
- **Release gating.** `release.yml` gates on `github.triggering_actor == 'samber'`, so the fork cannot release until the gate is changed. That is fork administration and is independent of this design. D9 already adds the v0/v1 guard.
- **Sub-module publishing.** The `http/*` modules keep `replace` plus placeholder requires. External consumers will need tagged versions (`http/<x>/v0.y.z`) and real requires. Pre-existing, separate release-process change.
- **Tooling.** Whether `make test` should cover every workspace module (separate tooling change), and whether the docs site gets a self-owned search after Algolia's removal.
