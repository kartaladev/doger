package bench

import (
	"reflect"
	"testing"

	"github.com/kartaladev/doger/stacktrace"
)

func BenchmarkNewFrameFromCaller(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		_, _ = stacktrace.NewFrameFromCaller()
	}
}

func BenchmarkNewFrameFromPC(b *testing.B) {
	pc := reflect.ValueOf(stacktrace.NewFrameFromPC).Pointer()

	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		_, _ = stacktrace.NewFrameFromPC(pc)
	}
}
