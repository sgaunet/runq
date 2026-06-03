package runner_test

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sgaunet/runq/internal/logwriter"
	"github.com/sgaunet/runq/internal/runner"
	"github.com/sgaunet/runq/internal/ui"
)

func newTestRunner(t *testing.T, parallel, queueCap int) (*runner.Runner, func()) {
	t.Helper()
	dir := t.TempDir()
	lw, err := logwriter.OpenRun(dir, time.Unix(0, 0))
	if err != nil {
		t.Fatalf("OpenRun: %v", err)
	}
	r := runner.New(runner.Options{
		Parallelism: parallel,
		QueueCap:    queueCap,
		Shell:       true,
		KillGrace:   time.Second,
		Sink:        ui.Quiet{},
		Log:         lw,
	})
	return r, func() { _ = lw.Close() }
}

func TestRunner_SuccessAndFailureCounts(t *testing.T) {
	r, cleanup := newTestRunner(t, 4, 50)
	defer cleanup()

	items := []runner.Spec{
		{Text: "true", Source: runner.SourceCLI},
		{Text: "true", Source: runner.SourceCLI},
		{Text: "false", Source: runner.SourceCLI},
		{Text: "exit 7", Source: runner.SourceCLI},
	}
	if _, err := r.Submit(items); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	r.Close()
	counts := r.Run(context.Background())
	if counts.Total != 4 {
		t.Errorf("Total = %d, want 4", counts.Total)
	}
	if counts.Succeeded != 2 {
		t.Errorf("Succeeded = %d, want 2", counts.Succeeded)
	}
	if counts.Failed != 2 {
		t.Errorf("Failed = %d, want 2", counts.Failed)
	}
}

func TestRunner_QueueCapEnforced(t *testing.T) {
	r, cleanup := newTestRunner(t, 1, 2)
	defer cleanup()

	// 5 items; cap is 2 → expect first 2 to be accepted then ErrQueueFull.
	items := []runner.Spec{
		{Text: "true"}, {Text: "true"}, {Text: "true"}, {Text: "true"}, {Text: "true"},
	}
	accepted, err := r.Submit(items)
	if !errors.Is(err, runner.ErrQueueFull) {
		t.Fatalf("Submit err = %v, want ErrQueueFull", err)
	}
	if len(accepted) != 2 {
		t.Errorf("accepted %d, want 2", len(accepted))
	}
	r.Close()
	r.Run(context.Background())
}

// TestRunner_LingerStaysAliveUntilCancel verifies FR-004: with Linger set,
// Run does NOT return when the queue drains; it keeps waiting until the
// context is cancelled.
func TestRunner_LingerStaysAliveUntilCancel(t *testing.T) {
	dir := t.TempDir()
	lw, err := logwriter.OpenRun(dir, time.Unix(0, 0))
	if err != nil {
		t.Fatalf("OpenRun: %v", err)
	}
	defer func() { _ = lw.Close() }()
	r := runner.New(runner.Options{
		Parallelism: 2, QueueCap: 50, Shell: true, KillGrace: time.Second,
		Linger: true, Sink: ui.Quiet{}, Log: lw,
	})
	if _, err := r.Submit([]runner.Spec{{Text: "true", Source: runner.SourceCLI}}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	doneC := make(chan runner.Counts, 1)
	go func() { doneC <- r.Run(ctx) }()

	// The single command drains quickly; a lingering Run must keep waiting.
	select {
	case <-doneC:
		t.Fatal("lingering Run returned on drain; want it to keep waiting")
	case <-time.After(300 * time.Millisecond):
	}
	if n := r.InFlight(); n != 0 {
		t.Errorf("InFlight = %d, want 0 after drain", n)
	}

	cancel()
	select {
	case counts := <-doneC:
		if counts.Total != 1 || counts.Succeeded != 1 {
			t.Errorf("counts = %+v, want 1 total/1 succeeded", counts)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("lingering Run did not return after ctx cancel")
	}
}

// TestRunner_NonLingerExitsOnDrainWithoutClose is the regression guard: the
// implicit runner (Linger=false) exits the moment its queue drains, even
// without Close (FR-021).
func TestRunner_NonLingerExitsOnDrainWithoutClose(t *testing.T) {
	r, cleanup := newTestRunner(t, 2, 50)
	defer cleanup()
	if _, err := r.Submit([]runner.Spec{{Text: "true", Source: runner.SourceCLI}}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	doneC := make(chan runner.Counts, 1)
	go func() { doneC <- r.Run(context.Background()) }()
	select {
	case counts := <-doneC:
		if counts.Succeeded != 1 {
			t.Errorf("counts = %+v, want 1 succeeded", counts)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("non-linger Run did not exit on drain without Close")
	}
}

// TestRunner_InFlight checks the pending+running accounting used by serve to
// choose its exit code.
func TestRunner_InFlight(t *testing.T) {
	r, cleanup := newTestRunner(t, 1, 50)
	defer cleanup()
	if n := r.InFlight(); n != 0 {
		t.Errorf("InFlight = %d, want 0 initially", n)
	}
	if _, err := r.Submit([]runner.Spec{
		{Text: "true"}, {Text: "true"}, {Text: "true"},
	}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if n := r.InFlight(); n != 3 {
		t.Errorf("InFlight = %d, want 3 after submit (all pending)", n)
	}
}

// countingSink counts the maximum number of concurrently running commands
// observed. It satisfies the ui.Sink interface.
type countingSink struct {
	active atomic.Int32
	maxObs atomic.Int32
}

func (c *countingSink) OnQueued(_, _ string) {}
func (c *countingSink) OnStart(_, _ string) {
	n := c.active.Add(1)
	for {
		curMax := c.maxObs.Load()
		if n <= curMax || c.maxObs.CompareAndSwap(curMax, n) {
			break
		}
	}
}
func (c *countingSink) OnSuccess(_, _ string, _ int, _ time.Duration) { c.active.Add(-1) }
func (c *countingSink) OnFailure(_, _ string, _ int, _ time.Duration) { c.active.Add(-1) }
func (c *countingSink) OnCancelled(_, _ string, _ time.Duration)      { c.active.Add(-1) }
func (c *countingSink) OnTimedOut(_, _ string, _ time.Duration)       { c.active.Add(-1) }
func (c *countingSink) OnSpawnError(_, _ string, _ error)             { c.active.Add(-1) }
func (c *countingSink) Close() error                                  { return nil }

func TestRunner_ParallelismCapNotExceeded(t *testing.T) {
	dir := t.TempDir()
	lw, err := logwriter.OpenRun(dir, time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	defer lw.Close()
	const parallel = 3
	sink := &countingSink{}
	r := runner.New(runner.Options{
		Parallelism: parallel, QueueCap: 50, Shell: true,
		KillGrace: time.Second, Sink: sink, Log: lw,
	})

	items := make([]runner.Spec, 12)
	for i := range items {
		items[i] = runner.Spec{Text: "sleep 0.2", Source: runner.SourceCLI}
	}
	if _, err := r.Submit(items); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	r.Close()
	r.Run(context.Background())
	got := int(sink.maxObs.Load())
	if got > parallel {
		t.Errorf("max concurrent = %d, want <= %d", got, parallel)
	}
	if got == 0 {
		t.Errorf("max concurrent = 0; sink not invoked?")
	}
}

func TestRunner_AssignsSequentialIDs(t *testing.T) {
	r, cleanup := newTestRunner(t, 1, 10)
	defer cleanup()
	items := []runner.Spec{{Text: "true"}, {Text: "true"}, {Text: "true"}}
	accepted, err := r.Submit(items)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"c-0001", "c-0002", "c-0003"}
	for i, id := range want {
		if accepted[i] != id {
			t.Errorf("accepted[%d] = %q, want %q", i, accepted[i], id)
		}
	}
	r.Close()
	r.Run(context.Background())
}

// TestRunner_LogErrors_CountedOnNewRecordFailure verifies that when the log
// directory is removed mid-run (causing NewRecord to fail), Counts.LogErrors
// is incremented and the command's output is not silently discarded.
func TestRunner_LogErrors_CountedOnNewRecordFailure(t *testing.T) {
	dir := t.TempDir()
	lw, err := logwriter.OpenRun(dir, time.Unix(0, 0))
	if err != nil {
		t.Fatalf("OpenRun: %v", err)
	}
	// Remove the run directory so NewRecord cannot create files in it.
	if err := os.RemoveAll(lw.Dir()); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	defer func() { _ = lw.Close() }()

	r := runner.New(runner.Options{
		Parallelism: 2,
		QueueCap:    10,
		Shell:       true,
		KillGrace:   time.Second,
		Sink:        ui.Quiet{},
		Log:         lw,
	})
	if _, err := r.Submit([]runner.Spec{
		{Text: "true", Source: runner.SourceCLI},
		{Text: "true", Source: runner.SourceCLI},
	}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	r.Close()
	counts := r.Run(context.Background())
	if counts.LogErrors == 0 {
		t.Errorf("LogErrors = 0, want > 0 after NewRecord failure")
	}
	if counts.Total != 2 {
		t.Errorf("Total = %d, want 2 (commands must still run)", counts.Total)
	}
}
