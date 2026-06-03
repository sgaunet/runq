package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sgaunet/runq/internal/exec"
	"github.com/sgaunet/runq/internal/logwriter"
	"github.com/sgaunet/runq/internal/ui"
)

// ErrQueueFull is returned by Submit when the pending-queue cap is reached.
var ErrQueueFull = errors.New("queue full")

// Options configures a Run.
type Options struct {
	Parallelism    int
	QueueCap       int
	Shell          bool
	DefaultTimeout time.Duration
	KillGrace      time.Duration

	Sink ui.Sink
	Log  *logwriter.Run
}

// Runner orchestrates command execution. Construct one via New, populate
// the initial queue with Submit, then call Run.
type Runner struct {
	opts Options
	ids  idGen

	mu       sync.Mutex
	pending  []*Command
	running  map[string]*Command
	finished []*Command
	closed   bool

	wake      chan struct{}
	once      sync.Once
	closeC    chan struct{}
	logErrors atomic.Int64 // counts per-command log open/finish failures
}

// New constructs a Runner with the given options.
func New(opts Options) *Runner {
	if opts.Parallelism <= 0 {
		opts.Parallelism = 10
	}
	if opts.QueueCap <= 0 {
		opts.QueueCap = 50
	}
	if opts.KillGrace <= 0 {
		opts.KillGrace = 5 * time.Second
	}
	return &Runner{
		opts:    opts,
		running: map[string]*Command{},
		wake:    make(chan struct{}, 1),
		closeC:  make(chan struct{}),
	}
}

// Submit enqueues commands. It assigns each command a new id and returns
// the assigned ids in the same order. If accepting all of them would
// exceed QueueCap, Submit enqueues as many as fit and returns
// ErrQueueFull along with the ids of the accepted subset.
func (r *Runner) Submit(items []Spec) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, errors.New("runner closed")
	}
	accepted := make([]string, 0, len(items))
	now := time.Now()
	for i := range items {
		if len(r.pending) >= r.opts.QueueCap {
			r.notify()
			return accepted, ErrQueueFull
		}
		c := &Command{
			Text:    items[i].Text,
			Source:  items[i].Source,
			Timeout: items[i].Timeout,
		}
		c.ID = r.ids.next()
		c.setState(StateQueued, now)
		r.pending = append(r.pending, c)
		accepted = append(accepted, c.ID)
		if r.opts.Sink != nil {
			r.opts.Sink.OnQueued(c.ID, c.Text)
		}
	}
	r.notify()
	return accepted, nil
}

// notify wakes the scheduler if it's waiting. Non-blocking.
func (r *Runner) notify() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// Close signals that no more submissions will arrive. After Run finishes
// processing the existing queue and running set, it returns.
func (r *Runner) Close() {
	r.once.Do(func() {
		r.mu.Lock()
		r.closed = true
		r.mu.Unlock()
		close(r.closeC)
		r.notify()
	})
}

// Run blocks, draining the pending queue and running set, and returns the
// final counts. It respects ctx for cancellation.
//
// Run returns when:
//   - ctx is cancelled (in-flight commands are signaled, the function waits
//     for them to terminate, then returns); or
//   - Close has been called AND both pending and running are empty.
func (r *Runner) Run(ctx context.Context) Counts {
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, r.opts.Parallelism)

	for {
		// Try to start as many as possible.
		for {
			c := r.takeNext()
			if c == nil {
				break
			}
			select {
			case semaphore <- struct{}{}:
			case <-ctx.Done():
				// Put it back so it ends up in finished with cancelled state.
				r.cancelCommand(c)
				continue
			}
			wg.Add(1)
			go func(c *Command) {
				defer wg.Done()
				defer func() { <-semaphore }()
				r.runOne(ctx, c)
				r.notify()
			}(c)
		}

		// Termination conditions (FR-021):
		// - Per spec, the runner exits when both pending and running are
		//   empty. A forwarder arriving exactly at that moment loses
		//   the race and sees no listener (code 14 on its side).
		// - On context cancellation, we wait for in-flight to finish
		//   then exit.
		r.mu.Lock()
		done := (len(r.pending) == 0 && len(r.running) == 0) ||
			(ctx.Err() != nil && len(r.running) == 0)
		r.mu.Unlock()
		if done {
			break
		}

		select {
		case <-r.wake:
		case <-ctx.Done():
		case <-r.closeC:
		}
	}

	wg.Wait()
	return r.counts()
}

// takeNext pops the next pending command, marks it Running, and returns
// it. Returns nil if there is no pending work.
func (r *Runner) takeNext() *Command {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.pending) == 0 {
		return nil
	}
	c := r.pending[0]
	r.pending = r.pending[1:]
	r.running[c.ID] = c
	return c
}

// cancelCommand records a queued command as cancelled (used when the
// context fires before the command had a chance to start).
func (r *Runner) cancelCommand(c *Command) {
	now := time.Now()
	c.setState(StateCancelled, now)
	c.exitCode.Store(-1)
	r.mu.Lock()
	r.finished = append(r.finished, c)
	r.mu.Unlock()
	if r.opts.Sink != nil {
		r.opts.Sink.OnCancelled(c.ID, c.Text, 0)
	}
}

// runOne actually executes one command, capturing output, writing the log
// record, and firing UI hooks.
func (r *Runner) runOne(ctx context.Context, c *Command) {
	startedAt := time.Now()
	c.setState(StateRunning, startedAt)
	if r.opts.Sink != nil {
		r.opts.Sink.OnStart(c.ID, c.Text)
	}

	spec := exec.Spec{
		Text:      c.Text,
		Shell:     r.opts.Shell,
		Timeout:   c.Timeout,
		KillGrace: r.opts.KillGrace,
	}
	if spec.Timeout == 0 {
		spec.Timeout = r.opts.DefaultTimeout
	}
	if !r.opts.Shell {
		// Argv mode: split on whitespace. No shell quoting. This is the
		// documented v1 behavior — operators who need quoting use shell
		// mode (the default). See contracts/cli.md.
		spec.Argv = strings.Fields(c.Text)
	}

	// Open this command's own log file and stream its output straight to
	// disk (FR-012: no in-memory buffering). One file per command means no
	// cross-command interleaving and no shared write lock.
	//
	// On NewRecord failure: do not discard output silently (FR-015). Fall back
	// to stderr so the command's output is visible. Increment logErrors so the
	// run exits non-zero with exitcode.LogWriteFailed.
	var rec *logwriter.Record
	out := io.Discard
	if r.opts.Log != nil {
		var err error
		rec, err = r.opts.Log.NewRecord(c.ID, c.Text, string(c.Source), startedAt)
		if err != nil {
			fmt.Fprintf(os.Stderr, "runq: cannot open log for %q: %v\n", c.Text, err)
			r.logErrors.Add(1)
			out = os.Stderr // surface output rather than silently discard (FR-015)
		} else {
			out = rec
		}
	}

	// Finalize the log record exactly once, even on panic. Named locals are
	// assigned after exec.Run so the deferred closure captures their final values.
	var (
		endedAt   time.Time
		exitField string
		dur       time.Duration
	)
	defer func() {
		if rec != nil {
			if err := rec.Finish(endedAt, exitField, dur); err != nil {
				fmt.Fprintf(os.Stderr, "runq: %v\n", err)
				r.logErrors.Add(1)
			}
		}
	}()

	res := exec.Run(ctx, spec, out)

	endedAt = time.Now()
	dur = endedAt.Sub(startedAt)

	// Map exec.Result → State + exit code field.
	var finalState State
	switch res.Reason {
	case "ok":
		finalState = StateSucceeded
		exitField = fmt.Sprintf("%d", res.ExitCode)
	case "failed":
		finalState = StateFailed
		exitField = fmt.Sprintf("%d", res.ExitCode)
	case "cancelled":
		finalState = StateCancelled
		exitField = "cancelled"
	case "timed-out":
		finalState = StateTimedOut
		exitField = "timed-out"
	case "spawn-error":
		finalState = StateSpawnError
		exitField = "spawn-error"
	default:
		// signal-N
		finalState = StateFailed
		exitField = res.Reason
	}

	c.setState(finalState, endedAt)
	c.exitCode.Store(int32(res.ExitCode)) // #nosec G115 -- OS exit codes fit int32

	// Fire UI hook.
	if r.opts.Sink != nil {
		switch finalState {
		case StateSucceeded:
			r.opts.Sink.OnSuccess(c.ID, c.Text, res.ExitCode, dur)
		case StateFailed:
			r.opts.Sink.OnFailure(c.ID, c.Text, res.ExitCode, dur)
		case StateCancelled:
			r.opts.Sink.OnCancelled(c.ID, c.Text, dur)
		case StateTimedOut:
			r.opts.Sink.OnTimedOut(c.ID, c.Text, dur)
		case StateSpawnError:
			r.opts.Sink.OnSpawnError(c.ID, c.Text, res.Err)
		}
	}

	// Move from running to finished.
	r.mu.Lock()
	delete(r.running, c.ID)
	r.finished = append(r.finished, c)
	r.mu.Unlock()
}

// counts produces the run summary.
func (r *Runner) counts() Counts {
	r.mu.Lock()
	defer r.mu.Unlock()
	var counts Counts
	counts.Total = len(r.finished)
	for _, c := range r.finished {
		switch c.State() {
		case StateSucceeded:
			counts.Succeeded++
		case StateFailed:
			counts.Failed++
		case StateCancelled:
			counts.Cancelled++
		case StateTimedOut:
			counts.TimedOut++
		case StateSpawnError:
			counts.SpawnErrors++
		}
	}
	counts.LogErrors = int(r.logErrors.Load())
	return counts
}

// Finished returns a snapshot of completed commands. Safe to call after
// Run returns.
func (r *Runner) Finished() []*Command {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*Command, len(r.finished))
	copy(out, r.finished)
	return out
}
