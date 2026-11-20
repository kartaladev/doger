# Spec Delta

## Purpose

Defines the contract of the debug web UI that every HTTP adapter (`dogerhttpstd`, `dogerhttpgin`, `dogerhttpecho`, `dogerhttpfiber`, `dogerchi`) serves. It covers safe rendering of names and links, defensive response headers, bounded dependency trees, source-path display, read-only behavior, and mounting behind the consumer's own middleware.

## ADDED Requirements

### Requirement: Names are rendered as text
Every page (home, scope tree, service list, service page) SHALL render each scope name, service name, dependency name and dependent name as HTML text. A name SHALL NOT be able to inject an element, an attribute or a script into the page, whatever characters it contains. This holds on every adapter.

#### Scenario: Hostile names are escaped on every page
```gherkin
Given a root container with a child scope named "<name>"
And a lazy service named "<name>" in that child scope that depends on a root service named "dep-<name>"
And the service was invoked once
And the debug UI is mounted with <adapter> at "/debug/doger"
When the consumer sends GET "<page>"
Then the response status is 200
And the body does not contain the raw text "<name>"
And the body contains "<escaped>"
And parsing the body as HTML finds no "script" or "img" element and no attribute whose name starts with "on"

Examples:
  | adapter        | name                            | page                                                  | escaped                                      |
  | dogerhttpstd   | <script>alert(1)</script>       | /debug/doger/scope?scope_id=<root id>                 | &lt;script&gt;alert(1)&lt;/script&gt;        |
  | dogerhttpstd   | "><img src=x onerror=alert(1)>  | /debug/doger/service                                  | &#34;&gt;&lt;img src=x onerror=alert(1)&gt;  |
  | dogerhttpstd   | <script>alert(1)</script>       | /debug/doger/service?scope_id=<child id>&service_name=%3Cscript%3Ealert(1)%3C%2Fscript%3E | &lt;script&gt;alert(1)&lt;/script&gt; |
  | dogerhttpgin   | <script>alert(1)</script>       | /debug/doger/scope?scope_id=<root id>                 | &lt;script&gt;alert(1)&lt;/script&gt;        |
  | dogerhttpgin   | "><img src=x onerror=alert(1)>  | /debug/doger/service                                  | &#34;&gt;&lt;img src=x onerror=alert(1)&gt;  |
  | dogerhttpecho  | <script>alert(1)</script>       | /debug/doger/scope?scope_id=<root id>                 | &lt;script&gt;alert(1)&lt;/script&gt;        |
  | dogerhttpecho  | "><img src=x onerror=alert(1)>  | /debug/doger/service                                  | &#34;&gt;&lt;img src=x onerror=alert(1)&gt;  |
  | dogerhttpfiber | <script>alert(1)</script>       | /debug/doger/scope?scope_id=<root id>                 | &lt;script&gt;alert(1)&lt;/script&gt;        |
  | dogerhttpfiber | "><img src=x onerror=alert(1)>  | /debug/doger/service                                  | &#34;&gt;&lt;img src=x onerror=alert(1)&gt;  |
  | dogerchi       | <script>alert(1)</script>       | /debug/doger/scope?scope_id=<root id>                 | &lt;script&gt;alert(1)&lt;/script&gt;        |
  | dogerchi       | "><img src=x onerror=alert(1)>  | /debug/doger/service                                  | &#34;&gt;&lt;img src=x onerror=alert(1)&gt;  |
```

#### Scenario: Page functions escape names without an adapter
```gherkin
Given a root container with a lazy service named "<name>"
When the consumer calls dogerhttp.<function> with base path "/debug/doger"
Then the returned error is nil
And the returned HTML does not contain the raw text "<name>"
And the returned HTML contains "<escaped>"

Examples:
  | function        | name                           | escaped                                      |
  | ServiceListHTML | <script>alert(1)</script>      | &lt;script&gt;alert(1)&lt;/script&gt;        |
  | ScopeTreeHTML   | <script>alert(1)</script>      | &lt;script&gt;alert(1)&lt;/script&gt;        |
  | ServiceHTML     | "><img src=x onerror=alert(1)> | &#34;&gt;&lt;img src=x onerror=alert(1)&gt;  |
  | ServiceListHTML | plain-name                     | plain-name                                   |
```

#### Scenario: A base path cannot inject markup
```gherkin
Given a root container
When the consumer calls dogerhttp.IndexHTML with base path "/x\"><script>alert(1)</script>"
Then the returned HTML contains no "script" element
And every link on the page starts with "/x%22%3E%3Cscript%3Ealert%281%29%3C/script%3E"
```

<!-- cases: N/A 3 zero values (nil injector): the adapters take the container the consumer mounts; a nil injector is rejected by the core API (resolution-errors), not by the UI -->
<!-- cases: N/A 6 not found/duplicate: covered by "Missing or unknown identifiers fall back to list pages" -->
<!-- cases: N/A 7 lifecycle: covered by "The UI is read-only" and "Concurrent viewing is race-free" -->
<!-- cases: N/A 8 concurrency: covered by "Concurrent viewing is race-free" -->
<!-- cases: N/A 9 panics: no request input reaches a panic path; rendering errors are covered by "Rendering failures reveal no details" -->
<!-- cases: N/A 10 compatibility: plain names render unchanged (last Examples row of "Page functions escape names without an adapter") -->

### Requirement: Links carry escaped parameters
Every link the UI renders to a scope or a service SHALL encode its `scope_id` and `service_name` query values with URL query escaping. Following a link SHALL open the page of exactly the scope or service it names, for any name.

#### Scenario: A link opens the service it names
```gherkin
Given a root container with a lazy service named "<name>"
And the debug UI is mounted with dogerhttpstd at "/debug/doger"
When the consumer sends GET "/debug/doger/service" and follows the link whose text is the service name
Then the link's query contains "service_name=<encoded>"
And the followed page shows "Service name:" followed by the escaped form of "<name>"

Examples:
  | name                      | encoded                                        |
  | a&b#c                     | a%26b%23c                                      |
  | <script>alert(1)</script> | %3Cscript%3Ealert%281%29%3C%2Fscript%3E        |
  | with space                | with+space                                     |
  | plain                     | plain                                          |
```

#### Scenario: Links resolve on every adapter
```gherkin
Given a root container with a lazy service named "a&b#c"
And the debug UI is mounted with <adapter> at "/debug/doger"
When the consumer sends GET "/debug/doger/service" and follows the link for "a&b#c"
Then the followed page's status is 200
And the followed page shows the service page of "a&amp;b#c", not the service list

Examples:
  | adapter        |
  | dogerhttpstd   |
  | dogerhttpgin   |
  | dogerhttpecho  |
  | dogerhttpfiber |
  | dogerchi       |
```

<!-- cases: N/A 2 overrides: link encoding has no option -->
<!-- cases: N/A 3 zero values: an empty name cannot be registered (core registration rules) -->
<!-- cases: N/A 4 boundaries: encoding is per character; covered by the Examples rows -->
<!-- cases: N/A 5 errors: encoding cannot fail -->
<!-- cases: N/A 6 not found: covered by "Missing or unknown identifiers fall back to list pages" -->
<!-- cases: N/A 7, 8, 9: links are static text; no lifecycle, concurrency or panic behavior -->
<!-- cases: N/A 10 compatibility: plain names keep the same URLs (row "plain") -->

### Requirement: Responses carry defensive headers
Every response the UI writes on every adapter, including the redirect from the scope page, SHALL set `Content-Type: text/html; charset=utf-8` (pages only), `X-Content-Type-Options: nosniff`, `Content-Security-Policy: default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'` and `Cache-Control: no-store`.

#### Scenario: Headers are set on every route
```gherkin
Given a root container with a lazy service named "db"
And the debug UI is mounted with <adapter> at "/debug/doger"
When the consumer sends GET "<path>"
Then the response status is <status>
And the header "X-Content-Type-Options" is "nosniff"
And the header "Content-Security-Policy" is "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'"
And the header "Cache-Control" is "no-store"
And the header "Content-Type" is <content type>

Examples:
  | adapter        | path                                                   | status | content type                 |
  | dogerhttpstd   | /debug/doger                                           | 200    | "text/html; charset=utf-8"   |
  | dogerhttpstd   | /debug/doger/scope                                     | 302    | any                          |
  | dogerhttpstd   | /debug/doger/scope?scope_id=<root id>                  | 200    | "text/html; charset=utf-8"   |
  | dogerhttpstd   | /debug/doger/service                                   | 200    | "text/html; charset=utf-8"   |
  | dogerhttpstd   | /debug/doger/service?scope_id=<root id>&service_name=db | 200   | "text/html; charset=utf-8"   |
  | dogerhttpgin   | /debug/doger                                           | 200    | "text/html; charset=utf-8"   |
  | dogerhttpgin   | /debug/doger/scope                                     | 302    | any                          |
  | dogerhttpgin   | /debug/doger/service                                   | 200    | "text/html; charset=utf-8"   |
  | dogerhttpecho  | /debug/doger                                           | 200    | "text/html; charset=utf-8"   |
  | dogerhttpecho  | /debug/doger/scope                                     | 302    | any                          |
  | dogerhttpecho  | /debug/doger/service                                   | 200    | "text/html; charset=utf-8"   |
  | dogerhttpfiber | /debug/doger                                           | 200    | "text/html; charset=utf-8"   |
  | dogerhttpfiber | /debug/doger/scope                                     | 302    | any                          |
  | dogerhttpfiber | /debug/doger/service                                   | 200    | "text/html; charset=utf-8"   |
  | dogerchi       | /debug/doger                                           | 200    | "text/html; charset=utf-8"   |
  | dogerchi       | /debug/doger/scope                                     | 302    | any                          |
  | dogerchi       | /debug/doger/service                                   | 200    | "text/html; charset=utf-8"   |
```

#### Scenario: Consumer middleware can add headers
```gherkin
Given the debug UI is mounted with dogerhttpstd at "/debug/doger" inside a consumer middleware that sets "Strict-Transport-Security: max-age=63072000"
When the consumer sends GET "/debug/doger/service"
Then the header "Strict-Transport-Security" is "max-age=63072000"
And the header "X-Content-Type-Options" is "nosniff"
```

<!-- cases: N/A 3 zero values: headers do not depend on input -->
<!-- cases: N/A 4 boundaries: no numeric input -->
<!-- cases: N/A 5 errors: header writing cannot fail; the error status path is covered by "Rendering failures reveal no details" -->
<!-- cases: N/A 6 not found: unknown ids render a list page and are covered by the /service rows -->
<!-- cases: N/A 7, 8, 9: headers are static per response -->
<!-- cases: N/A 10 compatibility: Content-Type is unchanged; the other headers are new (BREAKING only for consumers framing the UI in an iframe, see design D10/D12) -->

### Requirement: Dependency trees are bounded
A service page SHALL expand each service of its dependency and dependent trees at most once. A service met again SHALL be rendered as a link to its own page, marked as a reference, with no children. The page size SHALL grow with the number of dependency edges, not with the number of paths.

#### Scenario: A layered graph renders each service once
```gherkin
Given a root container with 24 lazy services "svc-00" to "svc-23", where each "svc-N" with N >= 2 depends on "svc-(N-1)" and "svc-(N-2)", and "svc-01" depends on "svc-00"
And every service was invoked through "svc-23"
And the debug UI is mounted with <adapter> at "/debug/doger"
When the consumer sends GET "/debug/doger/service?scope_id=<root id>&service_name=svc-23"
Then the response status is 200
And the dependency tree lists 45 entries: 23 expanded and 22 marked as references
And the response body is smaller than 65536 bytes
And the response is written within 1s

Examples:
  | adapter        |
  | dogerhttpstd   |
  | dogerhttpgin   |
  | dogerhttpecho  |
  | dogerhttpfiber |
  | dogerchi       |
```

#### Scenario: Tree size follows the graph shape
```gherkin
Given a root container whose service "top" has a dependency graph shaped as <graph>
When the consumer calls dogerhttp.ServiceHTML for "top" in the root scope
Then the dependency tree lists <entries> entries, of which <refs> are marked as references

Examples:
  | graph                                                         | entries | refs |
  | no dependencies                                               | 0       | 0    |
  | one dependency "a"                                            | 1       | 0    |
  | chain "a" -> "b" -> "c"                                       | 3       | 0    |
  | diamond: "top" depends on "a" and "b", both depend on "c"     | 4       | 1    |
  | 10 layers of 3: "top" depends on all of layer 1, each service of layers 1-9 on all 3 of the next layer | 84 | 54 |
```

#### Scenario: A reference links to the referenced service
```gherkin
Given a root container where "a" and "b" both depend on "c", and "top" depends on "a" and "b"
When the consumer calls dogerhttp.ServiceHTML for "top" in the root scope
Then the second entry for "c" is marked as a reference
And it links to "/debug/doger/service?scope_id=<root id>&service_name=c"
And it has no children
```

<!-- cases: N/A 2 overrides: the bound has no option; full trees are reachable by following references -->
<!-- cases: N/A 3 zero values: covered by the "no dependencies" row -->
<!-- cases: N/A 5 errors: rendering a bounded tree cannot fail on graph shape -->
<!-- cases: N/A 6 not found: covered by "Missing or unknown identifiers fall back to list pages" -->
<!-- cases: N/A 7 cycles: the core forbids dependency cycles among built services; a reference would bound one anyway -->
<!-- cases: N/A 8 concurrency: covered by "Concurrent viewing is race-free" -->
<!-- cases: N/A 9 panics: no panic path -->
<!-- cases: N/A 10 compatibility: BREAKING by design (D11); trees without repeats render the same entries as before (chain row) -->

### Requirement: Source locations are shortened by default
By default the service page SHALL show where a service was provided and invoked as `<file base name>:<line>`, never a directory. With the mount option `WithFullSourcePaths()`, passed to an adapter's `Use` or to a page function, it SHALL show the full recorded path.

#### Scenario: Source path display follows the option
```gherkin
Given a root container with a lazy service "db" provided and invoked from the test file "web_ui_test.go" at line <line>
And the debug UI is mounted with <adapter> at "/debug/doger" with options <options>
When the consumer sends GET "/debug/doger/service?scope_id=<root id>&service_name=db"
Then the "Invoked at" value is <shown>

Examples:
  | adapter        | options                 | shown                                              |
  | dogerhttpstd   | none                    | "web_ui_test.go:<line>"                            |
  | dogerhttpstd   | WithFullSourcePaths()   | the absolute path ending in "/http/std/web_ui_test.go:<line>" |
  | dogerhttpgin   | none                    | "web_ui_test.go:<line>"                            |
  | dogerhttpecho  | none                    | "web_ui_test.go:<line>"                            |
  | dogerhttpfiber | none                    | "web_ui_test.go:<line>"                            |
  | dogerchi       | none                    | "web_ui_test.go:<line>"                            |
  | dogerchi       | WithFullSourcePaths()   | the absolute path ending in "/http/chi/web_ui_test.go:<line>" |
```

#### Scenario: Shortening handles unusual paths
```gherkin
Given a recorded source location "<recorded>"
When the UI shortens it for display
Then it shows "<shown>"

Examples:
  | recorded                                   | shown                 |
  | /home/ci/work/app/main.go:42               | main.go:42            |
  | C:\Users\ci\app\main.go:42                 | main.go:42            |
  | main.go:42                                 | main.go:42            |
  | ""                                         | ""                    |
  | /a/<script>.go:7                           | &lt;script&gt;.go:7   |
```

#### Scenario: A service never invoked shows no location
```gherkin
Given a root container with a lazy service "db" that was never invoked
When the consumer calls dogerhttp.ServiceHTML for "db" in the root scope
Then the "Invoked at" value is empty
```

<!-- cases: N/A 5 errors: shortening cannot fail -->
<!-- cases: N/A 6 duplicate option: passing WithFullSourcePaths() twice behaves like once (idempotent setter, no observable difference to test beyond the option row) -->
<!-- cases: N/A 7, 8: display-only, no lifecycle or concurrency behavior -->
<!-- cases: N/A 9 panics: no panic path; shortening is fuzzed (Test Strategy) -->
<!-- cases: N/A 10 compatibility: BREAKING display change by design; full paths remain available through WithFullSourcePaths() -->

### Requirement: The UI is read-only
Serving any page SHALL NOT build, invoke, health-check, start or shut down any service, and SHALL NOT register or remove anything. A request with a method other than GET or HEAD SHALL NOT render a page and SHALL be answered with a 4xx status.

#### Scenario: Viewing every page builds nothing
```gherkin
Given a root container with a lazy service "db" whose provider counts its calls, that implements HealthCheck and Shutdown with counters
And a child scope "tenant" with a transient service "req"
And the debug UI is mounted with <adapter> at "/debug/doger"
When the consumer sends GET to the home, scope tree, service list, "db" service page and "req" service page
Then every response status is 200 or 302
And the provider of "db" was called 0 times
And HealthCheck was called 0 times
And Shutdown was called 0 times
And the root still lists the same services and child scopes as before the requests

Examples:
  | adapter        |
  | dogerhttpstd   |
  | dogerhttpgin   |
  | dogerhttpecho  |
  | dogerhttpfiber |
  | dogerchi       |
```

#### Scenario: Non-GET methods are rejected
```gherkin
Given a root container with a lazy service "db"
And the debug UI is mounted with <adapter> at "/debug/doger"
When the consumer sends <method> "/debug/doger/service"
Then the response status is between 400 and 499
And the body contains no "<!DOCTYPE html>"

Examples:
  | adapter        | method |
  | dogerhttpstd   | POST   |
  | dogerhttpstd   | DELETE |
  | dogerhttpgin   | POST   |
  | dogerhttpecho  | POST   |
  | dogerhttpfiber | POST   |
  | dogerchi       | PUT    |
```

<!-- cases: N/A 2 overrides: read-only behavior has no option -->
<!-- cases: N/A 3, 4: no input values -->
<!-- cases: N/A 6 not found: covered by "Missing or unknown identifiers fall back to list pages" -->
<!-- cases: N/A 7 after shutdown: covered by "Concurrent viewing is race-free" (viewer after root shutdown) -->
<!-- cases: N/A 9 panics: no panic path -->
<!-- cases: N/A 10 compatibility: GET behavior unchanged; non-GET on dogerhttpstd was previously served (BREAKING only for consumers POSTing to the UI, which had no use) -->

### Requirement: Missing or unknown identifiers fall back to list pages
A scope page request without `scope_id` SHALL redirect with status 302 to the root scope's page. A service page request with a missing, empty or unknown `scope_id` or `service_name` SHALL render the service list. No page SHALL echo a query value that does not name an existing scope or service.

#### Scenario: Fallbacks for missing and unknown identifiers
```gherkin
Given a root container with id "<root id>" and a lazy service "db"
And the debug UI is mounted with dogerhttpstd at "/debug/doger"
When the consumer sends GET "<path>"
Then the response status is <status>
And <outcome>

Examples:
  | path                                                          | status | outcome                                                          |
  | /debug/doger/scope                                            | 302    | Location is "/debug/doger/scope?scope_id=<root id>"              |
  | /debug/doger/scope?scope_id=                                  | 302    | Location is "/debug/doger/scope?scope_id=<root id>"              |
  | /debug/doger/scope?scope_id=unknown                           | 200    | the body shows the whole scope tree                              |
  | /debug/doger/service?scope_id=<root id>                       | 200    | the body is the service list                                     |
  | /debug/doger/service?service_name=db                          | 200    | the body is the service list                                     |
  | /debug/doger/service?scope_id=unknown&service_name=db         | 200    | the body is the service list                                     |
  | /debug/doger/service?scope_id=<root id>&service_name=missing  | 200    | the body is the service list                                     |
```

#### Scenario: Query values are never reflected
```gherkin
Given a root container with a lazy service "db"
And the debug UI is mounted with <adapter> at "/debug/doger"
When the consumer sends GET "/debug/doger/service?scope_id=%3Cscript%3Ex%3C%2Fscript%3E&service_name=%3Cimg%20src%3Dx%3E"
Then the response status is 200
And the body contains neither "<script>x</script>" nor "&lt;script&gt;x" nor "<img src=x>"

Examples:
  | adapter        |
  | dogerhttpstd   |
  | dogerhttpgin   |
  | dogerhttpecho  |
  | dogerhttpfiber |
  | dogerchi       |
```

<!-- cases: N/A 2 overrides: fallback behavior has no option -->
<!-- cases: N/A 4 boundaries: identifiers are compared for equality only -->
<!-- cases: N/A 5 errors: unknown identifiers are not errors; they fall back -->
<!-- cases: N/A 7, 8, 9: covered by "The UI is read-only" and "Concurrent viewing is race-free" -->
<!-- cases: N/A 10 compatibility: the existing fallback behavior is unchanged; this requirement pins it -->

### Requirement: Rendering failures reveal no details
When a page cannot be rendered, the adapter SHALL respond with status 500, the defensive headers, and a body that does not contain the underlying error text. The error SHALL be passed, once per failed request, to the handler given with `dogerhttp.WithErrorHandler(func(ctx context.Context, err error))`, where `ctx` is the request's context. Without that option the error is discarded and nothing is written to stdout, stderr or a global logger.

#### Scenario: A failing page returns a generic 500
```gherkin
Given a page renderer that fails with the error "template: secret path /srv/app/x.go"
And the debug UI response helper of <adapter>
When the helper writes that failure
Then the response status is 500
And the body does not contain "/srv/app/x.go"
And the header "X-Content-Type-Options" is "nosniff"

Examples:
  | adapter        |
  | dogerhttpstd   |
  | dogerhttpgin   |
  | dogerhttpecho  |
  | dogerhttpfiber |
  | dogerchi       |
```

#### Scenario: A consumer error handler receives the render failure
```gherkin
Given a page renderer that fails with the error "template: secret path /srv/app/x.go"
And the debug UI is mounted with <adapter> at "/debug/doger" with options <options>
When the consumer sends GET "/debug/doger/service" with a request context carrying the value "req-42"
Then the response status is 500
And the body does not contain "/srv/app/x.go"
And the handler was called <calls> time(s) with an error whose message contains "/srv/app/x.go" and a context carrying "req-42"

Examples:
  | adapter        | options                       | calls |
  | dogerhttpstd   | WithErrorHandler(record)      | 1     |
  | dogerhttpgin   | WithErrorHandler(record)      | 1     |
  | dogerhttpecho  | WithErrorHandler(record)      | 1     |
  | dogerhttpfiber | WithErrorHandler(record)      | 1     |
  | dogerchi       | WithErrorHandler(record)      | 1     |
  | dogerhttpstd   | none                          | 0     |
  | dogerhttpstd   | WithErrorHandler(nil)         | 0     |
  | dogerhttpstd   | WithErrorHandler(a), WithErrorHandler(record) | 1 (only record; the last option wins) |
```

<!-- cases: N/A 1, 3, 4: the success path never calls the handler (no-handler and nil rows cover zero values); success pages are covered by the other requirements -->
<!-- cases: N/A 6, 7: no registry or lifecycle behavior -->
<!-- cases: N/A 8 concurrency: the handler is called on the request goroutine; concurrent failures each call it once, covered by "Concurrent viewing is race-free" running with a recording handler -->
<!-- cases: N/A 9 panics: a panicking handler propagates to the framework's recovery, which is the consumer's middleware choice; the 500 response is not guaranteed in that case -->
<!-- cases: N/A 10 compatibility: chi and std previously wrote err.Error() into the body; echo and fiber passed the error to the framework error handler. All adapters now write the generic 500 and pass the error only to WithErrorHandler (intended change, D12) -->

### Requirement: The UI mounts behind consumer middleware
Each adapter SHALL let the consumer place the UI behind its own middleware with that framework's usual form, so that a request the middleware rejects never reaches the UI. `dogerchi.Use` SHALL accept any `chi.Router`, including the routers returned by `With` and passed to `Group`.

#### Scenario: Authentication middleware guards the UI
```gherkin
Given a root container with a lazy service "db"
And a basic-auth middleware that accepts user "admin" with password "s3cret"
And the debug UI is mounted at "/debug/doger" with <mount>
When the consumer sends GET "/debug/doger/service" with credentials <credentials>
Then the response status is <status>

Examples:
  | mount                                                                 | credentials   | status |
  | dogerhttpstd: auth(dogerhttpstd.Use("/debug/doger", root))            | none          | 401    |
  | dogerhttpstd: auth(dogerhttpstd.Use("/debug/doger", root))            | admin:s3cret  | 200    |
  | dogerhttpgin: dogerhttpgin.Use(r.Group("/debug/doger", auth), root)   | none          | 401    |
  | dogerhttpgin: dogerhttpgin.Use(r.Group("/debug/doger", auth), root)   | admin:s3cret  | 200    |
  | dogerhttpecho: dogerhttpecho.Use(e.Group("/debug/doger", auth), "/debug/doger", root) | none | 401 |
  | dogerhttpecho: dogerhttpecho.Use(e.Group("/debug/doger", auth), "/debug/doger", root) | admin:s3cret | 200 |
  | dogerhttpfiber: dogerhttpfiber.Use(app.Group("/debug/doger", auth), "/debug/doger", root) | none | 401 |
  | dogerhttpfiber: dogerhttpfiber.Use(app.Group("/debug/doger", auth), "/debug/doger", root) | admin:s3cret | 200 |
  | dogerchi: dogerchi.Use(r.With(auth), "/debug/doger", root)            | none          | 401    |
  | dogerchi: dogerchi.Use(r.With(auth), "/debug/doger", root)            | admin:s3cret  | 200    |
  | dogerchi: r.Group(func(g chi.Router) { g.Use(auth); dogerchi.Use(g, "/debug/doger", root) }) | none | 401 |
  | dogerchi: r.Group(func(g chi.Router) { g.Use(auth); dogerchi.Use(g, "/debug/doger", root) }) | admin:s3cret | 200 |
```

#### Scenario: Existing chi mounts keep compiling
```gherkin
Given a consumer program that calls dogerchi.Use(chi.NewRouter(), "/debug/doger", root) with a *chi.Mux
When the program is built
Then it compiles without changes
And GET "/debug/doger/service" returns status 200
```

<!-- cases: N/A 3 zero values: a nil router is a programming error of the framework call, not UI behavior -->
<!-- cases: N/A 4 boundaries: no numeric input -->
<!-- cases: N/A 5 errors: rejection is the middleware's own response (401 rows) -->
<!-- cases: N/A 6 duplicate mount: mounting twice under different paths is ordinary framework routing -->
<!-- cases: N/A 7, 8, 9: covered by "The UI is read-only" and "Concurrent viewing is race-free" -->

### Requirement: Concurrent viewing is race-free
Serving pages concurrently with scope creation, service invocation and scope or root shutdown SHALL be free of data races and panics. After root shutdown, pages SHALL still be served.

#### Scenario: Viewers race with container churn
```gherkin
Given a root container with a lazy service "db"
And the debug UI is mounted with <adapter> at "/debug/doger"
When 4 goroutines each create, invoke a service in, and shut down 50 child scopes while 4 goroutines each send 50 GET requests across all pages
Then the race detector reports no race
And every response status is 200 or 302
And after root Shutdown, GET "/debug/doger/service" returns status 200

Examples:
  | adapter        |
  | dogerhttpstd   |
  | dogerchi       |
```

<!-- cases: N/A 1-6: functional behavior is covered by the other requirements -->
<!-- cases: N/A 9 panics: covered by "every response status is 200 or 302" under churn -->
<!-- cases: N/A 10 compatibility: the existing behavior passed this check in the audit; the requirement pins it -->
