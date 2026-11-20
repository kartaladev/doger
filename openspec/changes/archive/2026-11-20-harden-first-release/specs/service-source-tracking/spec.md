# Spec Delta

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
And a service registered with <registration> on line <line> of "source_tracking_test.go" inside the test function "<function>"
When the consumer calls <explain>
Then the returned ok flag is true
And ExplainServiceOutput.Invoked.File ends with "/source_tracking_test.go"
And ExplainServiceOutput.Invoked.Function starts with "<function>"
And ExplainServiceOutput.Invoked.Line is <line>
And the "Invoked:" line of ExplainServiceOutput.String() does not contain "stacktrace.go:NewFrameFromCaller"

Examples:
  | kind  | registration                                                          | explain                                      | function                           | line |
  | eager | doger.ProvideValue(i, &DB{})                                          | doger.ExplainService[*DB](i)                 | TestSourceTracking_ProviderFrame   | 40   |
  | alias | doger.Provide(i, newDB) then doger.Alias[*DB, doger.Healthchecker](i) | doger.ExplainService[doger.Healthchecker](i) | TestSourceTracking_ProviderFrame   | 52   |
```

#### Scenario: Invocation frame of an alias service is recorded at the alias implementation
```gherkin
Given a root scope with a lazy service whose provider returns a value implementing the exported interface "Healthchecker"
And an alias of it to "Healthchecker"
When the alias instance is requested once
Then exactly 1 invocation frame is recorded for the alias
And that frame's File contains "service_alias.go"
```

Internal names in this requirement's scenarios (`example1`, file names) appear only where they are the observable `stacktrace.Frame` value a consumer reads, and are kept for that reason. The `line` column holds the line where the test registers the service; the test asserts against the line it records with `runtime.Caller` at that call, so the literal values are illustrative.

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
<!-- cases: 10 compatibility: BREAKING for eager and alias provider frames, which now report the consumer's call site instead of service_eager.go/service_alias.go; invocation frames are unchanged -->

## ADDED Requirements

### Requirement: Registration source is the consumer's call site
The provider frame recorded for a value or alias registration SHALL be the first frame outside the library's root package (frames from `_test.go` files of the root package count as outside), so it names the consumer's file, function and line that registered the service, on every registration form.

#### Scenario: Source frame of each registration form
```gherkin
Given a root scope created with doger.New()
When the consumer registers "*DB" with <form> on a line it records with runtime.Caller
Then doger.ExplainService[<explained>](root) returns ok true
And Invoked.File is the recorded file
And Invoked.Line is the recorded line
And Invoked.File does not end with "service_eager.go", "service_alias.go", "di.go" or "root_scope.go"

Examples:
  | form                                                                 | explained          |
  | doger.ProvideValue(root, &DB{})                                      | *DB                |
  | doger.ProvideNamedValue(root, "*DB", &DB{})                          | *DB                |
  | doger.OverrideValue(root, &DB{})                                     | *DB                |
  | doger.OverrideNamedValue(root, "*DB", &DB{})                         | *DB                |
  | doger.Package(doger.Eager(&DB{}))(root)                              | *DB                |
  | doger.Package(doger.EagerNamed("*DB", &DB{}))(root)                  | *DB                |
  | doger.Provide(root, newDB) then doger.Alias[*DB, doger.Healthchecker](root) | doger.Healthchecker |
```

#### Scenario: Registration through New records the New call site
```gherkin
Given the consumer calls doger.New(doger.Eager(&DB{})) on a line it records with runtime.Caller
When the consumer calls doger.ExplainService[*DB] on the returned root
Then Invoked.Line is the recorded line of the doger.New call
```

<!-- cases: 1 happy path: ProvideValue row -->
<!-- cases: N/A 2 overrides: no option -->
<!-- cases: 3 zero values: a nil value (doger.ProvideValue[*DB](root, nil)) records the same call site; covered as an extra row in the test table -->
<!-- cases: 4 boundaries: registration called directly (depth 1) and through New/Package (depth 3+) -->
<!-- cases: N/A 5 invalid input: registration has no error path relevant to frames -->
<!-- cases: 6 override: OverrideValue and OverrideNamedValue rows -->
<!-- cases: N/A 7 lifecycle: the frame is captured once, at registration -->
<!-- cases: N/A 8 concurrency: frame capture is per call; covered by the existing -race suite -->
<!-- cases: N/A 9 panics: capture does not panic; "No caller frame found" covers the not-found result -->
<!-- cases: 10 compatibility: lazy and transient services already reported the provider's location and are unchanged -->
