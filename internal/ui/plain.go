package ui

import (
	"fmt"
	"io"
	"sync"
	"time"
)

// Plain emits aligned, human-readable status lines to a writer (typically
// os.Stderr). It is used when the destination is not a TTY, when --quiet is off
// but the bullets UI isn't appropriate, or as a unit-testable fallback. Line
// formatting is delegated to Layout so the plain and bullets sinks stay
// consistent.
type Plain struct {
	mu sync.Mutex
	w  io.Writer
	l  Layout
}

// NewPlain constructs a plain-text Sink writing to w with the given Layout.
func NewPlain(w io.Writer, l Layout) *Plain { return &Plain{w: w, l: l} }

func (p *Plain) writeln(line string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.w == nil {
		return
	}
	_, _ = fmt.Fprintln(p.w, line)
}

// OnQueued writes a QUEUED status line for the command.
func (p *Plain) OnQueued(id, text string) {
	p.writeln(p.l.Render(id, StatusQueued, text, nil, nil, ""))
}

// OnStart writes a STARTED status line for the command.
func (p *Plain) OnStart(id, text string) {
	p.writeln(p.l.Render(id, StatusStarted, text, nil, nil, ""))
}

// OnSuccess writes an OK status line with the command's exit code and duration.
func (p *Plain) OnSuccess(id, text string, exitCode int, dur time.Duration) {
	p.writeln(p.l.Render(id, StatusSuccess, text, &exitCode, &dur, ""))
}

// OnFailure writes a FAILED status line with the command's exit code and duration.
func (p *Plain) OnFailure(id, text string, exitCode int, dur time.Duration) {
	p.writeln(p.l.Render(id, StatusFailure, text, &exitCode, &dur, ""))
}

// OnCancelled writes a CANCEL status line with the command's duration.
func (p *Plain) OnCancelled(id, text string, dur time.Duration) {
	p.writeln(p.l.Render(id, StatusCancelled, text, nil, &dur, ""))
}

// OnTimedOut writes a TIMEOUT status line with the command's duration.
func (p *Plain) OnTimedOut(id, text string, dur time.Duration) {
	p.writeln(p.l.Render(id, StatusTimedOut, text, nil, &dur, ""))
}

// OnSpawnError writes a SPAWN-ERR status line with the spawn error as a
// trailing, bounded note (the exit and duration zones stay aligned).
func (p *Plain) OnSpawnError(id, text string, err error) {
	p.writeln(p.l.Render(id, StatusSpawnError, text, nil, nil, "err="+err.Error()))
}

// Close releases UI resources; the plain Sink holds none, so it is a no-op.
func (p *Plain) Close() error { return nil }
