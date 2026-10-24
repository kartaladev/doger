# CLAUDE.md

Go library: fork of `github.com/samber/do/v2` (generics-based dependency injection). The public API is the product.

## Commands

- Test: `make test` (`go test -race ./...`; `TestMain` runs goleak, so leaked goroutines fail the suite)
- Single test: `go test -race -run TestName ./<pkg>`
- Lint: `make lint` (golangci-lint, config in `.golangci.yml`)
- Workspace: `go.work` includes `./http/*` and `./examples/*` modules

## Project rules (mandatory)

1. **Go skills.** For any Go work, in planning or implementation, use the `cc-skills-golang` skills (samber). Load `golang-how-to` first and let it pick the specific ones. Typical picks: `golang-testing`, `golang-stretchr-testify`, `golang-error-handling`, `golang-design-patterns`, `golang-concurrency`, `golang-refactoring`.
2. **Navigation via gopls.** Use the `LSP` tool / `golang-gopls` skill for definitions, references, implementations, call hierarchy, rename, and post-edit diagnostics. Don't use grep for symbol navigation.
3. **Strict TDD: red → green → refactor.**
   - RED: write the test first and run it. It must fail for the expected reason, not because of a typo or an unrelated compile error.
   - GREEN: write the simplest production code that passes.
   - REFACTOR: change structure only, never behavior. Work in small named steps from Fowler's catalog (Extract Function, Inline Function, Rename, Move Function, Replace Conditional with Polymorphism, Introduce Parameter Object…). Run tests after each step. If a test has to change, it isn't a refactor.
4. **Hot-path coverage.** There is no coverage-percentage target, but every hot path must be covered by tests. That means the public API, resolution/invocation, lifecycle (health check, shutdown), scope traversal, and concurrency-sensitive code.
5. **Defects need proof.** Never claim a bug, error, or defect without a failing test that reproduces it. A fix starts with that failing regression test. If you can't reproduce it, say so instead of asserting it.
6. **Library principle.** Use sensible, opinionated defaults that work with zero configuration, and give consumers full control: every opinionated choice can be overridden (options, interfaces, hooks) without forking. Avoid breaking exported APIs; flag any break explicitly.
7. **Specs.** Plan changes with OpenSpec (`/opsx:*` skills). The project schema `openspec/schemas/doger-spec-driven` requires Gherkin scenarios with concrete `Examples`, an exhaustive case checklist (defaults, overrides, zero values, boundaries, errors, lifecycle, concurrency, panics, compatibility), and RED/GREEN/REFACTOR task groups. Project context and rules are in `openspec/config.yaml`.

## Test conventions

- testify `assert`/`require`; table-driven tests with named subtests
- Concurrency tests run under `-race`; use `testWithTimeout` (`main_test.go`) for tests that could hang
- `Example*` tests document public API defaults and overrides
