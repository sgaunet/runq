package runner_test

import (
	"context"
	"errors"
	"path/filepath"
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
	lw, _, err := logwriter.Open(filepath.Join(dir, "log.log"))
	if err != nil {
		t.Fatalf("Open log: %v", err)
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
	lw, _, err := logwriter.Open(filepath.Join(dir, "log.log"))
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
