package doger

import "fmt"

// ContainerPhase is where a root container is in its lifecycle. Its zero
// value is PhaseIdle.
type ContainerPhase int

const (
	// PhaseIdle is a container that has not started nor shut down.
	PhaseIdle ContainerPhase = iota
	// PhaseStarting is a container whose first Start is running.
	PhaseStarting
	// PhaseStarted is a container whose Start succeeded, while its Started
	// hooks run.
	PhaseStarted
	// PhaseReady is a container whose Start succeeded and whose Started
	// hooks have returned.
	PhaseReady
	// PhaseFailed is a container whose first Start failed.
	PhaseFailed
	// PhaseClosing is a container whose root Shutdown is running.
	PhaseClosing
	// PhaseClosed is a container whose root Shutdown completed.
	PhaseClosed
)

// String returns the lower-case phase name, or "ContainerPhase(n)" for an
// unknown value.
func (p ContainerPhase) String() string {
	if name, ok := enumText(p, containerPhaseNames); ok {
		return name
	}

	return fmt.Sprintf("ContainerPhase(%d)", int(p))
}

// containerPhaseNames are the names of the ContainerPhase constants, by
// value.
var containerPhaseNames = []string{"idle", "starting", "started", "ready", "failed", "closing", "closed"}

// MarshalText encodes the phase as its String form, so JSON shows it by
// name. A value outside the declared constants fails.
func (p ContainerPhase) MarshalText() ([]byte, error) {
	name, ok := enumText(p, containerPhaseNames)
	if !ok {
		return nil, libErrorf("invalid container phase %d", int(p))
	}

	return []byte(name), nil
}

// UnmarshalText decodes the String form of a phase. Any other text fails
// and leaves p unchanged.
func (p *ContainerPhase) UnmarshalText(text []byte) error {
	value, ok := enumValue[ContainerPhase](text, containerPhaseNames)
	if !ok {
		return libErrorf("unknown container phase %q", text)
	}
	*p = value

	return nil
}

// Phase returns the container's current phase. Once root Shutdown began,
// the close phase is reported, whatever the start phase. A clone starts
// idle; child scopes have no phase of their own.
func (s *RootScope) Phase() ContainerPhase {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()

	switch s.closePhase {
	case closeClosing:
		return PhaseClosing
	case closeClosed:
		return PhaseClosed
	case closeOpen:
	}

	switch s.startPhase {
	case startStarting:
		return PhaseStarting
	case startStarted:
		return PhaseStarted
	case startReady:
		return PhaseReady
	case startFailed:
		return PhaseFailed
	case startIdle:
	}

	return PhaseIdle
}
