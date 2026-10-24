# Tasks

This change is a pure mechanical refactor (`skip_specs: true`): no behavior changes, so there are no RED/GREEN groups. Each group is a behavior-preserving refactoring step (Fowler: *Rename*, *Move Function* across module paths). The existing suite is the safety net. Every group ends with that suite passing unchanged. A test that would need a non-mechanical edit means the step is not a refactor: stop and investigate.

## 1. Baseline

- [ ] 1.1 Record the test inventory per workspace module: for each `dir` in `go list -m -f '{{.Dir}}'`, run `go test -list '.*' ./...`, attribute each name to its package **directory** (stable across the import-path rename), and save the sorted output outside the repository (e.g. the session scratchpad) as `baseline-tests.txt`. Verify: the file is non-empty for the root module's packages (`.`, `bench`, `http`, `stacktrace`, `tests`). Note: the `http/*` and `examples/*` modules have no `_test.go` files, so their baseline is build + vet (task 1.2).
- [ ] 1.2 Record the green baseline (requires `fix-stacktrace-frame-filter` to be applied first; before it, 6 stacktrace-related tests fail when the checkout directory is not named `do`): `make test` and `make lint` pass, and `go vet ./... && go test -race ./...` passes in every workspace module. Verify: show the passing output. If anything fails, stop and report it before renaming anything; do not fix it here.
- [ ] 1.3 Record the reference baseline: `grep -rn 'samber/do' --exclude-dir=node_modules --exclude-dir=.git .` saved as `baseline-refs.txt`, with each upstream-history reference to keep marked (issue links, v1 tree, releases, LICENSE copyright, `go.dev/play` links). Verify: the allow-list exists and is the input for task 8.3.

## 2. Module paths and wiring (go.mod / go.work)

- [ ] 2.1 Rename the root module: `go mod edit -module github.com/kartaladev/doger` in the root `go.mod`. Verify: `go list -m` in the root prints `github.com/kartaladev/doger`. Builds are expected to fail until group 3; that is expected mid-step.
- [ ] 2.2 Rename the `http/{chi,echo,fiber,gin,std}` modules to `github.com/kartaladev/doger/http/<x>` with `go mod edit -module`. Rewrite their `require`/`replace` entries from `github.com/samber/do/v2` to `github.com/kartaladev/doger => ../../`. Verify: `go mod edit -json` for each module shows only new paths.
- [ ] 2.3 Rename every `examples/*` module, including the `examples/http/gin` outlier, to `github.com/kartaladev/doger/examples/<dir>`. Rewrite their `require`/`replace` entries for the root and for `http/chi` (`examples/web-application`). Verify: `grep -rn 'samber/do' --include=go.mod .` returns nothing.
- [ ] 2.4 Confirm `go.work` needs no change: directories are unchanged, so the `use` entries stay. Verify: `go work edit -json` lists the same 25 directories as before.

## 3. Root package `do` → `doger`

- [ ] 3.1 Rename the package clause in every root-package file, including `_test.go` files, from `package do` to `package doger`. Use gopls package rename where it applies, otherwise a direct clause edit. Verify: `go list -f '{{.Name}}' .` prints `doger`.
- [ ] 3.2 Rewrite root-internal imports of `github.com/samber/do/v2/stacktrace` to `github.com/kartaladev/doger/stacktrace` (import specs only). Verify: `go build ./...` and `go vet ./...` pass in the root module, and gopls diagnostics are clean on the root package.
- [ ] 3.3 Rewrite imports in `stacktrace`, `tests`, `tests/fixtures` and `bench` to the new paths. Drop the explicit `do` import aliases in the 2 files that have them. Run `gofmt -r 'do.a -> doger.a' -w` on those packages. Verify: `make test` passes, and `go test -list '.*' ./...` for the root module matches `baseline-tests.txt`.
- [ ] 3.4 Run `make lint`. Verify: passes with no new findings.

## 4. HTTP sub-packages

- [ ] 4.1 Rename `http` from `dohttp` to `dogerhttp`: the package clause, plus importer qualifiers via `gofmt -r 'dohttp.a -> dogerhttp.a'`. Rewrite its imports to the new root path. Verify: `go build ./http/...` from the root passes.
- [ ] 4.2 Rename `http/chi` (`dochi`→`dogerchi`), `http/echo` (`doecho`→`dogerecho`), `http/fiber` (`dofiber`→`dogerfiber`), `http/gin` (`gin`→`dogergin`) and `http/std` (`dohttpstd`→`dogerhttpstd`). For each: package clause, import paths, then `gofmt -r` qualifier rewrites in its own tests and in any importer. Verify: in each module, `go vet ./... && go test -race ./...` passes and the test list matches the baseline.
- [ ] 4.3 Run `go mod tidy` in each `http/*` module. Verify: no diff beyond the path rewrites from group 2.

## 5. Examples

- [ ] 5.1 Rewrite imports in every `examples/*` module to the new paths. Apply `gofmt -r 'do.a -> doger.a'` and the matching `dochi`/`doecho`/`dofiber`/`gin`/`dohttpstd` → `doger*` rewrites. Verify: `go vet ./... && go build ./...` passes in every examples module.
- [ ] 5.2 Run any example tests: `go test -race ./...` in each examples module. Verify: passes and the test list matches the baseline.
- [ ] 5.3 Run `go mod tidy` in each examples module. Verify: no diff beyond the path rewrites.

## 6. Docs, README, LICENSE

Attribution rule for this group: never remove credit to the original/upstream owner (Samuel Berthe, `samber/do`) or to third-party sources. Only module-identity references change.

- [ ] 6.1 Add a fork notice near the top of `README.md` ("doger is a fork of [samber/do](https://github.com/samber/do) by Samuel Berthe"). Then update `README.md`: module path, `go get` / import snippets, package qualifier (`doger.`), and pkg.go.dev / repository badges and links. Keep upstream-history links. Verify: every remaining `samber/do` match in `README.md` is on the allow-list from 1.3.
- [ ] 6.2 Update `docs/docusaurus.config.ts`: title, `editUrl` (`.../tree/main/docs/`), pkg.go.dev and repository links. Keep the v1-tree and upstream-history links. Update the 26 docs pages under `docs/docs` and `docs/src` for import paths and qualifiers in code snippets. Verify: the docs build (`npm run build` in `docs/`) succeeds, and the remaining `samber/do` matches are allow-listed.
- [ ] 6.3 Add `Copyright (c) 2026 kartaladev` to `LICENSE` directly below the unchanged `Copyright (c) 2022 Samuel Berthe` line. The MIT text is untouched. Verify: `head -5 LICENSE` shows both lines, and `git diff LICENSE` is a one-line addition.

## 7. Example tests document the new API

- [ ] 7.1 Check that `Example*` tests in the root and `http` packages compile and pass under the new package name: `go test -run Example ./...` in the root and `http/*` modules. Verify: passes, with the same Example list as the baseline.

## 8. Integration checks

- [ ] 8.1 Test inventory: regenerate the per-module `go test -list '.*' ./...` output and diff it against `baseline-tests.txt`. Verify: the only differences are module and package qualifiers in package headers; every test name is present.
- [ ] 8.2 Full green run: `make test`, `make lint`, and `go vet ./... && go test -race ./...` in every workspace module. Verify: all pass, and goleak reports no leaks.
- [ ] 8.3 Reference sweep: `grep -rn 'samber/do' --exclude-dir=node_modules --exclude-dir=.git .`. Verify: every match is on the allow-list from 1.3, or is one of the fork-attribution notices added in 6.1 and 6.2. The sweep must also confirm the attribution survived: `LICENSE` still contains `Samuel Berthe`, and `README.md` contains the fork notice. `github.com/samber/go-type-to-string` is a separate dependency and stays.
- [ ] 8.4 Package names: `go list -f '{{.ImportPath}} {{.Name}}' ./...` in each module. Verify: the output matches design.md D2 exactly.

## Workflow follow-up

- Decide the fork's CI ownership gating (`release.yml` `triggering_actor == 'samber'`); see design.md Open Questions.
- Consider extending `make test` to cover every workspace module (separate tooling change).
- Decide what to do with `go.dev/play` links in doc comments, which point at upstream code (docs decision).
- Archive this change once `make test` passes and the checks in group 8 are recorded, then start `upgrade-go-1-27`.
