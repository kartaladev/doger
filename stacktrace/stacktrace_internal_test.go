package stacktrace

import (
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSelfPkgPath_MatchesSymbolPrefix guards the own-package rule
// NewFrameFromCaller passes on: the linker escapes dots in the last
// import-path element of symbol names, so selfPkgPath only matches while
// that element has none.
func TestSelfPkgPath_MatchesSymbolPrefix(t *testing.T) {
	t.Parallel()
	must := require.New(t)

	f := runtime.FuncForPC(reflect.ValueOf(NewFrameFromCaller).Pointer())
	must.NotNil(f)
	must.True(strings.HasPrefix(f.Name(), selfPkgPath+"."), "symbol %q, package path %q", f.Name(), selfPkgPath)
}
