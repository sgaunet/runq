package ui

import (
	"fmt"
	"io"
	"sync"
	"time"
)

// Plain emits human-readable status lines to a writer (typically os.Stderr).
// It is used when the destination is not a TTY, when --quiet is off but
// the bullets UI isn't appropriate, or as a unit-testable fallback.
type Plain struct {
	mu sync.Mutex
	w  io.Writer
}

// NewPlain constructs a plain-text Sink writing to w.
func NewPlain(w io.Writer) *Plain { return &Plain{w: w} }

func (p *Plain) writeln(format string, args ...any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.w == nil {
		return
	}
	_, _ = fmt.Fprintln(p.w, fmt.Sprintf(format, args...))
}

// OnQueued writes a QUEUED status line for the command.
func (p *Plain) OnQueued(id, text string) {
	p.writeln("runq: %s QUEUED   %s", id, truncate(text, 80))
}

// OnStart writes a STARTED status line for the command.
func (p *Plain) OnStart(id, text string) {
	p.writeln("runq: %s STARTED  %s", id, truncate(text, 80))
}

// OnSuccess writes an OK status line with the command's exit code and duration.
func (p *Plain) OnSuccess(id, text string, exitCode int, dur time.Duration) {
	p.writeln("runq: %s OK       %s · exit=%d · dur=%s", id, truncate(text, 60), exitCode, dur.Round(time.Millisecond))
}

// OnFailure writes a FAILED status line with the command's exit code and duration.
func (p *Plain) OnFailure(id, text string, exitCode int, dur time.Duration) {
	p.writeln("runq: %s FAILED   %s · exit=%d · dur=%s", id, truncate(text, 60), exitCode, dur.Round(time.Millisecond))
}

// OnCancelled writes a CANCEL status line with the command's duration.
func (p *Plain) OnCancelled(id, text string, dur time.Duration) {
	p.writeln("runq: %s CANCEL   %s · dur=%s", id, truncate(text, 60), dur.Round(time.Millisecond))
}

// OnTimedOut writes a TIMEOUT status line with the command's duration.
func (p *Plain) OnTimedOut(id, text string, dur time.Duration) {
	p.writeln("runq: %s TIMEOUT  %s · dur=%s", id, truncate(text, 60), dur.Round(time.Millisecond))
}

// OnSpawnError writes a SPAWN-ERR status line with the spawn error.
func (p *Plain) OnSpawnError(id, text string, err error) {
	p.writeln("runq: %s SPAWN-ERR %s · err=%v", id, truncate(text, 60), err)
}

// Close releases UI resources; the plain Sink holds none, so it is a no-op.
func (p *Plain) Close() error { return nil }

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n < 4 {
		return s[:n]
	}
	return s[:n-1] + "…"
}
