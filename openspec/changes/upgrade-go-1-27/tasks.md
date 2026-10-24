# Tasks

Tooling and refactor change (`skip_specs: true`). Groups are behavior-preserving steps, with no RED/GREEN, guarded by the existing suite. The exceptions are the explicit test deletions in group 3 (design D2) and one characterization test in group 2. Run `make test` and `make lint` after every step. If a step turns the suite red, revert that step and investigate. Do not patch forward.

## 1. Baseline and directive bump

- [ ] 1.1 Record the baseline: the per-module test inventory (`go test -list '.*' ./...`, attributed by package directory, saved outside the repo), plus green `make test`, `make lint`, and per-module `go vet ./... && go test -race ./...`. Verify: everything passes. If it doesn't, stop and report; do not fix anything here.
- [ ] 1.2 Bump every module with `go mod edit -go=1.27` (all 25 `go.mod` files), and the workspace with `go work edit -go=1.27`. Verify: `grep -rn '^go ' --include=go.mod . && grep -n '^go ' go.work` shows only `go 1.27`, and no `toolchain` line was added.
- [ ] 1.3 Run the full suite with the bump and no other edit (design risks: per-iteration loop variables, `panicnil`, `asynctimerchan`). Verify: `make test` passes, the health-check timeout tests pass with `go test -race -count=5 -run 'HealthCheck' .`, and per-module vet and race tests pass. If anything fails, stop and report it with output.
- [ ] 1.4 Run `go mod tidy` in each module. Verify: no diff beyond the `go` line.

## 2. Characterize directive-sensitive behavior

- [ ] 2.1 Add `invoke_test.go: TestHandleProviderPanic_PanicNil`. A provider that calls `panic(nil)` makes `handleProviderPanic` return a non-nil error that matches `*runtime.PanicNilError` via `errors.As`. This is a characterization test of behavior in this repo's own builds after the bump, so it is expected to pass when added. Verify: `go test -race -count=1 -run TestHandleProviderPanic_PanicNil .` passes.

## 3. Inline exactly-equivalent helpers (design D2)

- [ ] 3.1 *Inline Function*: replace `contains(s.invokerChain, name)` in `virtual_scope.go:115` with `slices.Contains`. Verify: `make test` passes and the circular-dependency assertions pass (`go test -race -run 'TestInvokeAnyByName|TestInvokeByName$' .`, which covers `invoke_test.go:53-61`).
- [ ] 3.2 *Inline Function*: replace `reverseSlice(ancestors)` in `di_explain.go:480` with `slices.Reverse`. Verify: `go test -race -run 'Explain' .` and `make test` pass.
- [ ] 3.3 *Inline Function*: replace `coalesce(...)` in `invoke.go:234` with `cmp.Or`. Verify: `go test -race -run 'InvokeStruct' .` and `make test` pass.
- [ ] 3.4 *Remove Dead Code*: delete `contains`, `reverseSlice` and `coalesce` from `utils.go`, together with their dedicated tests in `utils_test.go`. This is the explicit test-deletion exception in design D2. Verify: `gopls` diagnostics are clean; the inventory diff shows only those helper tests removed; `make test` and `make lint` pass.

## 4. Goroutine fan-outs and `any` (design D3, D4)

- [ ] 4.1 *Substitute Algorithm* (structure only): rewrite `scope.go:403-415` (children shutdown) to `sync.WaitGroup.Go`, capturing the per-iteration loop variables instead of passing parameters. Verify: the shutdown tests pass with `go test -race -count=3 -run 'Shutdown' .`, goleak is clean, and `make test` passes.
- [ ] 4.2 Same rewrite for `scope.go:488-510` (`shutdownServicesWithoutDependenciesInParallel`). Verify: same commands as 4.1.
- [ ] 4.3 `gofmt -r 'interface{} -> any' -w` over the non-test Go files of every module, then over the test files. Verify: `grep -rn 'interface{}' --include='*.go' .` returns nothing; `make test` and `make lint` pass.
- [ ] 4.4 Run the gopls modernize analyzers and review the proposed fixes. Apply only fixes inside design D4's scope, and list any rejected ones in the PR description. Verify: `make test` and `make lint` pass.

## 5. CI, lint and docs (design D5, D6)

- [ ] 5.1 Change the `.github/workflows/test.yml` matrix to `["1.27", "stable"]`. Verify: the YAML parses (`python3 -c 'import yaml,sys; yaml.safe_load(open(sys.argv[1]))' .github/workflows/test.yml`), and `release.yml` still uses `go-version-file: 'go.work'`.
- [ ] 5.2 Add `run.go: "1.27"` to `.golangci.yml`. Verify: `make lint` reports 0 issues.
- [ ] 5.3 Update the Go version badge and the "Go 1.18+" tagline in `README.md` to 1.27, and the "Compatible with Go 1.18 or later" line in `docs/docs/getting-started.md`. Leave the lines about sibling projects (`samber/lo`, `samber/mo`) as they are, because they describe other libraries. Verify: `grep -n '1\.18' README.md docs/docs/getting-started.md` shows only those sibling-project lines.

## 6. Integration checks

- [ ] 6.1 Inventory diff against the 1.1 baseline. Verify: the only differences are the helper tests deleted in 3.4 and the test added in 2.1.
- [ ] 6.2 Full green run: `make test`, `make lint`, and per-module `go vet ./... && go test -race ./...`. Verify: everything passes, with no goleak reports.
- [ ] 6.3 Confirm the features the next changes depend on are available. Do this in a throwaway file outside the repo (not committed): a module at `go 1.27` that imports `testing/synctest` and declares one generic method must compile with `go vet`. Verify: it compiles. Delete the throwaway.

## Workflow follow-up

- Archive this change after review, then start `add-start-lifecycle-and-kinds`.
- Consider extending `make test` to cover every workspace module (separate tooling change, also noted in `rename-module-doger`).
