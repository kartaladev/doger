package doger

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShutdownReport_ErrorFormatting(t *testing.T) {
	t.Parallel()
	is := assert.New(t)

	desc := ServiceDescription{ScopeID: "sid", ScopeName: "sname", Service: "svc"}
	rep := ShutdownReport{
		Succeed:             false,
		Services:            []ServiceDescription{desc},
		Errors:              map[ServiceDescription]error{desc: assert.AnError},
		ShutdownTime:        0,
		ServiceShutdownTime: map[ServiceDescription]time.Duration{desc: time.Millisecond},
	}

	is.Len(rep.Services, 1)
	is.Len(rep.Errors, 1)
	is.Len(rep.ServiceShutdownTime, 1)
	msg := rep.Error()
	is.Contains(msg, "doger: shutdown errors:")
	is.Contains(msg, "sname > svc")
}

func TestScope_Shutdown_ReportFields(t *testing.T) {
	t.Parallel()
	is := assert.New(t)

	i := New()

	// OK shutdowner
	ProvideNamedValue(i, "ok-svc", &lazyTestShutdownerOK{})
	// Failing shutdowner
	ProvideNamedValue(i, "ko-svc", &lazyTestShutdownerKO{})

	// Invoke both
	_, _ = InvokeNamed[*lazyTestShutdownerOK](i, "ok-svc")
	_, _ = InvokeNamed[*lazyTestShutdownerKO](i, "ko-svc")

	rep := i.Shutdown()

	is.NotNil(rep)
	is.False(rep.Succeed)
	is.Len(rep.Errors, 1)
	// Two services were shut down
	is.Len(rep.Services, 2)
	is.Len(rep.ServiceShutdownTime, 2)

	// Error should be attached to the failing service desc
	failing := ServiceDescription{ScopeID: i.self.id, ScopeName: i.self.name, Service: "ko-svc"}
	_, ok := rep.Errors[failing]
	is.True(ok)
}

func TestScope_ShutdownWithContext_ReportTimings(t *testing.T) {
	t.Parallel()
	is := assert.New(t)

	i := New()
	// Create a slow shutdowner (100ms)
	slow := newScopeTestSlowShutdowner(50 * time.Millisecond)
	ProvideNamedValue(i, "slow", slow)
	_, _ = InvokeNamed[*scopeTestSlowShutdowner](i, "slow")

	// Use a generous timeout to allow shutdown to complete
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	rep := i.ShutdownWithContext(ctx)

	is.NotNil(rep)
	is.True(rep.Succeed)
	// One service
	is.Len(rep.Services, 1)
	// Per-service timing should be recorded and > 0
	desc := ServiceDescription{ScopeID: i.self.id, ScopeName: i.self.name, Service: "slow"}
	dt, ok := rep.ServiceShutdownTime[desc]
	is.True(ok)
	is.Greater(dt, time.Duration(0))
}

func TestMergeShutdownReports_Succeed(t *testing.T) {
	t.Parallel()

	desc := ServiceDescription{ScopeID: "sid", ScopeName: "sname", Service: "svc"}
	ok := func() *ShutdownReport {
		return &ShutdownReport{
			Succeed:             true,
			Services:            []ServiceDescription{desc},
			Errors:              map[ServiceDescription]error{},
			ServiceShutdownTime: map[ServiceDescription]time.Duration{},
		}
	}
	failing := &ShutdownReport{
		Succeed:             false,
		Services:            []ServiceDescription{desc},
		Errors:              map[ServiceDescription]error{desc: assert.AnError},
		ServiceShutdownTime: map[ServiceDescription]time.Duration{},
	}

	tests := []struct {
		name    string
		reports []*ShutdownReport
		succeed bool
	}{
		{name: "no reports", reports: nil, succeed: true},
		{name: "two successful", reports: []*ShutdownReport{ok(), ok()}, succeed: true},
		{name: "one failing", reports: []*ShutdownReport{ok(), failing}, succeed: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.succeed, mergeShutdownReports(tt.reports...).Succeed)
		})
	}
}

func TestStartError_ConsumerBuilt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		err     *StartError
		message string
	}{
		{
			name:    "no Scope",
			err:     &StartError{Service: "db", Op: StartOpStart, Cause: errors.New("boom")},
			message: "doger: failed to start service `db` in scope ``: boom",
		},
		{
			name:    "no Cause",
			err:     &StartError{Service: "db", Op: StartOpBuild, Scope: New().self},
			message: "doger: failed to build service `db` in scope `[root]`: <nil>",
		},
		{
			name:    "handle without Scope and Cause",
			err:     &StartError{Service: "j", Kind: "k", Op: StartOpHandle},
			message: "doger: kind `k` failed for service `j` in scope ``: <nil>",
		},
		{
			name:    "zero value",
			err:     &StartError{},
			message: "doger: failed to build service `` in scope ``: <nil>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var message string
			require.NotPanics(t, func() { message = tt.err.Error() })
			assert.Equal(t, tt.message, message)
		})
	}
}

func TestLibErrorf(t *testing.T) {
	t.Parallel()

	errConsumerPrefixed := errors.New("doger: consumer text")
	errConsumer := errors.New("db down")
	startErr := &StartError{Service: "db", Op: StartOpStart, Cause: errConsumer}

	tests := []struct {
		name     string
		err      func() error
		want     string
		matches  []error // each must match with errors.Is
		excludes []error // none may match with errors.Is
		asStart  bool    // errors.As finds startErr
	}{
		{
			name: "no_args",
			err:  func() error { return libErrorf("container start aborted") },
			want: "doger: container start aborted",
		},
		{
			name: "non_error_args",
			err:  func() error { return libErrorf("service `%s` has %d tags", "db", 2) },
			want: "doger: service `db` has 2 tags",
		},
		{
			name:    "sentinel_wrapped_with_w",
			err:     func() error { return libErrorf("cannot start: %w", ErrShutdown) },
			want:    "doger: cannot start: container is shut down",
			matches: []error{ErrShutdown},
		},
		{
			name:     "sentinel_formatted_with_v",
			err:      func() error { return libErrorf("cannot start: %v", ErrShutdown) },
			want:     "doger: cannot start: container is shut down",
			excludes: []error{ErrShutdown},
		},
		{
			name:    "library_error_leading_with_a_sentinel",
			err:     func() error { return libErrorf("member: %w", fmt.Errorf("%w `x`", ErrServiceNotFound)) },
			want:    "doger: member: could not find service `x`",
			matches: []error{ErrServiceNotFound},
		},
		{
			name:    "library_start_error",
			err:     func() error { return libErrorf("aborted: %w", startErr) },
			want:    "doger: aborted: failed to start service `db` in scope ``: db down",
			matches: []error{errConsumer},
			asStart: true,
		},
		{
			name:    "consumer_error_with_the_prefix_kept_verbatim",
			err:     func() error { return libErrorf("waiting: %w", errConsumerPrefixed) },
			want:    "doger: waiting: doger: consumer text",
			matches: []error{errConsumerPrefixed},
		},
		{
			name:    "consumer_error_wrapping_a_sentinel_kept_verbatim",
			err:     func() error { return libErrorf("member: %w", fmt.Errorf("doger: mine: %w", ErrNotStarted)) },
			want:    "doger: member: doger: mine: doger: container not started",
			matches: []error{ErrNotStarted},
		},
		{
			name:    "consumer_error_without_the_prefix",
			err:     func() error { return libErrorf("hook panicked: %w", errConsumer) },
			want:    "doger: hook panicked: db down",
			matches: []error{errConsumer},
		},
		{
			name:    "nested_libErrorf",
			err:     func() error { return libErrorf("outer: %w", libErrorf("inner: %w", ErrNotStarted)) },
			want:    "doger: outer: inner: container not started",
			matches: []error{ErrNotStarted},
		},
		{
			name:    "two_wrapped_errors",
			err:     func() error { return libErrorf("%w: %w", ErrHealthCheckTimeout, context.Canceled) },
			want:    "doger: health check timeout: context canceled",
			matches: []error{ErrHealthCheckTimeout, context.Canceled},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			is := assert.New(t)

			err := tt.err()
			is.EqualError(err, tt.want)
			for _, target := range tt.matches {
				is.ErrorIs(err, target)
			}
			for _, target := range tt.excludes {
				is.NotErrorIs(err, target)
			}
			if tt.asStart {
				var got *StartError
				if is.ErrorAs(err, &got) {
					is.Same(startErr, got)
				}
			}
		})
	}
}
