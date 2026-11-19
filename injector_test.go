package doger

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestGetInjector(t *testing.T) {
	t.Parallel()

	root := New()
	scope := root.Scope("child")

	tests := []struct {
		name     string
		injector Injector
		want     Injector
		err      error
	}{
		{name: "nil", injector: nil, err: ErrNilInjector},
		{name: "typed_nil_root", injector: (*RootScope)(nil), err: ErrNilInjector},
		{name: "typed_nil_scope", injector: (*Scope)(nil), err: ErrNilInjector},
		{name: "root", injector: root, want: root},
		{name: "scope", injector: scope, want: scope},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := getInjector(tt.injector)

			assert.ErrorIs(t, err, tt.err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestInjectorOpts_addHook(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 100*time.Millisecond)
	is := assert.New(t)

	hookBeforeRegistration := func(scope *Scope, serviceName string) {}
	hookAfterRegistration := func(scope *Scope, serviceName string) {}
	hookBeforeInvocation := func(scope *Scope, serviceName string) {}
	hookAfterInvocation := func(scope *Scope, serviceName string, elapsed time.Duration, err error) {}
	hookBeforeShutdown := func(ctx context.Context, scope *Scope, serviceName string) {}
	hookAfterShutdown := func(ctx context.Context, scope *Scope, serviceName string, elapsed time.Duration, err error) {}

	i := New()

	//
	is.Empty(i.hooks.beforeRegistration.load())
	is.Empty(i.hooks.afterRegistration.load())
	is.Empty(i.hooks.beforeInvocation.load())
	is.Empty(i.hooks.afterInvocation.load())
	is.Empty(i.hooks.beforeShutdown.load())
	is.Empty(i.hooks.afterShutdown.load())

	//
	i.AddBeforeRegistrationHook(hookBeforeRegistration)
	is.Len(i.hooks.beforeRegistration.load(), 1)
	is.Empty(i.hooks.afterRegistration.load())
	is.Empty(i.hooks.beforeInvocation.load())
	is.Empty(i.hooks.afterInvocation.load())
	is.Empty(i.hooks.beforeShutdown.load())
	is.Empty(i.hooks.afterShutdown.load())

	//
	i.AddAfterRegistrationHook(hookAfterRegistration)
	is.Len(i.hooks.beforeRegistration.load(), 1)
	is.Len(i.hooks.afterRegistration.load(), 1)
	is.Empty(i.hooks.beforeInvocation.load())
	is.Empty(i.hooks.afterInvocation.load())
	is.Empty(i.hooks.beforeShutdown.load())
	is.Empty(i.hooks.afterShutdown.load())

	//
	i.AddBeforeInvocationHook(hookBeforeInvocation)
	is.Len(i.hooks.beforeRegistration.load(), 1)
	is.Len(i.hooks.afterRegistration.load(), 1)
	is.Len(i.hooks.beforeInvocation.load(), 1)
	is.Empty(i.hooks.afterInvocation.load())
	is.Empty(i.hooks.beforeShutdown.load())
	is.Empty(i.hooks.afterShutdown.load())

	//
	i.AddAfterInvocationHook(hookAfterInvocation)
	is.Len(i.hooks.beforeRegistration.load(), 1)
	is.Len(i.hooks.afterRegistration.load(), 1)
	is.Len(i.hooks.beforeInvocation.load(), 1)
	is.Len(i.hooks.afterInvocation.load(), 1)
	is.Empty(i.hooks.beforeShutdown.load())
	is.Empty(i.hooks.afterShutdown.load())

	//
	i.AddBeforeShutdownHook(hookBeforeShutdown)
	is.Len(i.hooks.beforeRegistration.load(), 1)
	is.Len(i.hooks.afterRegistration.load(), 1)
	is.Len(i.hooks.beforeInvocation.load(), 1)
	is.Len(i.hooks.afterInvocation.load(), 1)
	is.Len(i.hooks.beforeShutdown.load(), 1)
	is.Empty(i.hooks.afterShutdown.load())

	//
	i.AddAfterShutdownHook(hookAfterShutdown)
	is.Len(i.hooks.beforeRegistration.load(), 1)
	is.Len(i.hooks.afterRegistration.load(), 1)
	is.Len(i.hooks.beforeInvocation.load(), 1)
	is.Len(i.hooks.afterInvocation.load(), 1)
	is.Len(i.hooks.beforeShutdown.load(), 1)
	is.Len(i.hooks.afterShutdown.load(), 1)
}

func TestInjectorOpts_onEvent(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 100*time.Millisecond)
	is := assert.New(t)

	result := ""

	hookBeforeRegistration := func(scope *Scope, serviceName string) { result += "a" }
	hookAfterRegistration := func(scope *Scope, serviceName string) { result += "b" }
	hookBeforeInvocation := func(scope *Scope, serviceName string) { result += "c" }
	hookAfterInvocation := func(scope *Scope, serviceName string, elapsed time.Duration, err error) { result += "d" }
	hookBeforeShutdown := func(ctx context.Context, scope *Scope, serviceName string) { result += "e" }
	hookAfterShutdown := func(ctx context.Context, scope *Scope, serviceName string, elapsed time.Duration, err error) {
		result += "f"
	}

	i := NewWithOpts(&InjectorOpts{
		HookBeforeRegistration: []func(scope *Scope, serviceName string){hookBeforeRegistration},
		HookAfterRegistration:  []func(scope *Scope, serviceName string){hookAfterRegistration},
		HookBeforeInvocation:   []func(scope *Scope, serviceName string){hookBeforeInvocation},
		HookAfterInvocation:    []func(scope *Scope, serviceName string, elapsed time.Duration, err error){hookAfterInvocation},
		HookBeforeShutdown:     []func(ctx context.Context, scope *Scope, serviceName string){hookBeforeShutdown},
		HookAfterShutdown:      []func(ctx context.Context, scope *Scope, serviceName string, elapsed time.Duration, err error){hookAfterShutdown},
	})

	i.hooks.onBeforeRegistration(&Scope{id: "id", name: "name"}, "name")
	i.hooks.onAfterRegistration(&Scope{id: "id", name: "name"}, "name")
	i.hooks.onBeforeInvocation(&Scope{id: "id", name: "name"}, "name")
	i.hooks.onAfterInvocation(&Scope{id: "id", name: "name"}, "name", 0, nil)
	for _, hook := range i.hooks.beforeShutdown.load() {
		hook(context.Background(), &Scope{id: "id", name: "name"}, "name")
	}
	for _, hook := range i.hooks.afterShutdown.load() {
		hook(context.Background(), &Scope{id: "id", name: "name"}, "name", 0, nil)
	}

	is.Equal("abcdef", result)

	i.AddBeforeRegistrationHook(func(scope *Scope, serviceName string) { result += "1" })
	i.AddAfterRegistrationHook(func(scope *Scope, serviceName string) { result += "2" })
	i.AddBeforeInvocationHook(func(scope *Scope, serviceName string) { result += "3" })
	i.AddAfterInvocationHook(func(scope *Scope, serviceName string, elapsed time.Duration, err error) { result += "4" })
	i.AddBeforeShutdownHook(func(ctx context.Context, scope *Scope, serviceName string) { result += "5" })
	i.AddAfterShutdownHook(func(ctx context.Context, scope *Scope, serviceName string, elapsed time.Duration, err error) {
		result += "6"
	})

	result = ""

	i.hooks.onBeforeRegistration(&Scope{id: "id", name: "name"}, "name")
	i.hooks.onAfterRegistration(&Scope{id: "id", name: "name"}, "name")
	i.hooks.onBeforeInvocation(&Scope{id: "id", name: "name"}, "name")
	i.hooks.onAfterInvocation(&Scope{id: "id", name: "name"}, "name", 0, nil)
	for _, hook := range i.hooks.beforeShutdown.load() {
		hook(context.Background(), &Scope{id: "id", name: "name"}, "name")
	}
	for _, hook := range i.hooks.afterShutdown.load() {
		hook(context.Background(), &Scope{id: "id", name: "name"}, "name", 0, nil)
	}

	is.Equal("a1b2c3d4e5f6", result)
}
