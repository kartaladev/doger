package stacktrace

import "testing"

// frameAtDepth calls NewFrameFromCaller under depth extra frames of this
// test file.
//
//go:noinline
func frameAtDepth(depth int) (Frame, bool) {
	if depth == 0 {
		return NewFrameFromCaller()
	}
	return frameAtDepth(depth - 1)
}

func BenchmarkNewFrameFromCaller(b *testing.B) {
	for _, bm := range []struct {
		name  string
		depth int
	}{{name: "shallow", depth: 0}, {name: "depth_20", depth: 20}} {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, ok := frameAtDepth(bm.depth); !ok {
					b.Fatal("no frame")
				}
			}
		})
	}
}
