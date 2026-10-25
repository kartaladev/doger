package dogerhttp

import (
	"strings"
	"testing"

	"github.com/kartaladev/doger"
	"github.com/stretchr/testify/assert"
)

func Test_mAp_and_getScopeByID(t *testing.T) {
	t.Parallel()
	is := assert.New(t)

	// mAp
	out := mAp([]int{1, 2, 3}, func(n int) string { return strings.Repeat("x", n) })
	is.Equal([]string{"x", "xx", "xxx"}, out)

	// getScopeByID
	root := doger.New()
	child := root.Scope("child")
	got, ok := getScopeByID(root, child.ID())
	is.True(ok)
	is.Equal(child.ID(), got.ScopeID)

	_, ok = getScopeByID(root, "non-existent")
	is.False(ok)
}
