package stacktrace

import (
	"fmt"
	"reflect"

	"github.com/kartaladev/doger/internal/frames"
)

///
/// Inspired by palantir/stacktrace repo
/// -> https://github.com/palantir/stacktrace/blob/master/stacktrace.go
/// -> Apache 2.0 LICENSE
///

// selfPkgPath is the import path of this package, derived at runtime so
// that frame classification does not depend on the module path.
var selfPkgPath = reflect.TypeFor[pkgMarker]().PkgPath()

// pkgMarker is an empty type used to derive this package's import path.
type pkgMarker struct{}

// callerDepth is how many frames above NewFrameFromCaller it inspects.
const callerDepth = 8

// NewFrameFromCaller creates a new Frame from the current call stack.
// This function walks up the call stack to find the first frame that is not
// in the stacktrace package or Go runtime, providing useful debugging
// information about where a service was invoked from.
//
// The function filters out:
//   - Frames in the Go runtime (GOROOT)
//   - Frames of the stacktrace package itself, except those declared in
//     _test.go files
//
// Frames of other library packages are reported: the caller of a service
// constructor is the library code that built it.
//
// Returns a Frame representing the caller and a boolean indicating success.
// The boolean is false if no suitable frame was found.
//
// This function is used internally by the DI container to track service
// invocation locations for debugging and explanation purposes.
func NewFrameFromCaller() (Frame, bool) {
	// skip this function: it is in this package
	frame, ok := frames.Caller(1, callerDepth, selfPkgPath)
	return Frame(frame), ok
}

// NewFrameFromPC creates a new Frame from a program counter (PC) value.
// This function is used to create Frame objects from function pointers,
// typically for tracking where service providers were defined.
//
// Parameters:
//   - pc: The program counter value representing a function
//
// Returns a Frame representing the function and a boolean indicating success.
// The boolean is false if the PC value is invalid.
//
// This function is used internally to track service provider locations
// for debugging and explanation purposes.
func NewFrameFromPC(pc uintptr) (Frame, bool) {
	frame, ok := frames.FromPC(pc)
	return Frame(frame), ok
}

// Frame represents a single stack frame with debugging information.
// This struct contains information about a function call location,
// including the file, line number, function name, and program counter.
type Frame struct {
	PC       uintptr
	File     string
	Function string
	Line     int
}

// String returns a formatted string representation of the frame.
// The format is "file:function:line" which is useful for debugging
// and logging purposes.
//
// Returns a string in the format "path/to/file.go:FunctionName:123".
func (f Frame) String() string {
	return fmt.Sprintf("%s:%s:%d", f.File, f.Function, f.Line)
}
