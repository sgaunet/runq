// Package runner orchestrates the parallel execution of commands.
package runner

import (
	"sync/atomic"
	"time"
)

// State is the current position in a Command's lifecycle. See
// specs/001-parallel-cmd-runner/data-model.md.
type State int32

// State constants enumerate the lifecycle positions a Command can occupy,
// from initial queueing through running to a terminal outcome.
const (
	StateQueued State = iota
	StateRunning
	StateSucceeded
	StateFailed
	StateCancelled
	StateTimedOut
	StateSpawnError
)

func (s State) String() string {
	switch s {
	case StateQueued:
		return "queued"
	case StateRunning:
		return "running"
	case StateSucceeded:
		return "succeeded"
	case StateFailed:
		return "failed"
	case StateCancelled:
		return "cancelled"
	case StateTimedOut:
		return "timed-out"
	case StateSpawnError:
		return "spawn-error"
	default:
		return "unknown"
	}
}

// IsTerminal reports whether the state is final.
func (s State) IsTerminal() bool {
	switch s {
	case StateSucceeded, StateFailed, StateCancelled, StateTimedOut, StateSpawnError:
		return true
	default:
		return false
	}
}

// Source describes where a Command originated.
type Source string

// Source constants enumerate the origins from which a Command may be
// submitted to the Runner.
const (
	SourceCLI    Source = "cli"
	SourceFile   Source = "file"
	SourceStdin  Source = "stdin"
	SourceSocket Source = "socket"
)

// Spec is the value-typed input form for Submit. It carries the immutable
// fields needed to construct a Command and is safe to copy / append.
type Spec struct {
	Text    string
	Source  Source
	Timeout time.Duration
}

// Command is a single unit of work. Once a Command is handed to the
// Runner, its fields are mutated only by the Runner's goroutines; external
// code observes them via the accessor methods, which use atomics for
// State/ExitCode and an in-Runner mutex for time fields. Commands are
// passed around by pointer to avoid copying.
type Command struct {
	ID      string
	Text    string
	Source  Source
	Timeout time.Duration

	state    atomic.Int32
	exitCode atomic.Int32

	// time fields are written by the Runner under its own mutex; external
	// reads should call Times() which performs a stable copy.
	submitted time.Time
	started   time.Time
	ended     time.Time
}

// State returns the current state.
func (c *Command) State() State { return State(c.state.Load()) }

// ExitCode returns the recorded exit code (or 0 if not yet terminal).
func (c *Command) ExitCode() int { return int(c.exitCode.Load()) }

// Times returns submitted/started/ended snapshots. The caller is
// responsible for invoking this only after the command reached the desired
// state (e.g., after Run returns).
func (c *Command) Times() (submitted, started, ended time.Time) {
	return c.submitted, c.started, c.ended
}

// Duration returns the elapsed time between started and ended; zero if
// either is unset.
func (c *Command) Duration() time.Duration {
	if c.started.IsZero() || c.ended.IsZero() {
		return 0
	}
	return c.ended.Sub(c.started)
}

// setState is called by the Runner; not safe for concurrent calls on the
// same Command (the Runner serializes per-Command transitions).
func (c *Command) setState(s State, now time.Time) {
	c.state.Store(int32(s))
	switch s {
	case StateQueued:
		c.submitted = now
	case StateRunning:
		c.started = now
	default:
		if s.IsTerminal() {
			c.ended = now
		}
	}
}

// Counts is the run summary.
type Counts struct {
	Total       int `json:"total"`
	Succeeded   int `json:"succeeded"`
	Failed      int `json:"failed"`
	Cancelled   int `json:"cancelled"`
	TimedOut    int `json:"timed_out"`
	SpawnErrors int `json:"spawn_errors"`
	LogErrors   int `json:"log_errors"`
}

// idGen produces stable per-runner identifiers of the form c-NNNN.
type idGen struct {
	n atomic.Uint64
}

func (g *idGen) next() string {
	n := g.n.Add(1)
	return formatID(n)
}

func formatID(n uint64) string {
	if n < 10000 {
		const base = "0000"
		s := uintToASCII(n)
		if len(s) >= len(base) {
			return "c-" + s
		}
		return "c-" + base[:len(base)-len(s)] + s
	}
	return "c-" + uintToASCII(n)
}

func uintToASCII(n uint64) string {
	if n == 0 {
		return "0"
	}
	buf := make([]byte, 0, 20)
	for n > 0 {
		buf = append(buf, byte('0'+n%10))
		n /= 10
	}
	for i, j := 0, len(buf)-1; i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}
	return string(buf)
}
