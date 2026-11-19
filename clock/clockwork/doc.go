// Package dogerclockwork adapts github.com/jonboulle/clockwork clocks to
// doger.Clock, so a container can run on a controllable clock in tests: Wrap
// turns a clockwork.Clock into a doger.Clock, and Unwrap recovers it.
package dogerclockwork
