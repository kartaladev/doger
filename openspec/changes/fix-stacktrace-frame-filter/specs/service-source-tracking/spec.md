# Spec Delta

## Purpose

Lets the container record and report where a service was provided and invoked, by picking the right stack frame consistently in every checkout and install layout.

## ADDED Requirements

### Requirement: Caller frame skips the stacktrace package's own frames
When asked for the caller frame, the library SHALL skip frames whose function belongs to its own `stacktrace` package, plus frames in GOROOT. It SHALL report the first remaining frame, including frames from other packages of the library, with that frame's file, function name and line.

#### Scenario: Caller frame from a stacktrace test helper
```gherkin
Given the test helper "example1" in "stacktrace/stacktrace_test.go" at line 12 asks for its caller frame
And the repository is checked out in a directory named "<checkout>"
When the caller frame is computed
Then the result is found
And the frame function is "example1"
And the frame line is 12
And the frame file ends with "stacktrace/stacktrace_test.go"

Examples:
  | checkout |
  | do       |
  | doger    |
  | my-fork  |
```

#### Scenario: Provider frame of library-created services points at the creating library function
```gherkin
Given a root scope
And a "<kind>" service created by the library
When the provider frame of that service is inferred
Then the result is found
And the frame file contains "<file>"
And the frame function contains "<function>"

Examples:
  | kind  | file             | function        |
  | eager | service_eager.go | newServiceEager |
  | alias | service_alias.go | newServiceAlias |
```

#### Scenario: Invocation frame of an alias service is recorded at the alias implementation
```gherkin
Given a root scope with a lazy service of type "*lazyTestHeathcheckerOK"
And an alias service "Healthchecker" for it
When the alias service instance is requested once
Then exactly one invocation frame is recorded
And the invocation frame file contains "service_alias.go"
```

<!-- cases: N/A 2 overrides: frame selection has no consumer option; see proposal Defaults & Control -->
<!-- cases: N/A 7 lifecycle: frame selection is stateless and independent of scope lifecycle -->
<!-- cases: N/A 9 panics: frame selection performs no operation that can panic on valid runtime frames -->

### Requirement: Frame classification is independent of file location and module path
Whether a frame is skipped SHALL depend only on the package import path of the frame's function, and on whether its file is a `_test.go` file. It SHALL NOT depend on the checkout directory name, GOPATH or module-cache layout, or the library's module path.

#### Scenario: Classify frames across layouts and lookalike packages
```gherkin
Given the library's own stacktrace package import path is "<lib>/stacktrace"
And a stack frame whose function is "<function>" in file "<file>"
When the frame is classified
Then it is "<result>"

Examples:
  | lib                        | function                                            | file                                                                          | result   |
  | github.com/samber/do/v2    | github.com/samber/do/v2/stacktrace.NewFrameFromCaller | /home/u/src/do/stacktrace/stacktrace.go                                      | skipped  |
  | github.com/samber/do/v2    | github.com/samber/do/v2/stacktrace.NewFrameFromCaller | /home/u/src/doger/stacktrace/stacktrace.go                                   | skipped  |
  | github.com/samber/do/v2    | github.com/samber/do/v2/stacktrace.NewFrameFromCaller | /home/u/go/pkg/mod/github.com/samber/do/v2@v2.0.0/stacktrace/stacktrace.go  | skipped  |
  | github.com/kartaladev/doger | github.com/kartaladev/doger/stacktrace.NewFrameFromCaller | /home/u/go/pkg/mod/github.com/kartaladev/doger@v0.1.0/stacktrace/stacktrace.go | skipped |
  | github.com/samber/do/v2    | github.com/samber/do/v2/stacktrace.NewFrameFromCaller.func1 | /home/u/src/doger/stacktrace/stacktrace.go                             | skipped  |
  | github.com/samber/do/v2    | github.com/samber/do/v2/stacktrace.example1         | /home/u/src/doger/stacktrace/stacktrace_test.go                               | reported |
  | github.com/samber/do/v2    | github.com/samber/do/v2.newServiceEager[...]        | /home/u/src/doger/service_eager.go                                            | reported |
  | github.com/samber/do/v2    | github.com/samber/do/v2.(*serviceLazy[...]).getInstance | /home/u/src/doger/service_lazy.go                                         | reported |
  | github.com/samber/do/v2    | github.com/palantir/stacktrace.Propagate            | /home/u/go/pkg/mod/github.com/palantir/stacktrace@v0.0.0/stacktrace.go        | reported |
  | github.com/samber/do/v2    | github.com/acme/do/stacktrace.Capture               | /home/u/src/acme/do/stacktrace/capture.go                                     | reported |
  | github.com/samber/do/v2    | github.com/samber/do/v2/stacktracex.Helper          | /home/u/src/doger/stacktracex/helper.go                                       | reported |
  | github.com/samber/do/v2    | main.main                                           | /home/u/app/main.go                                                           | reported |
```

#### Scenario: Frames in GOROOT are skipped
```gherkin
Given GOROOT is "/usr/local/go"
And a stack frame whose function is "<function>" in file "<file>"
When the frame is classified
Then it is "skipped"

Examples:
  | function                     | file                                      |
  | runtime.goexit               | /usr/local/go/src/runtime/asm_arm64.s     |
  | testing.tRunner              | /usr/local/go/src/testing/testing.go      |
```

<!-- cases: N/A 2 overrides: classification has no consumer option -->
<!-- cases: N/A 3 zero values: a runtime frame always has a function and a file; an unresolvable PC is covered by the "No caller frame found" requirement -->
<!-- cases: N/A 6 duplicates: classification of one frame is a pure function of its inputs -->
<!-- cases: N/A 7 lifecycle: classification is stateless -->
<!-- cases: N/A 8 concurrency: classification reads only values fixed at package init; concurrent use is exercised by the existing -race invocation tests -->
<!-- cases: N/A 9 panics: classification does string and prefix checks only -->

### Requirement: No caller frame found
The library SHALL report "not found", returning a zero frame and `false`, when the program counter cannot be resolved to a function, or when no reportable frame appears within the first 10 frames. In either case it SHALL return without panicking.

#### Scenario: Search depth boundary
```gherkin
Given a call stack whose first <skippable> frames are all skippable
And frame <next> is reportable
When the caller frame is computed with a search depth of 10
Then the result found is <found>

Examples:
  | skippable | next | found |
  | 9         | 10   | true  |
  | 10        | 11   | false |
```

#### Scenario: Unresolvable program counter
```gherkin
Given the program counter 0
When a frame is built from that program counter
Then the result found is false
And the frame is the zero frame
```

<!-- cases: N/A 2 overrides: the search depth is internal and not configurable -->
<!-- cases: N/A 8 concurrency: stateless -->
<!-- cases: N/A 9 panics: covered by "without panicking" in the scenarios above -->

### Requirement: Existing frame formats are preserved
Frame file paths SHALL remain relative to the GOPATH `src` directory when the file is inside GOPATH, and unchanged otherwise. `Frame.String()` SHALL keep the format `file:function:line`.

#### Scenario: Frame string format
```gherkin
Given the test helper "example1" in "stacktrace/stacktrace_test.go" at line 12 asks for its caller frame
When the frame is formatted as a string
Then the string ends with "stacktrace/stacktrace_test.go:example1:12"
```

<!-- cases: N/A 2 overrides: no option -->
<!-- cases: N/A 4 boundaries: GOPATH prefix handling is already pinned by TestRemoveGoPath and is unchanged -->
<!-- cases: N/A 5 errors: formatting cannot fail -->
<!-- cases: N/A 7 lifecycle, 8 concurrency, 9 panics: pure formatting of an existing value -->
<!-- cases: 10 compatibility: covered by this requirement and by the provider/invocation frame scenarios above -->
