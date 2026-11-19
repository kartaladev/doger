// Package frames finds and formats the caller frames the library records as
// the source of a registration or an invocation. It backs the exported
// stacktrace package, and lets the root package skip its own frames without
// widening the stacktrace API.
package frames

import (
	"reflect"
	"runtime"
	"strings"
)

///
/// Inspired by palantir/stacktrace repo
/// -> https://github.com/palantir/stacktrace/blob/master/stacktrace.go
/// -> Apache 2.0 LICENSE
///

// MaxDepth is the largest number of frames Caller inspects.
const MaxDepth = 32

var (
	// goroot is read once at package init: runtime.GOROOT() re-reads an
	// env var on every call, and it cannot change during the process lifetime.
	//nolint:staticcheck // SA1019: runtime.GOROOT is deprecated, but it is the build's GOROOT that stack frame paths start with
	goroot = runtime.GOROOT()

	// selfPkgPath is the import path of this package, derived at runtime so
	// that frame classification does not depend on the module path.
	selfPkgPath = reflect.TypeFor[pkgMarker]().PkgPath()
)

// pkgMarker is an empty type used to derive this package's import path.
type pkgMarker struct{}

// Frame is a caller frame. Its fields match stacktrace.Frame, which
// converts from it.
type Frame struct {
	PC       uintptr
	File     string
	Function string
	Line     int
}

// Caller returns the first frame, among the depth frames (at most MaxDepth)
// above the caller of Caller once skip of them are dropped, that is not in
// GOROOT, not in this package, and not in one of pkgPaths except in its
// _test.go files. A skip of 0 starts at the caller of Caller.
//
// The stack is read in one runtime.Callers pass into a buffer of depth
// entries, and its frames are resolved one at a time, stopping at the first
// one reported. The buffer is on the heap whatever its type, since
// runtime.Frames keeps it, so it is sized to depth.
func Caller(skip, depth int, pkgPaths ...string) (Frame, bool) {
	depth = min(max(depth, 0), MaxDepth)
	pcs := make([]uintptr, depth)
	// skip runtime.Callers, Caller and the skip frames asked for
	n := runtime.Callers(skip+2, pcs)
	frames := runtime.CallersFrames(pcs[:n])

	return firstReportable(func() (runtime.Frame, bool) {
		frame, more := frames.Next()
		return frame, frame.PC != 0 || more
	}, depth, pkgPaths)
}

// firstReportable returns the first of the first depth frames next yields
// that skipFrame keeps, for this package and each of pkgPaths. next reports
// false once there is no frame left.
func firstReportable(next func() (runtime.Frame, bool), depth int, pkgPaths []string) (Frame, bool) {
	for range depth {
		frame, ok := next()
		if !ok || frame.Function == "" {
			break
		}
		if !skipped(frame.Function, frame.File, pkgPaths) {
			return New(frame.PC, frame.File, frame.Function, frame.Line), true
		}
	}

	return Frame{}, false
}

// skipped reports whether the frame of function in file is skipped: in
// GOROOT, in this package, or in one of pkgPaths outside its test files.
func skipped(function, file string, pkgPaths []string) bool {
	if skipFrame(selfPkgPath, goroot, function, file) {
		return true
	}
	for _, pkgPath := range pkgPaths {
		if skipFrame(pkgPath, "", function, file) {
			return true
		}
	}

	return false
}

// skipFrame reports whether a frame must be skipped when looking for the
// caller: frames in GOROOT, and frames of the package selfPkg except those
// declared in _test.go files. function is the fully qualified function name.
func skipFrame(selfPkg, goroot, function, file string) bool {
	if goroot != "" && strings.HasPrefix(file, goroot+"/") {
		return true
	}

	return strings.HasPrefix(function, selfPkg+".") && !strings.HasSuffix(file, "_test.go")
}

// FromPC returns the frame of the function containing pc, as declared, and
// false when pc does not resolve to a function.
func FromPC(pc uintptr) (Frame, bool) {
	fun := runtime.FuncForPC(pc)
	if fun == nil {
		return Frame{}, false
	}

	file, line := fun.FileLine(pc)

	return New(pc, file, fun.Name(), line), true
}

// New builds the reported Frame from a raw file path and a fully qualified
// function name: the path is made GOPATH-relative and the name is
// shortened.
func New(pc uintptr, file, function string, line int) Frame {
	return Frame{
		PC:       pc,
		File:     removeGoPath(file),
		Function: shortFuncName(function),
		Line:     line,
	}
}

// ptrReceiverReplacer strips the pointer-receiver syntax left over after
// splitting off the package path, e.g. "(*PtrReceiver).MethodName" ->
// "PtrReceiver.MethodName". The pattern is "(*" (not bare "(" and "*"
// separately) so a generic receiver's type arguments, which can themselves
// contain '*' (e.g. "(*ServiceLazy[*int]).getInstance"), are left untouched;
// NewReplacer tries the longest match at each position, so "(*" wins over
// the bare "(" fallback whenever both would apply.
var ptrReceiverReplacer = strings.NewReplacer("(*", "", "(", "", ")", "")

func shortFuncName(longName string) string {
	// f.Name() is like one of these:
	// - "github.com/palantir/shield/package.FuncName"
	// - "github.com/palantir/shield/package.Receiver.MethodName"
	// - "github.com/palantir/shield/package.(*PtrReceiver).MethodName"
	withoutPath := longName[strings.LastIndex(longName, "/")+1:]
	withoutPackage := withoutPath[strings.Index(withoutPath, ".")+1:]

	return ptrReceiverReplacer.Replace(withoutPackage)
}
