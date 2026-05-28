// Package ui provides the per-command status surface for the runner.
package ui

import "time"

// Sink receives lifecycle events for each command. Implementations must be
// safe for concurrent use; the runner fires events from multiple
// goroutines.
type Sink interface {
	OnQueued(id, text string)
	OnStart(id, text string)
	OnSuccess(id, text string, exitCode int, dur time.Duration)
	OnFailure(id, text string, exitCode int, dur time.Duration)
	OnCancelled(id, text string, dur time.Duration)
	OnTimedOut(id, text string, dur time.Duration)
	OnSpawnError(id, text string, err error)

	// Close releases any UI resources (line tracking, animation
	// timers). Implementations must tolerate multiple calls.
	Close() error
}

// Quiet is a no-op Sink for --quiet.
type Quiet struct{}

// OnQueued is a no-op; Quiet emits no output.
func (Quiet) OnQueued(string, string) {}

// OnStart is a no-op; Quiet emits no output.
func (Quiet) OnStart(string, string) {}

// OnSuccess is a no-op; Quiet emits no output.
func (Quiet) OnSuccess(string, string, int, time.Duration) {}

// OnFailure is a no-op; Quiet emits no output.
func (Quiet) OnFailure(string, string, int, time.Duration) {}

// OnCancelled is a no-op; Quiet emits no output.
func (Quiet) OnCancelled(string, string, time.Duration) {}

// OnTimedOut is a no-op; Quiet emits no output.
func (Quiet) OnTimedOut(string, string, time.Duration) {}

// OnSpawnError is a no-op; Quiet emits no output.
func (Quiet) OnSpawnError(string, string, error) {}

// Close is a no-op; Quiet holds no resources.
func (Quiet) Close() error { return nil }
