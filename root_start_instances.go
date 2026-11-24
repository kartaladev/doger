package doger

import (
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
)

// abandonedInstances counts the abandoned calls that still run, by instance
// and step key, for a container and its clones, which share it. Only
// pointer-like instances are counted. Its mutex is a leaf of the lock order:
// it is taken alone or under startedMu, never takes another lock and never
// runs user code.
type abandonedInstances struct {
	mu    sync.Mutex
	calls map[instanceStep]int
	// changes counts the changes of calls; written under mu, read without
	// it by a dependents check that asks whether it is still current
	changes atomic.Uint64
}

// instanceStep is the key of a step on its instance: the Start method (a
// nil kind) or a kind's handler call.
type instanceStep struct {
	instance any
	kind     *kindID
}

// instanceStepOf returns the key of step on its instance, and false when
// the instance is not tracked by instance (see trackableInstance).
func instanceStepOf(step startStep) (instanceStep, bool) {
	instance := step.candidate.instance
	if !trackableInstance(instance) {
		return instanceStep{}, false
	}

	return instanceStep{instance: instance, kind: step.kind}, true
}

// trackableInstance reports whether instance is tracked by instance: only
// a non-nil pointer to a value of non-zero size, a non-nil channel or a
// non-nil unsafe pointer is one object that several registrations can
// share and also a valid map key (a map is shared too, but hashing it
// panics). Any other value is tracked per registration only: equal values
// are copies, distinct pointers to a zero-size value may share an address,
// and typed nil pointers of one type are equal, so none of them may block
// another registration.
func trackableInstance(instance any) bool {
	if instance == nil {
		return false
	}
	value := reflect.ValueOf(instance)
	kind := value.Kind()
	// IsNil panics for any other kind
	if kind != reflect.Pointer && kind != reflect.Chan && kind != reflect.UnsafePointer {
		return false
	}
	if value.IsNil() {
		return false
	}

	return kind != reflect.Pointer || value.Type().Elem().Size() != 0
}

// add counts an abandoned call of step that still runs.
func (a *abandonedInstances) add(step startStep) {
	key, ok := instanceStepOf(step)
	if !ok {
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if a.calls == nil {
		a.calls = map[instanceStep]int{}
	}
	a.calls[key]++
	a.changes.Add(1)
}

// remove uncounts an abandoned call of step that ended.
func (a *abandonedInstances) remove(step startStep) {
	key, ok := instanceStepOf(step)
	if !ok {
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	a.changes.Add(1)
	// several containers sharing a can each have abandoned a call of it
	if a.calls[key] > 1 {
		a.calls[key]--
		return
	}
	delete(a.calls, key)
}

// running reports whether an abandoned call of step's instance and step key
// still runs, in this container or one sharing a.
func (a *abandonedInstances) running(step startStep) bool {
	key, ok := instanceStepOf(step)
	if !ok {
		return false
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	return a.calls[key] > 0
}

// snapshot returns the step keys of each instance with an abandoned call
// still running, nil when there is none, and the change count it reflects.
func (a *abandonedInstances) snapshot() (steps map[any][]*kindID, changes uint64) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if len(a.calls) == 0 {
		return nil, a.changes.Load()
	}
	steps = make(map[any][]*kindID, len(a.calls))
	for key := range a.calls {
		steps[key.instance] = append(steps[key.instance], key.kind)
	}

	return steps, a.changes.Load()
}

// trackedStillRunning returns the still-running error of the service desc
// of s when the instance it holds, built already, has an abandoned call
// still running in tracked (a snapshot), for a step that s has not
// recorded for that service: its Start method first, then the kinds by
// name. An alias or an export stands for its target (see healthSource).
// It returns nil
// otherwise, and for a service that is not built.
func (s *RootScope) trackedStillRunning(desc ServiceDescription, tracked map[any][]*kindID) *StartError {
	scope := s.scopeByID(desc.ScopeID)
	if scope == nil {
		return nil
	}
	service, found := scope.serviceGet(desc.Service)
	if !found {
		return nil
	}
	scope, name, service := healthSource(scope, desc.Service, service)
	instance, built := builtInstanceOf(service)
	if !built || !trackableInstance(instance) {
		return nil
	}
	kinds := slices.SortedFunc(slices.Values(tracked[instance]), compareStepKinds)
	if len(kinds) == 0 {
		return nil
	}

	wrapper, _ := service.(serviceWrapperAny)
	candidate := &startCandidate{scope: scope, service: wrapper, instance: instance}
	candidate.name = name

	s.startedMu.Lock()
	defer s.startedMu.Unlock()

	for _, kind := range kinds {
		step := startStep{candidate: candidate, op: StartOpStart, kind: kind}
		if kind != nil {
			step.op = StartOpHandle
		}
		// a step this container recorded is started here, whatever runs
		// elsewhere on its instance
		if !s.recordedLocked(step) {
			return step.stillRunningError()
		}
	}

	return nil
}
