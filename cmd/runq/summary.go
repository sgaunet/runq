package main

import (
	"fmt"
	"io"

	"github.com/sgaunet/runq/internal/config"
	"github.com/sgaunet/runq/internal/runner"
)

// writeSummary emits the run summary on out (stdout). It is silent when
// cfg.Quiet is set AND the output format is text (JSON is always emitted
// when requested, even with --quiet, so machine consumers can rely on
// it).
func writeSummary(out io.Writer, cfg config.Config, c runner.Counts, r *runner.Runner) error {
	switch cfg.OutputFormat {
	case config.OutputJSON:
		summary := runner.BuildJSONSummary(r, runner.RunInfo{
			LogDir:     cfg.LogDir,
			SocketPath: cfg.SocketPath,
		})
		return runner.EncodeJSONSummary(out, summary)
	default:
		if cfg.Quiet {
			return nil
		}
		_, err := fmt.Fprintf(out,
			"runq: %d ok, %d failed, %d timed-out, %d cancelled, %d spawn-errors (total %d)\n",
			c.Succeeded, c.Failed, c.TimedOut, c.Cancelled, c.SpawnErrors, c.Total)
		return err
	}
}
