package runner

import (
	"encoding/json"
	"io"
	"time"
)

// CommandSummary is the per-command shape used in the JSON summary.
type CommandSummary struct {
	ID        string `json:"id"`
	Text      string `json:"text"`
	Source    string `json:"source"`
	StartedAt string `json:"started_at,omitempty"`
	EndedAt   string `json:"ended_at,omitempty"`
	Duration  string `json:"duration,omitempty"`
	ExitCode  int    `json:"exit_code"`
	State     string `json:"state"`
}

// RunInfo describes the runner that produced the summary.
type RunInfo struct {
	LogDir     string `json:"log_dir,omitempty"`
	SocketPath string `json:"socket_path,omitempty"`
}

// JSONSummary is the top-level shape written to stdout when
// --output=json is requested. See contracts/cli.md.
type JSONSummary struct {
	Version  int              `json:"version"`
	Run      RunInfo          `json:"run"`
	Counts   Counts           `json:"counts"`
	Commands []CommandSummary `json:"commands"`
}

// BuildJSONSummary collects per-command snapshots from the runner and
// returns a JSONSummary suitable for json.Marshal.
func BuildJSONSummary(r *Runner, info RunInfo) JSONSummary {
	finished := r.Finished()
	cmds := make([]CommandSummary, 0, len(finished))
	for _, f := range finished {
		_, started, ended := f.Times()
		cs := CommandSummary{
			ID:       f.ID,
			Text:     f.Text,
			Source:   string(f.Source),
			ExitCode: f.ExitCode(),
			State:    f.State().String(),
		}
		if !started.IsZero() {
			cs.StartedAt = started.UTC().Format(time.RFC3339Nano)
		}
		if !ended.IsZero() {
			cs.EndedAt = ended.UTC().Format(time.RFC3339Nano)
			cs.Duration = f.Duration().String()
		}
		cmds = append(cmds, cs)
	}
	return JSONSummary{
		Version:  1,
		Run:      info,
		Counts:   r.counts(),
		Commands: cmds,
	}
}

// EncodeJSONSummary writes the JSON summary to w with a trailing
// newline.
func EncodeJSONSummary(w io.Writer, s JSONSummary) error {
	return json.NewEncoder(w).Encode(s)
}
