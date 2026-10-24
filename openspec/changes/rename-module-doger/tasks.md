# Tasks

Spec: `specs/module-identity/spec.md`. Behavior groups (3, 4, 5) are RED → GREEN → REFACTOR. Group 2 adds the compatibility guard before anything changes. Groups 6 and 7 are docs and metadata, with no Go behavior. Mechanical edits made by the same scripted rename (import specs, package clauses, aliases, qualifiers) are part of their GREEN or REFACTOR step. The only intended edits to test assertions are those of the RED tasks (design D4). Commands run from the repository root unless a task names a module. Read design.md before each group: the checkbox lines carry only the summary.

## 1. Prerequisites and baseline

- [ ] 1.1 Check hosting (design D10): verify `gh repo view kartaladev/doger --json nameWithOwner,defaultBranchRef` prints `kartaladev/doger` with default branch `main`, and `git remote get-url origin` points at it; if not, stop and record it in the PR description (code tasks may continue; merging is blocked until it resolves).
- [ ] 1.2 Record the test inventory: for each `dir` in `go list -m -f '{{.Dir}}'`, run `go test -list '.*' ./...`, attribute names to package directories, and save the sorted result outside the repo as `baseline-tests.txt`; verify it is non-empty for `.`, `bench`, `http`, `stacktrace` and `tests`.
- [ ] 1.3 Record the green baseline (requires `fix-stacktrace-frame-filter`): verify `make test`, `make lint`, and `go vet ./... && go test -race ./...` in every workspace module all pass; if anything fails, stop and report, and do not fix it here.
- [ ] 1.4 Record the reference baseline: save `grep -rn 'samber/do' --exclude-dir=node_modules --exclude-dir=.git .` as `baseline-refs.txt` with every line tagged keep or rewrite per design D11; verify every line has a tag.

## 2. Compatibility guard (requirement "Compatibility identifiers stay unchanged across the rename")

- [ ] 2.1 Add `module_identity_test.go: TestCompat_StructTagKey` and `TestCompat_ErrorSentinelMessages`, plus `http/pages_test.go: TestIndexHTML_BasePathLinks`, for the three compatibility scenarios (every Examples row as a named subtest); they pass on arrival, so show each can fail by temporarily changing `DefaultStructTagKey`, one sentinel message and the index link template (not committed); verify `go test -race -count=1 -run 'TestCompat_|TestIndexHTML_BasePathLinks' . ./http/` passes.

## 3. Root module identity and inferred names (requirements "Root module packages use the doger import path and package names", "Inferred service names of library types use the doger import path", "Service-not-found errors name library types with the doger import path")

- [ ] 3.1 RED: add `TestModuleIdentity_RootModulePackages`, `TestNameOf_LibraryTypes`, `TestProvideNamed_NameUnaffectedByModulePath` and `TestInvoke_NotFoundNamesLibraryTypes` (scenario rows as named subtests; see design Test Strategy), and rewrite the 127 expected library-type names to `github.com/kartaladev/doger.` (design D4); verify `go test -race -count=1 .` fails only in tests that assert library names or module identity, each showing the actual `github.com/samber/do/v2…` or package `do`, while every other package stays green (`TestProvideNamed_NameUnaffectedByModulePath` passes on arrival by design).
- [ ] 3.2 GREEN: rename the root module in one step (design D1–D3): root `go.mod` module line, package clauses `do`→`doger`, `do_test`→`doger_test` (`di_explain_frame_test.go`) and `dohttp`→`dogerhttp` by direct edit, root-module import specs, drop the `do` aliases in `http/pages_test.go` and `http/utils_test.go`, and `gofmt -r 'do.a -> doger.a' -w` on `di_explain_frame_test.go` and the files of `http`, `tests`, `tests/fixtures` and `bench` that import the root; verify the 3.1 tests pass, `make test` is green, and gopls diagnostics are clean on every root-module package.
- [ ] 3.3 REFACTOR: none needed: 3.2 is itself the *Rename*; only `gofmt`/import grouping remains; verify `make test` and `make lint` pass.
- [ ] 3.4 Docs: rewrite godoc usage `do.X` → `doger.X` and "the do library/package" prose in non-test comments of the root, `http` and `stacktrace` packages (design D6); verify `grep -nE '^\s*//.*\bdo\.[A-Z]' *.go http/*.go stacktrace/*.go` prints nothing and `make lint` passes.
- [ ] 3.5 Docs: update `README.md` (fork notice, badges, `go get`/import snippets, docs and repository links, prose rule) per design D7/D8; verify every remaining `samber/do` match in `README.md` is tagged keep in `baseline-refs.txt` or is a fork notice, and `cd docs && npm ci && npm run check:go-examples` passes.

## 4. Sub-module identity (requirements "HTTP adapter modules use doger import paths and package names", "Every workspace module path mirrors its directory under the doger path")

- [ ] 4.1 RED: add `TestModuleIdentity_HTTPAdapterModules` (5 rows) and `TestModuleIdentity_WorkspaceModules` (25 rows) to `module_identity_test.go`; verify `go test -race -count=1 -run 'TestModuleIdentity_(HTTPAdapterModules|WorkspaceModules)' .` fails with the old paths and package names as actual values, and nothing else fails.
- [ ] 4.2 GREEN: rewrite the 24 sub-module `go.mod` files (module line, `replace`, `require` with `v0.0.0-00010101000000-000000000000`; design D1), the five `http/*` package clauses (design D2), and the import specs plus `do.`→`doger.` qualifiers in `http/*` and `examples/*` (design D3); verify the 4.1 tests pass, `make test` is green, and in each of the 25 modules `go mod edit -json` lists only `github.com/kartaladev/doger*` requires at that v0 placeholder and `go vet ./...` passes.
- [ ] 4.3 REFACTOR: *Rename* the import aliases together with their qualifiers per the design D2 alias table (`dohttp`, `dochi`, `doecho`, `dofiber`, `dogin`, `ginhttpstd` → `doger*`), never rewriting `gin.`; verify `go vet ./... && go test -race ./... && go build ./...` passes in every `http/*` and `examples/*` module and `make lint` passes.
- [ ] 4.4 Run `go mod tidy` in each of the 24 sub-modules; verify the diff shows only path rewrites and re-sorted `require` lines.
- [ ] 4.5 Docs: update the `go get`/import snippets and qualifiers in `docs/docs/troubleshooting/web-ui.md` to the new adapter paths and package names; verify `grep -n 'samber/do' docs/docs/troubleshooting/web-ui.md` prints nothing and `cd docs && npm run check:go-examples` passes.

## 5. Debug UI branding (requirement "Debug UI shows doger branding")

- [ ] 5.1 RED: in `http/pages_test.go`, change the `TestIndexHTML` heading assertion to "Welcome to doger UI", add its title assertion, and add `TestIndexHTML_Branding` (7 rows), `TestPagesHTML_Title` (3 rows) and `TestPagesHTML_UpstreamAttributionOnly` (4 rows); verify `go test -race -count=1 -run 'TestIndexHTML|TestPagesHTML_' ./http/` fails only on those assertions, while `TestIndexHTML_BasePathLinks` stays green.
- [ ] 5.2 GREEN: edit the titles, heading and links in `http/index.go`, `http/service.go` and `http/scope.go` per design D5; verify the 5.1 command passes and `make test` is green.
- [ ] 5.3 REFACTOR: none needed: the changes are literal template text, with no duplication worth extracting; verify `make test` and `make lint` pass.

## 6. Docs site identity and upstream accounts (docs; no Go behavior)

- [ ] 6.1 Update `docs/docusaurus.config.ts` identity per design D7 (title, interim `url`/`baseUrl`, `organizationName`/`projectName`, `og:site_name`, `editUrl`, navbar, footer and version-dropdown links, absolute `do.samber.dev` URLs); verify `grep -n 'do\.samber\.dev\|github.com/samber/do' docs/docusaurus.config.ts` shows only the upstream v1 link.
- [ ] 6.2 Remove upstream analytics, search and sponsors per design D7: Ahrefs script, `gtag`, PostHog plugin plus `npm uninstall posthog-docusaurus`, `algolia`, `customFields.sponsors`, the Sponsor, Twitter and Stack Overflow links, and `.github/FUNDING.yml`; verify `grep -rnE 'ZlVVDleFCGZPB8Nd2KkKrw|G-ZQ0MR5WG9T|phc_|hogpost|9Q5MHPPFJM|sponsors/samber|gh-smbr|posthog-docusaurus' docs/docusaurus.config.ts docs/package.json .github` prints nothing.
- [ ] 6.3 Apply the design D7 prose and snippet rules to `docs/docs/**`, `docs/src/**`, `docs/static/llms.txt`, `docs/static/robots.txt` and `docs/plugins/structured-data/index.ts`, add the docs fork notice, and add `docs/docs/migrating/migrating-from-samber-do.md` (migration steps from design "Public API & Defaults"); verify `cd docs && npm ci && npm run lint && npm run build` passes.
- [ ] 6.4 Add `Copyright (c) 2026 kartaladev` to `LICENSE` directly below the unchanged `Copyright (c) 2022 Samuel Berthe` line (design D8); verify `git diff LICENSE` is exactly one added line.

## 7. Repository metadata

- [ ] 7.1 Add the semver guard step to `.github/workflows/release.yml` (fail unless `inputs.semver` matches `^v[01]\.[0-9]+\.[0-9]+$`; design D9); verify `go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12 .github/workflows/release.yml` reports no errors.
- [ ] 7.2 Update `CLAUDE.md` line 3 and the `openspec/config.yaml` `context` to name module `github.com/kartaladev/doger` (package `doger`), "a fork of `github.com/samber/do/v2`" (design D9); verify `grep -n 'kartaladev/doger' CLAUDE.md openspec/config.yaml` matches in both files.
- [ ] 7.3 Add a MODIFIED delta `specs/service-source-tracking/spec.md` to this change (possible now, because `fix-stacktrace-frame-filter` is archived first) that copies each affected requirement whole and rewrites `do.` to `doger.` and `github.com/samber/do/v2/stacktrace` to `github.com/kartaladev/doger/stacktrace` in its scenarios, and list it under Modified Capabilities in proposal.md; verify `openspec validate rename-module-doger --strict` passes and `grep -n 'samber/do/v2\|do\.New\|do\.Provide' openspec/changes/rename-module-doger/specs/service-source-tracking/spec.md` prints nothing.

## 8. Integration checks

- [ ] 8.1 Test inventory: regenerate the per-module `go test -list '.*' ./...` and diff it against `baseline-tests.txt`; verify that the only differences are the tests added in groups 2–5.
- [ ] 8.2 Full green run: verify `make test`, `make lint`, `go test -run Example .`, and `go vet ./... && go test -race ./...` plus a clean `go mod tidy` diff in every workspace module all pass, with no goleak reports.
- [ ] 8.3 Reference sweep: `grep -rn 'samber/do' --exclude-dir=node_modules --exclude-dir=.git .`; verify every match falls into a design D11 allow-list category, `LICENSE` still contains `Samuel Berthe`, and README and the docs carry the fork notice.
- [ ] 8.4 Package names: run `go list -f '{{.ImportPath}} {{.Name}}' ./...` in every module; verify the output matches design D2 and `specs/module-identity/spec.md`.

## Workflow follow-up

- After merge: cut the first tag (`v0.1.0`) through the guarded `release.yml`, once the release-gating Open Question is resolved, then confirm `go list -m github.com/kartaladev/doger@v0.1.0` resolves through the proxy.
- Decide sub-module publishing (`http/<x>/v0.y.z` tags, real requires), the final docs domain, and docs search (design Open Questions).
- Decide what to do with the `go.dev/play` links in doc comments (docs decision).
- Consider extending `make test` to cover every workspace module (separate tooling change).
- Archive this change once group 8 is recorded, then start `upgrade-go-1-27`.
