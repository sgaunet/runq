package runner

import (
	"errors"
	"os"
	"time"

	"github.com/sgaunet/runq/internal/ipc"
)

// SubmitForwarded converts ipc.SubmitItem values to Specs (parsing
// optional timeouts), enqueues them, and returns the assigned ids. On
// queue overflow it returns the accepted prefix and ipc.ErrQueueFull so
// the IPC server can translate it to wire code QUEUE_FULL.
func (r *Runner) SubmitForwarded(items []ipc.SubmitItem) ([]string, error) {
	specs := make([]Spec, 0, len(items))
	for _, it := range items {
		s := Spec{Text: it.Text, Source: SourceSocket}
		if it.Timeout != "" {
			d, err := time.ParseDuration(it.Timeout)
			if err != nil {
				return nil, err
			}
			s.Timeout = d
		}
		specs = append(specs, s)
	}
	ids, err := r.Submit(specs)
	if err != nil {
		if errors.Is(err, ErrQueueFull) {
			return ids, ipc.ErrQueueFull
		}
		return ids, err
	}
	return ids, nil
}

// Snapshot returns the lightweight runner status used by the ipc
// hello-ack.
func (r *Runner) Snapshot() ipc.RunnerInfo {
	r.mu.Lock()
	pending := len(r.pending)
	running := len(r.running)
	r.mu.Unlock()
	return ipc.RunnerInfo{
		PID:           os.Getpid(),
		StartedAt:     time.Now().UTC().Format(time.RFC3339Nano),
		Parallelism:   r.opts.Parallelism,
		QueueCapacity: r.opts.QueueCap,
		QueuePending:  pending,
		Running:       running,
	}
}
