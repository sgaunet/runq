// Package exec runs a single child command with full context-aware
// cancellation, a per-command timeout, and process-group kill semantics so
// no children are orphaned.
package exec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync/atomic"
	"syscall"
	"time"
)

// Spec describes how to run one command.
type Spec struct {
	// Text is the command to run. In shell mode it is handed to
	// "/bin/sh -c". In argv mode it is parsed as a Go-style argv (the
	// caller is responsible for splitting).
	Text string

	// Argv is the parsed argv used when Shell is false. When Shell is
	// true, Argv is ignored.
	Argv []string

	// Shell selects shell mode (default) vs argv mode.
	Shell bool

	// Timeout, when > 0, sets a per-command deadline. The command is
	// cancelled (SIGTERM → SIGKILL after KillGrace) if it exceeds it.
	Timeout time.Duration

	// KillGrace is the wait between SIGTERM and SIGKILL when the context
	// or timeout cancels the command.
	KillGrace time.Duration
}

// Result captures how a command terminated.
type Result struct {
	ExitCode int
	Reason   string // "ok" | "failed" | "cancelled" | "timed-out" | "spawn-error" | "signal-N"
	Err      error  // non-nil for spawn-error
}

// Run executes the command described by s, writing its stdout and stderr to
// out. Returns when the child exits, the context is cancelled, or the
// per-command timeout fires. The caller is responsible for capturing
// output; the same writer is used for both streams so they interleave in
// arrival order (matching what the user would see in a terminal).
//
// Run never panics. It honors ctx cancellation by killing the entire
// process group (Setpgid) — SIGTERM, then SIGKILL after s.KillGrace.
//
// forceCtx is a second, independent cancellation used for forced shutdown
// (e.g. a serve listener's 2nd Ctrl+C): when it is cancelled the process
// group is SIGKILLed immediately, bypassing any remaining s.KillGrace
// window. Pass context.Background() when no forced path is needed (the
// implicit runner does), which leaves the SIGTERM→grace→SIGKILL behavior
// unchanged.
func Run(ctx, forceCtx context.Context, s Spec, out io.Writer) Result {
	// Per-command timeout layered on top of the parent context.
	cmdCtx := ctx
	var cancelTimeout context.CancelFunc
	if s.Timeout > 0 {
		cmdCtx, cancelTimeout = context.WithTimeout(ctx, s.Timeout)
		defer cancelTimeout()
	}

	var cmd *exec.Cmd
	if s.Shell {
		cmd = exec.Command("/bin/sh", "-c", s.Text) //nolint:gosec // G204: runq's purpose is to execute user-provided commands
	} else {
		if len(s.Argv) == 0 {
			return Result{ExitCode: -1, Reason: "spawn-error", Err: errors.New("argv mode requires non-empty argv")}
		}
		cmd = exec.Command(s.Argv[0], s.Argv[1:]...) //nolint:gosec // G204: runq's purpose is to execute user-provided commands
	}

	// Child stdin = /dev/null (FR-023a).
	devNull, err := os.OpenFile(os.DevNull, os.O_RDONLY, 0)
	if err != nil {
		return Result{ExitCode: -1, Reason: "spawn-error", Err: fmt.Errorf("open /dev/null: %w", err)}
	}
	defer func() { _ = devNull.Close() }()
	cmd.Stdin = devNull
	cmd.Stdout = out
	cmd.Stderr = out

	// Put the child in its own process group so we can signal the whole
	// tree on cancel/timeout.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return Result{ExitCode: -1, Reason: "spawn-error", Err: err}
	}

	// Track the process group id for the cancellation goroutine.
	pgid := cmd.Process.Pid

	// Watch for cancellation; on cancel, signal the group.
	done := make(chan struct{})
	watcherDone := make(chan struct{})
	var killTriggered atomic.Bool
	go func() {
		defer close(watcherDone)
		select {
		case <-cmdCtx.Done():
			killTriggered.Store(true)
			killProcessGroup(pgid, syscall.SIGTERM)
			grace := s.KillGrace
			if grace <= 0 {
				grace = 5 * time.Second
			}
			select {
			case <-done:
				return
			case <-forceCtx.Done():
				// Forced shutdown: skip the rest of the grace window.
				killProcessGroup(pgid, syscall.SIGKILL)
			case <-time.After(grace):
				killProcessGroup(pgid, syscall.SIGKILL)
			}
		case <-forceCtx.Done():
			// Forced shutdown before any SIGTERM: kill immediately.
			killTriggered.Store(true)
			killProcessGroup(pgid, syscall.SIGKILL)
		case <-done:
			return
		}
	}()

	waitErr := cmd.Wait()
	close(done)
	<-watcherDone // ensure the goroutine has stopped touching shared state

	// Classify outcome.
	switch {
	case killTriggered.Load() && errors.Is(cmdCtx.Err(), context.DeadlineExceeded):
		return Result{ExitCode: -1, Reason: "timed-out", Err: cmdCtx.Err()}
	case killTriggered.Load():
		return Result{ExitCode: -1, Reason: "cancelled", Err: cmdCtx.Err()}
	}

	if waitErr == nil {
		return Result{ExitCode: 0, Reason: "ok"}
	}

	var ee *exec.ExitError
	if errors.As(waitErr, &ee) {
		// Exit due to signal vs exit code.
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return Result{
				ExitCode: 128 + int(ws.Signal()),
				Reason:   fmt.Sprintf("signal-%d", ws.Signal()),
			}
		}
		return Result{ExitCode: ee.ExitCode(), Reason: "failed"}
	}
	return Result{ExitCode: -1, Reason: "spawn-error", Err: waitErr}
}

func killProcessGroup(pid int, sig syscall.Signal) {
	// Negative pid signals the entire process group. Ignore errors: the
	// child may already be gone.
	_ = syscall.Kill(-pid, sig)
}
