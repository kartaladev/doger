package doger

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shutdownCtxKey marks the context a shutdown test passes.
type shutdownCtxKey struct{}

// ctxShutdowner implements Shutdown(context.Context) without an error and
// records the contexts it received.
type ctxShutdowner struct {
	mu   sync.Mutex
	ctxs []context.Context
}

func (s *ctxShutdowner) Shutdown(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ctxs = append(s.ctxs, ctx)
}

func (s *ctxShutdowner) received() []context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]context.Context(nil), s.ctxs...)
}

// ctxErrShutdowner implements Shutdown(context.Context) error.
type ctxErrShutdowner struct{ ctxShutdowner }

func (s *ctxErrShutdowner) Shutdown(ctx context.Context) error {
	s.ctxShutdowner.Shutdown(ctx)
	return nil
}

// registerShutdowner registers instance as "s" in scope, lazily (and
// invoked, so it is built) or as an eager value.
func registerShutdowner[T any](t *testing.T, scope Injector, kind string, instance T) {
	t.Helper()

	switch kind {
	case "lazy":
		ProvideNamed(scope, "s", func(Injector) (T, error) { return instance, nil })
		_, err := InvokeNamed[T](scope, "s")
		require.NoError(t, err)
	case "eager":
		ProvideNamedValue(scope, "s", instance)
	default:
		t.Fatalf("unknown kind %q", kind)
	}
}

// TestLazyShutdown_ShutdownerWithContext_IsCalled pins that a built service
// implementing Shutdown(ctx), with or without an error result, is shut down
// once with the shutdown context (its values reach the service).
func TestLazyShutdown_ShutdownerWithContext_IsCalled(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"lazy", "eager"} {
		for _, variant := range []string{"Shutdown(ctx)", "Shutdown(ctx) error"} {
			t.Run(kind+"/"+variant, func(t *testing.T) {
				t.Parallel()
				is := assert.New(t)

				root := New()
				tenant := root.Scope("tenant")
				var recorder *ctxShutdowner
				if variant == "Shutdown(ctx)" {
					instance := &ctxShutdowner{}
					registerShutdowner(t, tenant, kind, instance)
					recorder = instance
				} else {
					instance := &ctxErrShutdowner{}
					registerShutdowner(t, tenant, kind, instance)
					recorder = &instance.ctxShutdowner
				}

				ctx := context.WithValue(context.Background(), shutdownCtxKey{}, "marker")
				report := tenant.ShutdownWithContext(ctx)

				require.True(t, report.Succeed, report.Error())
				received := recorder.received()
				if is.Len(received, 1, "Shutdown was called once") {
					is.Equal("marker", received[0].Value(shutdownCtxKey{}), "with the shutdown context")
				}
			})
		}
	}
}

// TestShutdown_EndedContextSkipsShutdowner pins that a built Shutdowner
// reached after the shutdown context ended is not called, and that the
// report holds the context's error for it.
func TestShutdown_EndedContextSkipsShutdowner(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"lazy", "eager"} {
		for _, variant := range []string{"Shutdown(ctx)", "Shutdown(ctx) error"} {
			t.Run(kind+"/"+variant, func(t *testing.T) {
				t.Parallel()
				is := assert.New(t)

				root := New()
				tenant := root.Scope("tenant")
				var recorder *ctxShutdowner
				if variant == "Shutdown(ctx)" {
					instance := &ctxShutdowner{}
					registerShutdowner(t, tenant, kind, instance)
					recorder = instance
				} else {
					instance := &ctxErrShutdowner{}
					registerShutdowner(t, tenant, kind, instance)
					recorder = &instance.ctxShutdowner
				}

				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				report := tenant.ShutdownWithContext(ctx)

				is.False(report.Succeed)
				is.Empty(recorder.received(), "Shutdown was not called")
				err := report.Errors[newServiceDescription(tenant.ID(), tenant.Name(), "s")]
				is.ErrorIs(err, context.Canceled)
			})
		}
	}
}
