# release-integrity Specification

## Purpose

Defines what a consumer can rely on for every released tag of the doger modules. Every module in the workspace is built, tested, linted and scanned for vulnerabilities. Each released module resolves and builds without the repository's `go.work` and carries no test-only requirements. Releases come only from `main` through a protected environment, and their CI supply chain is pinned.

## Requirements

### Requirement: Every workspace module is tested on three operating systems
`make test-all` SHALL run `go test -race ./...` in every module that `make modules` lists, which is every `use` entry of `go.work`. It SHALL exit non-zero, naming the module, when any module fails to build or test. CI SHALL run it on Linux, macOS and Windows for every push and pull request.

#### Scenario: make modules lists every workspace module
```gherkin
Given the repository at the head of main
When the maintainer runs "make modules"
Then the output lists exactly the directories of the go.work use entries, one per line, including "<module>"

Examples:
  | module           |
  | .                |
  | ./http/chi       |
  | ./http/echo      |
  | ./http/fiber     |
  | ./http/gin       |
  | ./http/std       |
  | ./scheduler/gocron |
  | ./clock/clockwork |
  | ./examples/simple |
```

#### Scenario: A failure in any module fails make test-all
```gherkin
Given the repository at the head of main
And a test in module "<module>" that fails with "boom"
When the maintainer runs "make test-all"
Then the command exits with a non-zero status
And its output names "<module>" as the failing module
And the other modules were still tested

Examples:
  | module             |
  | .                  |
  | ./http/chi         |
  | ./scheduler/gocron |
  | ./clock/clockwork  |
```

#### Scenario: A clean tree passes make test-all
```gherkin
Given the repository at the head of main with no failing test
When the maintainer runs "make test-all"
Then the command exits with status 0
And every module listed by "make modules" was run with "-race"
```

#### Scenario: CI runs the module loop on three operating systems
```gherkin
Given the workflow ".github/workflows/test.yml"
When "scripts/check-workflows.sh" inspects it
Then its test job runs "make test-all"
And its matrix covers the operating systems "ubuntu-latest", "macos-latest" and "windows-latest"
And its matrix covers the Go versions "1.27.x" and "stable"
```

<!-- cases: N/A 2 overrides: the module list comes from go.work only; there is no consumer option -->
<!-- cases: N/A 3 zero values: go.work always lists at least the root module -->
<!-- cases: N/A 6 duplicate: go.work cannot list a directory twice -->
<!-- cases: N/A 7, 8: modules are tested independently; ordering and concurrency do not change the outcome -->
<!-- cases: N/A 9 panics: a panicking test is a failing test (covered by the failure scenario) -->
<!-- cases: N/A 10 compatibility: "make test" keeps its root-only meaning for fast local runs -->

### Requirement: Every workspace module is linted
`make lint-all` SHALL run golangci-lint with the repository's `.golangci.yml` in every module that `make modules` lists, and SHALL exit non-zero when any module reports an issue. The configuration SHALL enable `gosec`, `errorlint` and `noctx`, and every suppression SHALL carry an inline justification.

#### Scenario: An issue in any module fails make lint-all
```gherkin
Given the repository at the head of main
And module "<module>" contains <issue>
When the maintainer runs "make lint-all"
Then the command exits with a non-zero status
And its output names the file in "<module>" and the linter "<linter>"

Examples:
  | module      | issue                                              | linter   |
  | ./http/chi  | an unchecked "w.Write" call                        | errcheck |
  | ./http/std  | a "fmt.Fprint" of request data into the response   | gosec    |
  | .           | "err == ErrServiceNotFound" instead of errors.Is   | errorlint |
  | ./http/gin  | an "http.Get" call without a context               | noctx    |
```

#### Scenario: The head of main is lint-clean
```gherkin
Given the repository at the head of main
When the maintainer runs "make lint-all"
Then the command exits with status 0
And every "//nolint" directive in the repository names its linter and carries a reason after "//"
```

<!-- cases: N/A 2 overrides: the lint configuration is the repository's; consumers are not affected -->
<!-- cases: N/A 3, 4: no input values -->
<!-- cases: N/A 6, 7, 8, 9: lint runs are independent per module -->
<!-- cases: N/A 10 compatibility: "make lint" keeps its root-only meaning -->

### Requirement: No released module has a reachable known vulnerability
`make vuln` SHALL run govulncheck in every module that `make modules` lists, and SHALL exit non-zero when any module has a reachable vulnerability. CI SHALL run it on every push, pull request and weekly schedule. The release job SHALL run it on the post-processed tree before tagging and SHALL refuse to tag when it fails.

#### Scenario: A reachable vulnerability fails the gate
```gherkin
Given module "<module>" requires "<dependency>"
When the maintainer runs "make vuln"
Then the command exits with <status>
And its output <names>

Examples:
  | module       | dependency                          | status   | names                         |
  | ./http/fiber | github.com/valyala/fasthttp v1.51.0 (brotli v1.1.0) | non-zero | names "GO-2026-6888" |
  | ./http/fiber | github.com/valyala/fasthttp v1.79.0 (no brotli)     | 0        | reports no reachable vulnerability |
  | ./http/echo  | golang.org/x/net v0.61.0            | 0 unless a reachable finding is reported | lists imported-only findings without failing |
  | .            | the root module's current requirements | 0     | reports no reachable vulnerability |
```

#### Scenario: Released dependency floors
```gherkin
Given the repository at the head of main
When the maintainer reads the go.mod of "<module>"
Then it requires "<dependency>" at version "<minimum>" or later

Examples:
  | module       | dependency                   | minimum |
  | ./http/fiber | github.com/valyala/fasthttp  | v1.79.0 |
  | ./http/echo  | golang.org/x/net             | v0.62.0 |
  | ./http/gin   | golang.org/x/net             | v0.62.0 |
```

#### Scenario: An accepted vulnerability without a fix is recorded
```gherkin
Given govulncheck reports "<id>" for module "<module>" with no fixed version
And the adapter does not call the affected symbol
When the maintainer reads SECURITY.md
Then it lists "<id>" under "Accepted advisories" with the module and the reason

Examples:
  | id            | module      |
  | GO-2026-6875  | ./http/echo |
  | GO-2026-6872  | ./http/echo |
```

<!-- cases: N/A 2 overrides: the gate has no opt-out; an advisory without a fix is recorded instead (third scenario) -->
<!-- cases: N/A 3 zero values: a module with no dependencies passes trivially (root row) -->
<!-- cases: N/A 6, 7, 8, 9: scans are independent per module -->
<!-- cases: N/A 10 compatibility: dependency bumps are patch or minor updates of adapter-only requirements -->

### Requirement: Released modules resolve without the workspace
For every released module, a clean consumer module with no `go.work`, `GOWORK=off` and only a module proxy holding the release SHALL be able to `go get` that module at the release version and build a program importing it. Each released adapter SHALL require the root module at the same release version and SHALL contain no `replace` directive.

#### Scenario: A clean consumer builds every released module
```gherkin
Given "scripts/release-dry-run.sh v0.99.0" has post-processed a copy of the repository as the release job does and published it to a local file proxy
And a new consumer module with GOWORK=off, GOFLAGS=-mod=mod and GOPROXY set to that proxy only
When the consumer runs "go get <module>@<version>" and builds a program importing "<package>"
Then both commands exit with status 0

Examples:
  | module                                         | version | package                                        |
  | github.com/kartaladev/doger                    | v0.99.0 | github.com/kartaladev/doger                    |
  | github.com/kartaladev/doger/http/std           | v0.99.0 | github.com/kartaladev/doger/http/std           |
  | github.com/kartaladev/doger/http/gin           | v0.99.0 | github.com/kartaladev/doger/http/gin           |
  | github.com/kartaladev/doger/http/echo          | v0.99.0 | github.com/kartaladev/doger/http/echo          |
  | github.com/kartaladev/doger/http/fiber         | v0.99.0 | github.com/kartaladev/doger/http/fiber         |
  | github.com/kartaladev/doger/http/chi           | v0.99.0 | github.com/kartaladev/doger/http/chi           |
  | github.com/kartaladev/doger/scheduler/gocron   | v0.99.0 | github.com/kartaladev/doger/scheduler/gocron   |
  | github.com/kartaladev/doger/clock/clockwork    | v0.99.0 | github.com/kartaladev/doger/clock/clockwork    |
```

#### Scenario: Released adapters pin the root release
```gherkin
Given "scripts/release-dry-run.sh v0.99.0" has post-processed a copy of the repository
When the dry run inspects the go.mod of "<module>"
Then it requires "github.com/kartaladev/doger v0.99.0"
And it contains no replace directive

Examples:
  | module             |
  | http/std           |
  | http/gin           |
  | http/echo          |
  | http/fiber         |
  | http/chi           |
  | scheduler/gocron   |
  | clock/clockwork    |
```

#### Scenario: The dry run rejects an invalid version
```gherkin
Given the repository at the head of main
When the maintainer runs "scripts/release-dry-run.sh <version>"
Then the script exits with <status>

Examples:
  | version       | status   |
  | v0.99.0       | 0        |
  | v1.0.0-rc.1   | 0        |
  | v2.0.0        | non-zero |
  | 0.1.0         | non-zero |
  | ""            | non-zero |
```

<!-- cases: N/A 2 overrides: release post-processing has no consumer option -->
<!-- cases: N/A 6 duplicate tag: tagging an existing version is refused by git in the release job (provenance requirement) -->
<!-- cases: N/A 7, 8, 9: the dry run is sequential and side-effect free (it works on a copy and a private module cache) -->
<!-- cases: N/A 10 compatibility: the root module path and the adapter module paths are unchanged (module-identity) -->

### Requirement: Released go.mod files carry no test-only requirements
After the release post-processing, the go.mod of each released module SHALL require only what its non-test packages import, directly or indirectly. In particular, the root module SHALL NOT require `github.com/stretchr/testify`, `go.uber.org/goleak` or `github.com/jonboulle/clockwork`, and no adapter SHALL require testify or goleak.

#### Scenario: Test-only requirements are stripped
```gherkin
Given "scripts/release-dry-run.sh v0.99.0" has post-processed a copy of the repository
When the dry run reads the go.mod of "<module>"
Then it does not require "<dependency>"

Examples:
  | module           | dependency                      |
  | .                | github.com/stretchr/testify     |
  | .                | go.uber.org/goleak              |
  | .                | github.com/jonboulle/clockwork  |
  | scheduler/gocron | github.com/stretchr/testify     |
  | scheduler/gocron | go.uber.org/goleak              |
  | http/std         | github.com/stretchr/testify     |
  | clock/clockwork  | github.com/stretchr/testify     |
```

#### Scenario: Runtime requirements are kept
```gherkin
Given "scripts/release-dry-run.sh v0.99.0" has post-processed a copy of the repository
When the dry run reads the go.mod of "<module>"
Then it requires "<dependency>"

Examples:
  | module           | dependency                        |
  | .                | github.com/samber/go-type-to-string |
  | scheduler/gocron | github.com/go-co-op/gocron/v2     |
  | clock/clockwork  | github.com/jonboulle/clockwork    |
```

<!-- cases: N/A 2 overrides: none -->
<!-- cases: N/A 3, 4: no input values -->
<!-- cases: N/A 6, 7, 8, 9: post-processing is a deterministic transformation of a copy -->
<!-- cases: N/A 10 compatibility: consumers previously received testify and goleak through scheduler/gocron; removing them shrinks their build graph without changing any API -->

### Requirement: Releases are cut only from main through a protected environment
The release workflow SHALL refuse to run unless the dispatched ref is `refs/heads/main`. Its job SHALL run in the `release` environment. After test stripping and tidying it SHALL build, vet and run `make vuln` on the post-processed tree before tagging. It SHALL create the GitHub release with generated release notes.

#### Scenario: The release workflow enforces provenance
```gherkin
Given the workflow ".github/workflows/release.yml"
When "scripts/check-workflows.sh" inspects it
Then the release job has the condition "github.ref == 'refs/heads/main'"
And the release job declares "environment: release"
And a step runs "go mod tidy" in every released module before the adapters are pinned
And a step runs "go build ./..." and "go vet ./..." in every released module after pinning
And a step runs "make vuln" before the "Create tags" step
And the release action is called with "generate_release_notes: true"
```

#### Scenario: A dispatch from another branch is refused
```gherkin
Given the release workflow dispatched with semver "v0.99.0" on ref "<ref>"
When the release job is evaluated
Then the job is <outcome>

Examples:
  | ref                     | outcome |
  | refs/heads/main         | run     |
  | refs/heads/feature-x    | skipped |
  | refs/tags/v0.98.0       | skipped |
```

<!-- cases: N/A 2 overrides: environment reviewers are configured by maintainers in repository settings, outside the code -->
<!-- cases: N/A 3, 4: semver validation is covered by "The dry run rejects an invalid version" and the existing "Check semver" step -->
<!-- cases: N/A 7, 8, 9: a single release job runs sequentially -->
<!-- cases: N/A 10 compatibility: tag names (root <semver>, adapters <dir>/<semver>) are unchanged; clock/clockwork gets its own tag in the same form -->

### Requirement: CI actions are pinned and least-privileged
Every `uses:` reference in `.github/workflows/*` SHALL name a full 40-character commit SHA followed by a version comment. Every workflow SHALL declare top-level `permissions: contents: read`, and SHALL raise a permission only on the job that needs it.

#### Scenario: Workflow pinning and permissions are checked
```gherkin
Given the workflow "<workflow>"
When "scripts/check-workflows.sh" inspects it
Then every "uses:" line matches "@[0-9a-f]{40} # v"
And the top-level permissions are exactly "contents: read"
And only the jobs <raised jobs> raise a permission

Examples:
  | workflow                        | raised jobs                                   |
  | test.yml                        | none                                          |
  | lint.yml                        | none                                          |
  | doc.yml                         | the deploy job (pages: write, id-token: write) |
  | security.yml                    | the codeql and trivy-codeql jobs (security-events: write, to upload to code scanning) |
  | release.yml                     | the release job (contents: write)             |
  | dependabot-automerge.yaml       | the automerge job (contents: write, pull-requests: write) |
```

#### Scenario: A tag-pinned action fails the check
```gherkin
Given a workflow step "uses: actions/checkout@v7"
When "scripts/check-workflows.sh" runs
Then it exits with a non-zero status naming the file and the line
```

<!-- cases: N/A 2 overrides: none -->
<!-- cases: N/A 3 zero values: a workflow without steps has no uses lines and still needs top-level permissions (rows above) -->
<!-- cases: N/A 6, 7, 8, 9: static file checks -->
<!-- cases: N/A 10 compatibility: workflow triggers and job names are unchanged -->

### Requirement: Dependency updates are merged automatically only for minor and patch versions
Dependabot SHALL cover every module directory, including `/scheduler/**` and `/clock/**`. The auto-merge workflow SHALL merge a pull request only when its author is `dependabot[bot]` and the update type is a semver patch or minor update; major updates SHALL wait for review.

#### Scenario: Auto-merge decision
```gherkin
Given a pull request opened by "<author>" whose Dependabot update type is "<update type>"
When the auto-merge workflow evaluates it
Then auto-merge is <outcome>

Examples:
  | author            | update type                 | outcome  |
  | dependabot[bot]   | version-update:semver-patch | enabled  |
  | dependabot[bot]   | version-update:semver-minor | enabled  |
  | dependabot[bot]   | version-update:semver-major | not enabled |
  | octocat           | version-update:semver-patch | not enabled |
```

#### Scenario: Every module directory is covered
```gherkin
Given ".github/dependabot.yml"
When "scripts/check-workflows.sh" compares its gomod directories with "make modules"
Then every listed module directory is matched by a gomod entry, including "/scheduler/gocron" and "/clock/clockwork"
```

<!-- cases: N/A 3, 4: no input values beyond the table -->
<!-- cases: N/A 7, 8, 9: workflow evaluation is per pull request -->
<!-- cases: N/A 10 compatibility: patch and minor updates keep merging automatically as before -->

### Requirement: The repository ships its policy files
The repository SHALL contain `SECURITY.md` with a private reporting channel and the accepted-advisories list, `CHANGELOG.md` with a section per release that lists every BREAKING change with its migration, and `.github/CODEOWNERS`. It SHALL NOT contain `vercel.json`.

#### Scenario: Policy files are present
```gherkin
Given the repository at the head of main
When "scripts/check-workflows.sh" checks the policy files
Then "<path>" <state>

Examples:
  | path                 | state                                                                        |
  | SECURITY.md          | exists and contains "Reporting a vulnerability" and "Accepted advisories"     |
  | CHANGELOG.md         | exists, has an "Unreleased" section, and every section listing "BREAKING" entries gives their migrations |
  | .github/CODEOWNERS   | exists and assigns "*" to at least one owner                                 |
  | vercel.json          | does not exist                                                               |
```

<!-- cases: N/A 2-9: static file presence checks -->
<!-- cases: N/A 10 compatibility: documentation deployment uses GitHub Pages (doc.yml), unaffected by removing vercel.json -->
