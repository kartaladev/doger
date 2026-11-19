// Package dogerhttp renders the doger debug web UI: the scope tree, the
// service list and each service's explanation, as HTML pages for a doger
// injector. The adapter modules under github.com/kartaladev/doger/http (std,
// chi, gin, echo, fiber) mount these pages on a router; [WriteHeaders] sets
// the security headers each page is served with.
package dogerhttp
