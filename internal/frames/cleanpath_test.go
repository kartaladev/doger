package frames

///
/// Stolen from palantir/stacktrace repo
/// -> https://github.com/palantir/stacktrace/blob/master/cleanpath/gopath_test.go
/// -> Apache 2.0 LICENSE
///

import (
	"runtime"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRemoveGoPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		gopath   []string
		path     string
		expected string
		windows  bool // the row uses Windows separators
	}{
		{
			name:     "empty_gopath",
			gopath:   []string{},
			path:     "/some/dir/src/pkg/prog.go",
			expected: "/some/dir/src/pkg/prog.go",
		},
		{
			name:     "single_matching_dir",
			gopath:   []string{"/some/dir"},
			path:     "/some/dir/src/pkg/prog.go",
			expected: "pkg/prog.go",
		},
		{
			name:     "nonmatching_dir",
			gopath:   []string{"/other/dir"},
			path:     "/some/dir/src/pkg/prog.go",
			expected: "/some/dir/src/pkg/prog.go",
		},
		{
			name:     "multiple_matching_dirs_shorter_first",
			gopath:   []string{"/some", "/some/src/dir"},
			path:     "/some/src/dir/src/pkg/prog.go",
			expected: "pkg/prog.go",
		},
		{
			name:     "multiple_matching_dirs_longer_first",
			gopath:   []string{"/some/src/dir", "/some"},
			path:     "/some/src/dir/src/pkg/prog.go",
			expected: "pkg/prog.go",
		},
		{
			name:     "dir_with_trailing_separator",
			gopath:   []string{"/some/dir/"},
			path:     "/some/dir/src/pkg/prog.go",
			expected: "pkg/prog.go",
		},
		{
			name:     "sibling_dir_sharing_a_prefix",
			gopath:   []string{"/some/dir"},
			path:     "/some/dir/srcx/pkg/prog.go",
			expected: "/some/dir/srcx/pkg/prog.go",
		},
		{
			// a Windows GOPATH uses backslashes; frame paths use slashes
			name:     "windows_gopath",
			gopath:   []string{`C:\Users\me\go`},
			path:     "C:/Users/me/go/src/pkg/prog.go",
			expected: "pkg/prog.go",
			windows:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.windows && runtime.GOOS != "windows" {
				t.Skip("backslashes separate paths on Windows only")
			}

			dirs := make([]string, len(tt.gopath))
			copy(dirs, tt.gopath)
			sort.Stable(longestFirst(dirs))

			cleaned := removeGoPathDirs(tt.path, dirs)
			assert.Equal(t, tt.expected, cleaned, "testcase: %+v", tt)
		})
	}
}
