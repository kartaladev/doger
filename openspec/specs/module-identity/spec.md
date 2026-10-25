# module-identity Specification

## Purpose
Defines the identity consumers see for this library: the module and package import paths, the package names, the service names the container infers from library types, and the debug UI branding, plus the identifiers that deliberately stay the same for compatibility.

## Requirements

### Requirement: Root module packages use the doger import path and package names
The root module SHALL have the module path `github.com/kartaladev/doger`, with no major-version suffix. Its root package SHALL be named `doger`, and every other package of the root module SHALL be importable under that path with the package name listed for it.

#### Scenario: Root module packages have doger import paths and names
```gherkin
Given the root module of the repository
When the package in directory "<dir>" is loaded
Then its import path is "<import_path>"
And its package name is "<name>"

Examples:
  | dir        | import_path                            | name       |
  | .          | github.com/kartaladev/doger            | doger      |
  | http       | github.com/kartaladev/doger/http       | dogerhttp  |
  | stacktrace | github.com/kartaladev/doger/stacktrace | stacktrace |
```

<!-- cases: N/A 2 overrides: a module path is fixed by the module; consumers can only alias the import (`do "github.com/kartaladev/doger"`), which is Go language behavior, not library behavior -->
<!-- cases: N/A 3 zero values: there is no input to a module path -->
<!-- cases: N/A 4 boundaries: the root module has exactly the three importable non-test package directories listed -->
<!-- cases: N/A 5 invalid input: importing the old path `github.com/samber/do/v2` resolves upstream's module, not this one; that is Go tooling behavior -->
<!-- cases: N/A 6 not found/duplicate: one module path per module -->
<!-- cases: N/A 7 lifecycle: static property of the build -->
<!-- cases: N/A 8 concurrency: static property of the build -->
<!-- cases: N/A 9 panics: nothing executes -->
<!-- cases: N/A 10 backward compatibility: deliberately BREAKING; migration is in design.md "Public API & Defaults" -->

### Requirement: HTTP adapter modules use doger import paths and package names
Each HTTP adapter module SHALL have the module path `github.com/kartaladev/doger/http/<framework>`, nested under the root path with no major-version suffix. Its package name SHALL carry the `doger` prefix so that it does not clash with the framework it adapts.

#### Scenario: HTTP adapter modules have doger import paths and names
```gherkin
Given the HTTP adapter module in directory "<dir>"
When its module and package are loaded
Then its module path is "<module_path>"
And its package name is "<name>"

Examples:
  | dir        | module_path                            | name         |
  | http/chi   | github.com/kartaladev/doger/http/chi   | dogerchi     |
  | http/echo  | github.com/kartaladev/doger/http/echo  | dogerecho    |
  | http/fiber | github.com/kartaladev/doger/http/fiber | dogerfiber   |
  | http/gin   | github.com/kartaladev/doger/http/gin   | dogergin     |
  | http/std   | github.com/kartaladev/doger/http/std   | dogerhttpstd |
```

<!-- cases: N/A 2 overrides: module paths are fixed; consumers may alias the import -->
<!-- cases: N/A 3 zero values: no input -->
<!-- cases: N/A 4 boundaries: exactly the five adapters listed -->
<!-- cases: N/A 5 invalid input: no input -->
<!-- cases: N/A 6 not found/duplicate: one module per adapter directory -->
<!-- cases: N/A 7 lifecycle: static property of the build -->
<!-- cases: N/A 8 concurrency: static property of the build -->
<!-- cases: N/A 9 panics: nothing executes -->
<!-- cases: N/A 10 backward compatibility: deliberately BREAKING (old paths `github.com/samber/do/http/<x>/v2`); migration in design.md -->

### Requirement: Every workspace module path mirrors its directory under the doger path
Every module listed in the workspace SHALL have the module path `github.com/kartaladev/doger` followed by its directory relative to the repository root, with no major-version suffix anywhere.

#### Scenario: Workspace module paths mirror their directories
```gherkin
Given the workspace file of the repository
When the module in workspace directory "<dir>" is loaded
Then its module path is "<module_path>"
And its module path does not end with "/v2"

Examples:
  | dir                           | module_path                                               |
  | .                             | github.com/kartaladev/doger                               |
  | examples/dag                  | github.com/kartaladev/doger/examples/dag                  |
  | examples/eager-loading        | github.com/kartaladev/doger/examples/eager-loading        |
  | examples/event-driven         | github.com/kartaladev/doger/examples/event-driven         |
  | examples/healthcheckable      | github.com/kartaladev/doger/examples/healthcheckable      |
  | examples/http/chi             | github.com/kartaladev/doger/examples/http/chi             |
  | examples/http/echo            | github.com/kartaladev/doger/examples/http/echo            |
  | examples/http/fiber           | github.com/kartaladev/doger/examples/http/fiber           |
  | examples/http/gin             | github.com/kartaladev/doger/examples/http/gin             |
  | examples/http/std             | github.com/kartaladev/doger/examples/http/std             |
  | examples/interface            | github.com/kartaladev/doger/examples/interface            |
  | examples/nested-scope         | github.com/kartaladev/doger/examples/nested-scope         |
  | examples/package-system       | github.com/kartaladev/doger/examples/package-system       |
  | examples/service-aliases      | github.com/kartaladev/doger/examples/service-aliases      |
  | examples/service-explanation  | github.com/kartaladev/doger/examples/service-explanation  |
  | examples/shutdownable         | github.com/kartaladev/doger/examples/shutdownable         |
  | examples/simple               | github.com/kartaladev/doger/examples/simple               |
  | examples/struct               | github.com/kartaladev/doger/examples/struct               |
  | examples/transient-services   | github.com/kartaladev/doger/examples/transient-services   |
  | examples/web-application      | github.com/kartaladev/doger/examples/web-application      |
  | http/chi                      | github.com/kartaladev/doger/http/chi                      |
  | http/echo                     | github.com/kartaladev/doger/http/echo                     |
  | http/fiber                    | github.com/kartaladev/doger/http/fiber                    |
  | http/gin                      | github.com/kartaladev/doger/http/gin                      |
  | http/std                      | github.com/kartaladev/doger/http/std                      |
```

<!-- cases: N/A 2 overrides: no option -->
<!-- cases: N/A 3 zero values: an empty workspace is not a supported layout -->
<!-- cases: 4 boundaries: covered by the 25 rows, which are every workspace entry; the test fails if the workspace lists a directory that has no row -->
<!-- cases: N/A 5 invalid input: no input -->
<!-- cases: N/A 6 duplicate: Go tooling rejects duplicate module paths in a workspace -->
<!-- cases: N/A 7 lifecycle: static property of the build -->
<!-- cases: N/A 8 concurrency: static property of the build -->
<!-- cases: N/A 9 panics: nothing executes -->
<!-- cases: N/A 10 backward compatibility: example modules are not consumed; the examples/http/gin outlier path is intentionally normalized -->

### Requirement: Inferred service names of library types use the doger import path
When the container infers a service name from a type, a type declared in the library SHALL be named with the import path `github.com/kartaladev/doger`. Types declared outside the library, and explicitly named services, SHALL keep the names they had before the rename.

#### Scenario: Inferred names of library and non-library types
```gherkin
Given the type "<type>"
When its service name is inferred
Then the name is "<name>"

Examples:
  | type                        | name                                              |
  | *doger.RootScope            | *github.com/kartaladev/doger.RootScope            |
  | doger.Injector              | github.com/kartaladev/doger.Injector              |
  | doger.Healthchecker         | github.com/kartaladev/doger.Healthchecker         |
  | []*doger.RootScope          | []*github.com/kartaladev/doger.RootScope          |
  | map[string]doger.Injector   | map[string]github.com/kartaladev/doger.Injector   |
  | int                         | int                                               |
  | *bytes.Buffer               | *bytes.Buffer                                     |
```

#### Scenario: Explicitly named services keep their names
```gherkin
Given a root scope
And the value 42 registered under the explicit name "db"
When the consumer lists the provided services
Then the list contains exactly one service named "db"
And invoking "db" as int returns 42
```

<!-- cases: N/A 2 overrides: inferred names have no option; explicit names (second scenario) are the override -->
<!-- cases: N/A 3 zero values: the empty name is not inferred from any type -->
<!-- cases: 4 boundaries: pointer, slice and map wrappers of library types are rows of the first scenario -->
<!-- cases: N/A 5 invalid input: name inference accepts any type -->
<!-- cases: 6 not found: covered by "Service-not-found errors name library types with the doger import path" -->
<!-- cases: N/A 7 lifecycle: name inference is stateless -->
<!-- cases: N/A 8 concurrency: name inference is a pure function of the type -->
<!-- cases: N/A 9 panics: name inference does not panic for any type -->
<!-- cases: 10 backward compatibility: the int, *bytes.Buffer and explicit-name rows pin that non-library names are unchanged; library-type names are deliberately BREAKING -->

### Requirement: Service-not-found errors name library types with the doger import path
When a service of a library type cannot be found, the returned error SHALL match `ErrServiceNotFound` with `errors.Is`, SHALL keep the `DI: ` message prefix, and SHALL name the type with the import path `github.com/kartaladev/doger`.

#### Scenario: Not-found errors for library types
```gherkin
Given a root scope where "<registered>" is registered
When the consumer invokes the type "<requested>"
Then the error matches ErrServiceNotFound
And the error message is "<message>"

Examples:
  | registered     | requested           | message                                                                                                   |
  | nothing        | *doger.RootScope    | DI: could not find service `*github.com/kartaladev/doger.RootScope`, no service available                  |
  | the int value 42 | doger.Healthchecker | DI: could not find service `github.com/kartaladev/doger.Healthchecker`, available services: `int`        |
```

<!-- cases: N/A 2 overrides: error text has no option -->
<!-- cases: 3 zero values: the "nothing registered" row is the empty scope -->
<!-- cases: 4 boundaries: zero and one available service are the two rows -->
<!-- cases: 5 errors: both rows assert errors.Is(err, ErrServiceNotFound) -->
<!-- cases: N/A 6 duplicate/override: unrelated to naming -->
<!-- cases: N/A 7 lifecycle: unrelated to naming -->
<!-- cases: N/A 8 concurrency: unrelated to naming -->
<!-- cases: N/A 9 panics: invoking a missing service returns an error, it does not panic -->
<!-- cases: 10 backward compatibility: the `DI: ` prefix and the sentence shape are unchanged; only the library type path changes -->

### Requirement: Debug UI shows doger branding
The debug UI pages SHALL name the library `doger` in their titles and headings, and the index page SHALL link to this repository's resources. The only remaining mention of `samber/do` SHALL be a single attribution link to the upstream project on the index page.

#### Scenario: Index page links point to doger resources
```gherkin
Given the debug UI base path "/debug/do"
When the index page is rendered
Then it contains a link labelled "<label>" to "<href>"

Examples:
  | label            | href                                                   |
  | Repository       | https://github.com/kartaladev/doger                    |
  | New issue        | https://github.com/kartaladev/doger/issues             |
  | Documentation    | https://kartaladev.github.io/doger/                    |
  | Godoc            | https://pkg.go.dev/github.com/kartaladev/doger         |
  | Changelog        | https://github.com/kartaladev/doger/releases           |
  | License          | https://github.com/kartaladev/doger/blob/main/LICENSE  |
  | Upstream project | https://github.com/samber/do                           |
```

#### Scenario: Index page names the library
```gherkin
Given the debug UI base path "/debug/do"
When the index page is rendered
Then its title is "Dependency injection UI - doger"
And it contains the heading "Welcome to doger UI"
```

#### Scenario: Inspection page titles name the library
```gherkin
Given a root scope with the value "db-value" registered as "db-service"
And the debug UI base path "/debug/do"
When the "<page>" page is rendered
Then its title is "<title>"

Examples:
  | page           | title                      |
  | scope tree     | Inspect scope tree - doger |
  | service list   | Inspect service - doger    |
  | service detail | Inspect service - doger    |
```

#### Scenario: Upstream branding appears only as attribution
```gherkin
Given a root scope with the value "db-value" registered as "db-service"
And the debug UI base path "/debug/do"
When the "<page>" page is rendered
Then the text "samber/do" occurs <count> times

Examples:
  | page           | count |
  | index          | 1     |
  | scope tree     | 0     |
  | service list   | 0     |
  | service detail | 0     |
```

<!-- cases: 2 overrides: the consumer's base path is the only option; covered by "Debug UI links stay relative to the consumer's base path" -->
<!-- cases: N/A 3 zero values: an empty base path is covered by the existing page tests and does not affect branding -->
<!-- cases: N/A 4 boundaries: branding is constant text -->
<!-- cases: N/A 5 invalid input: unknown scope or service IDs are existing error paths unrelated to branding -->
<!-- cases: N/A 6 not found: as above -->
<!-- cases: N/A 7 lifecycle: rendering is stateless -->
<!-- cases: N/A 8 concurrency: rendering is stateless -->
<!-- cases: N/A 9 panics: rendering does not panic -->
<!-- cases: 10 backward compatibility: deliberately changed (titles, heading, links); routes stay unchanged (see the compatibility requirement) -->

### Requirement: Compatibility identifiers stay unchanged across the rename
The rename SHALL NOT change the default struct-injection tag key `do`, the `DI: ` prefix of the exported error sentinels, or the debug UI routes, which stay relative to the base path the consumer passes.

#### Scenario: Struct injection tag key keeps its default and override
```gherkin
Given a root scope created with tag key option "<option>"
And the value 7 registered as type int
When the consumer invokes a struct whose int field is tagged "<tag>"
Then the field value is <value>

Examples:
  | option  | tag         | value |
  | (unset) | do:""       | 7     |
  | inject  | inject:""   | 7     |
  | inject  | do:""       | 0     |
```

#### Scenario: Error sentinels keep their messages
```gherkin
Given the exported error sentinel "<sentinel>"
When its message is read
Then the message is "<message>"

Examples:
  | sentinel              | message                                          |
  | ErrServiceNotFound    | DI: could not find service                       |
  | ErrServiceNotMatch    | DI: could not find service satisfying interface  |
  | ErrCircularDependency | DI: circular dependency detected                 |
  | ErrHealthCheckTimeout | DI: health check timeout                         |
```

#### Scenario: Debug UI links stay relative to the consumer's base path
```gherkin
Given the debug UI base path "<base>"
When the index page is rendered
Then it links to "<base>/scope" and "<base>/service"

Examples:
  | base      |
  | /debug/do |
  | /admin/di |
```

<!-- cases: 1 default: the "(unset)" row is the zero-configuration default -->
<!-- cases: 2 overrides: the "inject" rows; the base path rows -->
<!-- cases: N/A 3 zero values: an empty tag key option means the default, already the "(unset)" row -->
<!-- cases: N/A 4 boundaries: no numeric limits -->
<!-- cases: 5 errors: the sentinel rows pin the exact messages consumers may match on -->
<!-- cases: N/A 6 not found: a missing injected service is an existing InvokeStruct error path, unchanged by the rename -->
<!-- cases: N/A 7 lifecycle: unrelated -->
<!-- cases: N/A 8 concurrency: unrelated -->
<!-- cases: N/A 9 panics: unrelated -->
<!-- cases: 10 backward compatibility: this requirement is the compatibility guard; its tests are added before the rename and must stay green through it -->
