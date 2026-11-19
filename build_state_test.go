package doger

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseGoroutineID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		stack string
		want  uint64
	}{
		{name: "running", stack: "goroutine 123 [running]:\nmain.main()", want: 123},
		{name: "one", stack: "goroutine 1 [running]:", want: 1},
		{name: "large", stack: "goroutine 18446744073709551615 [running]:", want: 18446744073709551615},
		{name: "truncated_after_id", stack: "goroutine 42 ", want: 42},
		{name: "no_prefix", stack: "thread 5 [running]:", want: 0},
		{name: "not_a_number", stack: "goroutine abc [running]:", want: 0},
		{name: "empty", stack: "", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, parseGoroutineID([]byte(tt.stack)))
		})
	}
}

func TestGoroutineID_DistinctPerGoroutine(t *testing.T) {
	t.Parallel()
	is := assert.New(t)

	own := goroutineID()
	other := make(chan uint64)
	go func() { other <- goroutineID() }()

	is.NotZero(own)
	is.Equal(own, goroutineID(), "stable on one goroutine")
	is.NotEqual(own, <-other)
}
