# Spec Delta

## Purpose
Lets the container record and report where a service was provided and invoked, by picking the right stack frame consistently in every checkout, install layout and build mode.

## MODIFIED Requirements

### Requirement: Caller frame skips the stacktrace package's own frames
When asked for the caller frame, the library SHALL skip frames whose function belongs to its own `stacktrace` package, except frames from `_test.go` files, and frames whose file is under GOROOT. It SHALL report the first remaining frame, including frames from other packages of the library, with that frame's file, function name and line.

#### Scenario: Caller frame from a stacktrace test helper
```gherkin
Given the function "example1" at line 12 of the stacktrace package's test file calls stacktrace.NewFrameFromCaller
And the repository is checked out in a directory named "doger"
When "example1" is called
Then the returned found flag is true
And Frame.Function is "example1"
And Frame.Line is 12
And Frame.File ends with "stacktrace/stacktrace_test.go"
```

#### Scenario: Provider frame shown by ExplainService
```gherkin
Given a root scope created with doger.New()
And a service registered with <registration>
When the consumer calls <explain>
Then the returned ok flag is true
And ExplainServiceOutput.Invoked.File ends with "<file>"
And ExplainServiceOutput.Invoked.Function starts with "<function>"
And the "Invoked:" line of ExplainServiceOutput.String() does not contain "stacktrace.go:NewFrameFromCaller"

Examples:
  | kind  | registration                                                       | explain                                      | file              | function        |
  | eager | doger.ProvideValue(i, &DB{})                                       | doger.ExplainService[*DB](i)                 | /service_eager.go | newServiceEager |
  | alias | doger.Provide(i, newDB) then doger.As[*DB, doger.Healthchecker](i) | doger.ExplainService[doger.Healthchecker](i) | /service_alias.go | newServiceAlias |
```

#### Scenario: Invocation frame of an alias service is recorded at the alias implementation
```gherkin
Given a root scope with a lazy service whose provider returns a value implementing the exported interface "Healthchecker"
And an alias of it to "Healthchecker"
When the alias instance is requested once
Then exactly 1 invocation frame is recorded for the alias
And that frame's File contains "service_alias.go"
```

Internal names in this requirement's scenarios (`example1`, `newServiceEager`, `newServiceAlias`, file names) appear only where they are the observable `stacktrace.Frame` value a consumer reads, and are kept for that reason.

Invocation frames are recorded internally and are not yet exposed by an exported API. This scenario pins that recorded value because it sits on the invocation hot path.

<!-- cases: 1 happy path: all three scenarios use zero configuration -->
<!-- cases: N/A 2 overrides: frame selection has no consumer option; see proposal Defaults & Control -->
<!-- cases: N/A 3 zero values: the inputs are runtime stacks, not consumer values; an empty GOROOT is covered by the GOROOT requirement -->
<!-- cases: N/A 4 boundaries: the search-depth boundary is covered by the "No caller frame found" requirement -->
<!-- cases: N/A 5 errors: frame selection returns a found flag, never an error; "not found" is covered by the "No caller frame found" requirement -->
<!-- cases: N/A 6 duplicates: duplicate invocation frames are deduplicated by the services' existing frame maps, unchanged here -->
<!-- cases: N/A 7 lifecycle: frame selection is stateless and independent of scope lifecycle -->
<!-- cases: 8 concurrency: covered by the existing concurrent invocation tests of lazy, eager and alias services under -race -->
<!-- cases: N/A 9 panics: frame selection performs no operation that can panic on valid runtime frames -->
<!-- cases: 10 compatibility: the provider frames (newServiceEager, newServiceAlias) a "do" checkout reports today stay the same -->

### Requirement: Own-package classification is independent of file location and module path
Whether a frame of the `stacktrace` package is skipped SHALL depend only on its function's import path and on whether its file is a `_test.go` file. GOROOT detection SHALL be the only rule that inspects the file location. Classification SHALL NOT depend on the checkout directory name, GOPATH, module-cache or vendor layout, or the library's module path.

#### Scenario: Classify frames across layouts and lookalike packages
```gherkin
Given the library's own stacktrace package import path is "<lib>/stacktrace"
And GOROOT is "/usr/local/go"
And a stack frame whose function is "<function>" in file "<file>"
When the frame is classified
Then it is "<result>"

Examples:
  | name                     | lib                         | function                                                        | file                                                                           | result   |
  | own_checkout_do          | github.com/kartaladev/doger | github.com/kartaladev/doger/stacktrace.NewFrameFromCaller       | /home/u/src/do/stacktrace/stacktrace.go                                        | skipped  |
  | own_checkout_doger       | github.com/kartaladev/doger | github.com/kartaladev/doger/stacktrace.NewFrameFromCaller       | /home/u/src/doger/stacktrace/stacktrace.go                                     | skipped  |
  | own_checkout_my_fork     | github.com/kartaladev/doger | github.com/kartaladev/doger/stacktrace.NewFrameFromCaller       | /home/u/src/my-fork/stacktrace/stacktrace.go                                   | skipped  |
  | own_module_cache         | github.com/kartaladev/doger | github.com/kartaladev/doger/stacktrace.NewFrameFromCaller       | /home/u/go/pkg/mod/github.com/kartaladev/doger@v0.1.0/stacktrace/stacktrace.go | skipped  |
  | own_module_cache_renamed | example.com/fork            | example.com/fork/stacktrace.NewFrameFromCaller                  | /home/u/go/pkg/mod/example.com/fork@v0.1.0/stacktrace/stacktrace.go            | skipped  |
  | own_vendored             | github.com/kartaladev/doger | github.com/kartaladev/doger/stacktrace.NewFrameFromCaller       | /home/u/app/vendor/github.com/kartaladev/doger/stacktrace/stacktrace.go        | skipped  |
  | own_closure              | github.com/kartaladev/doger | github.com/kartaladev/doger/stacktrace.NewFrameFromCaller.func1 | /home/u/src/doger/stacktrace/stacktrace.go                                     | skipped  |
  | own_test_helper_doger    | github.com/kartaladev/doger | github.com/kartaladev/doger/stacktrace.example1                 | /home/u/src/doger/stacktrace/stacktrace_test.go                                | reported |
  | own_test_helper_my_fork  | github.com/kartaladev/doger | github.com/kartaladev/doger/stacktrace.example1                 | /home/u/src/my-fork/stacktrace/stacktrace_test.go                              | reported |
  | lib_generic_func         | github.com/kartaladev/doger | github.com/kartaladev/doger.InvokeNamed[...]                    | /home/u/src/doger/di.go                                                        | reported |
  | lib_generic_method       | github.com/kartaladev/doger | github.com/kartaladev/doger.(*serviceLazy[...]).getInstance     | /home/u/src/doger/service_lazy.go                                              | reported |
  | external_test_package    | github.com/kartaladev/doger | github.com/kartaladev/doger/stacktrace_test.TestCaller          | /home/u/src/doger/stacktrace/caller_test.go                                    | reported |
  | lookalike_palantir       | github.com/kartaladev/doger | github.com/palantir/stacktrace.Propagate                        | /home/u/go/pkg/mod/github.com/palantir/stacktrace@v0.0.0/stacktrace.go         | reported |
  | lookalike_acme_do        | github.com/kartaladev/doger | github.com/acme/do/stacktrace.Capture                           | /home/u/src/acme/do/stacktrace/capture.go                                      | reported |
  | sibling_stacktracex      | github.com/kartaladev/doger | github.com/kartaladev/doger/stacktracex.Helper                  | /home/u/src/doger/stacktracex/helper.go                                        | reported |
  | consumer_main            | github.com/kartaladev/doger | main.main                                                       | /home/u/app/main.go                                                            | reported |
```

The `lib_generic_method` row keeps an unexported runtime symbol name as classifier input, because no exported generic type with methods exists to exercise the `(*T[...]).method` symbol form.

<!-- cases: 1 happy path: own_checkout_doger and consumer_main -->
<!-- cases: N/A 2 overrides: classification has no consumer option -->
<!-- cases: N/A 3 zero values: a runtime frame always has a function and a file; an unresolvable PC is covered by the "No caller frame found" requirement; empty GOROOT by the GOROOT requirement -->
<!-- cases: 4 boundaries: covered by the delimiter rows sibling_stacktracex and external_test_package and the lookalike rows -->
<!-- cases: N/A 5 errors: classification returns a boolean and has no error path -->
<!-- cases: N/A 6 duplicates: classification of one frame is a pure function of its inputs -->
<!-- cases: N/A 7 lifecycle: classification is stateless -->
<!-- cases: N/A 8 concurrency: classification reads only values fixed at package init; concurrent use is exercised by the existing -race invocation tests -->
<!-- cases: N/A 9 panics: classification does prefix and suffix checks only -->
<!-- cases: 10 compatibility: own_checkout_do keeps today's result; own_module_cache_renamed covers a module path other than the library's own -->

### Requirement: GOROOT frames are identified by path prefix
A frame SHALL be treated as a GOROOT frame only when GOROOT is non-empty and the frame's file path starts with GOROOT followed by `/`. When GOROOT is empty, as in a `-trimpath` build run without a `GOROOT` environment variable, no frame SHALL be treated as a GOROOT frame, and the other rules SHALL still apply.

#### Scenario: Frames in GOROOT are skipped by path prefix
```gherkin
Given the library's own stacktrace package import path is "github.com/kartaladev/doger/stacktrace"
And GOROOT is "/usr/local/go"
And a stack frame whose function is "<function>" in file "<file>"
When the frame is classified
Then it is "<result>"

Examples:
  | name                   | function        | file                                  | result   |
  | goroot_runtime_goexit  | runtime.goexit  | /usr/local/go/src/runtime/asm_arm64.s | skipped  |
  | goroot_testing_trunner | testing.tRunner | /usr/local/go/src/testing/testing.go  | skipped  |
  | goroot_sibling_gopath  | main.main       | /usr/local/gopath/src/app/main.go     | reported |
```

#### Scenario: Empty GOROOT does not skip every frame
```gherkin
Given the library's own stacktrace package import path is "github.com/kartaladev/doger/stacktrace"
And GOROOT is ""
And a stack frame whose function is "<function>" in file "<file>"
When the frame is classified
Then it is "<result>"

Examples:
  | name                   | function                                                  | file                                                        | result   |
  | goroot_empty_consumer  | main.main                                                 | example.com/app/main.go                                     | reported |
  | goroot_empty_own_frame | github.com/kartaladev/doger/stacktrace.NewFrameFromCaller | github.com/kartaladev/doger@v0.1.0/stacktrace/stacktrace.go | skipped  |
```

#### Scenario: Caller frame in a -trimpath build
```gherkin
Given the stacktrace package's tests are built with "-trimpath"
And the test process runs without a GOROOT environment variable
And the function "example1" at line 12 of the stacktrace package's test file calls stacktrace.NewFrameFromCaller
When "example1" is called
Then the returned found flag is true
And Frame.Function is "example1"
And Frame.Line is 12
```

<!-- cases: 1 happy path: goroot_runtime_goexit and goroot_testing_trunner with the default GOROOT -->
<!-- cases: N/A 2 overrides: GOROOT comes from the runtime; there is no consumer option -->
<!-- cases: 3 zero values: empty GOROOT is covered by "Empty GOROOT does not skip every frame" and the -trimpath scenario -->
<!-- cases: 4 boundaries: goroot_sibling_gopath covers a directory that shares GOROOT's prefix without the "/" separator -->
<!-- cases: N/A 5 errors: classification returns a boolean and has no error path -->
<!-- cases: N/A 6 duplicates: classification of one frame is a pure function of its inputs -->
<!-- cases: N/A 7 lifecycle: GOROOT is read once at package init and never changes -->
<!-- cases: N/A 8 concurrency: GOROOT is read-only after package init -->
<!-- cases: N/A 9 panics: prefix checks cannot panic, including on an empty GOROOT -->
<!-- cases: 10 compatibility: goroot_runtime_goexit and goroot_testing_trunner keep today's result with a non-empty GOROOT -->
