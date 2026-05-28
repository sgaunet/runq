package main

import (
	"github.com/sgaunet/runq/internal/ipc"
	"github.com/sgaunet/runq/internal/runner"
)

// ipcAdapter exposes the *Runner via the ipc.Handler interface without
// requiring the runner package to import ipc method names verbatim.
type ipcAdapter struct {
	r *runner.Runner
}

func (a ipcAdapter) Snapshot() ipc.RunnerInfo { return a.r.Snapshot() }
func (a ipcAdapter) Submit(items []ipc.SubmitItem) ([]string, error) {
	return a.r.SubmitForwarded(items)
}
func (a ipcAdapter) Stop() { a.r.Close() }
