package doger

// Collection is a live collection: unlike the snapshot returned by
// [InvokeAll], it resolves its members again on every read, so services
// registered after it was created are included. Create it with
// [InvokeCollection]. A Collection is safe to copy and to read concurrently.
// The zero Collection is bound to no container: its All and Map return an
// error matching [ErrNilInjector].
type Collection[T any] struct {
	scope  *Scope // members resolve from it; nil in a zero Collection
	holder string // the service whose provider created the collection, or ""
}

// InvokeCollection returns a live collection of every visible service
// assignable to T, resolved from the scope of i on each read. It builds
// nothing itself. A nil i panics with [ErrNilInjector]. When i is a
// provider's injector, the service that provider builds (the holder) is
// excluded from every read. The collection keeps the holder's scope and
// name, not its provider's build chain.
func InvokeCollection[T any](i Injector) Collection[T] {
	injector := mustInjector(i)
	holder, _ := invokingServiceName(injector)

	return Collection[T]{scope: injectorScope(injector), holder: holder}
}

// All resolves the members now, with the membership, order, error and hook
// rules of [InvokeAll]. r is the reader's injector: a provider passes its
// own, so that its service is excluded, its dependency on each member is
// recorded and cycles through its build chain are reported, as [InvokeAll]
// called from r does. Code outside a provider passes nil: the read then uses
// no build chain and records no dependency. The reader is excluded by name,
// as [InvokeAs] skips its invoker: a reader in a child scope that shadows a
// member of the holder's scope with its own name excludes that member too.
// On a zero Collection it returns nil and an error matching [ErrNilInjector].
func (c Collection[T]) All(r Injector) ([]T, error) {
	source, err := c.source(r)
	if err != nil {
		return nil, err
	}

	return invokeAllByGenericType[T](source)
}

// Map resolves the members now, keyed by service name, with the rules of
// [InvokeAllMap] and the reader's injector r of [Collection.All]. On a zero
// Collection it returns nil and an error matching [ErrNilInjector].
func (c Collection[T]) Map(r Injector) (map[string]T, error) {
	source, err := c.source(r)
	if err != nil {
		return nil, err
	}

	return invokeAllMapByGenericType[T](source)
}

// source returns where a read with reader injector r finds and builds the
// members: from the holder's scope, excluding the holder, through r when r
// is a provider's injector. A zero Collection has no scope and gives
// ErrNilInjector.
func (c Collection[T]) source(r Injector) (collectionSource, error) {
	if c.scope == nil {
		return collectionSource{}, ErrNilInjector
	}
	reader, _ := r.(*virtualScope)

	return collectionSource{from: c.scope, reader: reader, holder: c.holder}, nil
}
