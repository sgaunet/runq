package main

import (
	"github.com/sgaunet/runq/internal/ipc"
	"github.com/sgaunet/runq/internal/runner"
)

// ipcAdapter exposes the *Runner via the ipc.Handler interface without
// requiring the runner package to import ipc method names verbatim.
//
// onStop is invoked when a `runq stop` request is received. The implicit
// runner wires it to Runner.Close (drain and exit); `serve` wires it to a
// run-context cancel so `stop` triggers the same graceful shutdown as
// Ctrl+C (FR-011a).
type ipcAdapter struct {
	r      *runner.Runner
	onStop func()
}

func (a ipcAdapter) Snapshot() ipc.RunnerInfo { return a.r.Snapshot() }
func (a ipcAdapter) Submit(items []ipc.SubmitItem) ([]string, error) {
	return a.r.SubmitForwarded(items)
}
func (a ipcAdapter) Stop() { a.onStop() }
