package doger

import (
	"context"
	"reflect"
	"slices"
)

// kindID identifies a kind. NewKind allocates a new one, so two kinds with
// the same name stay distinct.
type kindID struct {
	name string
}

// kindBinding opts a registration into a kind.
type kindBinding struct {
	id       *kindID
	meta     any
	target   reflect.Type // the kind's T
	deferred bool         // registered under an interface type: checked on the instance at Start
}

// Stage says when a kind handler runs during Start. The constants start at
// 1: the zero Stage is invalid, [Kind.Handle] panics on it and MarshalText
// fails for it. A Stage reads and writes as the text "before-start" or
// "after-start".
type Stage int

const (
	// StageBeforeStart runs after every candidate is built, before any Starter starts.
	StageBeforeStart Stage = iota + 1
	// StageAfterStart runs after every Starter has started.
	StageAfterStart
)

// String returns "before-start", "after-start", or "unknown" for any other
// value.
func (s Stage) String() string {
	switch s {
	case StageBeforeStart:
		return "before-start"
	case StageAfterStart:
		return "after-start"
	default:
		return unknownText
	}
}

// MarshalText encodes a valid stage by its String text and fails for any
// other value.
func (s Stage) MarshalText() ([]byte, error) {
	if !validStage(s) {
		return nil, libErrorf("invalid stage %d", s)
	}

	return []byte(s.String()), nil
}

// validStage reports whether stage is one of the Stage constants.
func validStage(stage Stage) bool {
	return stage == StageBeforeStart || stage == StageAfterStart
}

// UnmarshalText decodes "before-start" or "after-start"; any other text
// fails and leaves s unchanged.
func (s *Stage) UnmarshalText(text []byte) error {
	switch string(text) {
	case "before-start":
		*s = StageBeforeStart
	case "after-start":
		*s = StageAfterStart
	default:
		return libErrorf("unknown stage %q", text)
	}

	return nil
}

// Kind is a typed component kind: services opted in with [Kind.With] are
// passed, at Start, to the handler registered with [Kind.Handle]. T is the
// interface (or type) every component must satisfy, and M is the metadata
// attached to each registration.
type Kind[T, M any] struct {
	id *kindID
}

// Component is a service opted into a kind, as passed to the kind's handler.
//
// Scope is the scope the service is registered in. For a service of the root
// scope it is the root's *Scope, not the *RootScope: a Shutdown reached
// through it bypasses the root lifecycle.
type Component[T, M any] struct {
	Scope    *Scope
	Name     string
	Instance T
	Meta     M
}

// kindEntry is a type-erased handler registration.
type kindEntry struct {
	id    *kindID
	stage Stage
	run   func(ctx context.Context, scope *Scope, name string, instance any, meta any) error
}

// NewKind creates a kind with target type T, metadata type M, and a name used
// in errors.
func NewKind[T, M any](name string) Kind[T, M] {
	return Kind[T, M]{id: &kindID{name: name}}
}

// Name returns the kind's name, or "" for a zero Kind.
func (k Kind[T, M]) Name() string {
	if k.id == nil {
		return ""
	}

	return k.id.name
}

// With returns a registration option that opts the service into the kind
// with the given metadata.
//
// Registration panics when the option is applied to a transient service,
// applied twice to one registration, or applied to a concrete type not
// assignable to T. A service registered under an interface type is checked
// on its instance at Start instead. With panics on a zero Kind.
func (k Kind[T, M]) With(meta M) ProvideOption {
	k.mustBeCreated()

	return func(o *serviceOptions) {
		o.kinds = append(o.kinds, kindBinding{id: k.id, meta: meta, target: reflect.TypeFor[T]()})
	}
}

// Handle registers the kind's handler on root.
// A later call for the same kind replaces both handler and stage; handlers
// of one stage run kind by kind, in the order kinds were first given a
// handler. Opted-in services of a kind without a handler are built at Start,
// and nothing else happens to them.
//
// At Start, the handler is called once per component, in dependency order,
// then scope depth, then registration order, with the Start-time context.
// The first error, or a recovered panic, stops Start. A handler call that
// failed during Refresh or Scope.Start is made again for that component by
// the next one that covers it. A handler may call
// [DeclareDependency]; edges declared during [StageBeforeStart] order the
// Starters of the same Start. Do not call Shutdown from a handler.
//
// The root is explicit: there is no default container. Handle panics on a
// zero Kind, an invalid stage (Stage(0) included), a nil handler, or a nil
// root, typed-nil included, with an error matching [ErrNilInjector].
func (k Kind[T, M]) Handle(root *RootScope, stage Stage, handler func(context.Context, Component[T, M]) error) {
	k.mustBeCreated()
	if !validStage(stage) {
		panic(libErrorf("invalid stage %d for kind `%s`", stage, k.id.name))
	}
	if handler == nil {
		panic(libErrorf("nil handler for kind `%s`", k.id.name))
	}
	if root == nil {
		panic(ErrNilInjector)
	}

	entry := k.newEntry(stage, handler)

	root.kindsMu.Lock()
	defer root.kindsMu.Unlock()

	// copy on write: the list may be shared with other containers
	kinds := slices.Clone(root.kinds)
	for i := range kinds {
		if kinds[i].id == k.id {
			kinds[i] = entry // keeps the slot: order of first Handle
			root.kinds = kinds
			return
		}
	}
	kinds = append(kinds, entry)
	root.kinds = kinds
}

// newEntry type-erases handler into a kind entry for stage.
func (k Kind[T, M]) newEntry(stage Stage, handler func(context.Context, Component[T, M]) error) kindEntry {
	return kindEntry{
		id:    k.id,
		stage: stage,
		run: func(ctx context.Context, scope *Scope, name string, instance any, meta any) error {
			typed, ok := instance.(T)
			if !ok {
				return libErrorf("service `%s` does not implement `%s` required by kind `%s`", name, reflect.TypeFor[T]().String(), k.id.name)
			}
			typedMeta, _ := meta.(M)

			return handler(ctx, Component[T, M]{Scope: scope, Name: name, Instance: typed, Meta: typedMeta})
		},
	}
}

// target returns the kind's target type T.
func (k Kind[T, M]) target() reflect.Type {
	return reflect.TypeFor[T]()
}

func (k Kind[T, M]) mustBeCreated() {
	if k.id == nil {
		panic(libErrorf("kind not created with NewKind"))
	}
}
