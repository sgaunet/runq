package runner_test

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/sgaunet/runq/internal/logwriter"
	"github.com/sgaunet/runq/internal/runner"
	"github.com/sgaunet/runq/internal/ui"
)

func TestJSONSummary_ShapeMatchesContract(t *testing.T) {
	dir := t.TempDir()
	lw, _, err := logwriter.Open(filepath.Join(dir, "log.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer lw.Close()
	r := runner.New(runner.Options{
		Parallelism: 2, QueueCap: 50, Shell: true,
		KillGrace: time.Second, Sink: ui.Quiet{}, Log: lw,
	})
	if _, err := r.Submit([]runner.Spec{
		{Text: "true", Source: runner.SourceCLI},
		{Text: "false", Source: runner.SourceCLI},
	}); err != nil {
		t.Fatal(err)
	}
	r.Close()
	r.Run(context.Background())

	info := runner.RunInfo{LogPath: "log.log", SocketPath: "/tmp/x.sock"}
	summary := runner.BuildJSONSummary(r, info)

	if summary.Version != 1 {
		t.Errorf("Version = %d, want 1", summary.Version)
	}
	if summary.Counts.Total != 2 || summary.Counts.Succeeded != 1 || summary.Counts.Failed != 1 {
		t.Errorf("counts = %+v", summary.Counts)
	}
	if len(summary.Commands) != 2 {
		t.Fatalf("commands len = %d, want 2", len(summary.Commands))
	}
	for _, c := range summary.Commands {
		if c.ID == "" {
			t.Errorf("command has empty id")
		}
		if c.Source != string(runner.SourceCLI) {
			t.Errorf("command source = %q", c.Source)
		}
		if c.StartedAt == "" || c.EndedAt == "" {
			t.Errorf("missing timestamps: %+v", c)
		}
	}

	// Verify it round-trips through JSON without losing required fields.
	var buf bytes.Buffer
	if err := runner.EncodeJSONSummary(&buf, summary); err != nil {
		t.Fatal(err)
	}
	var roundTrip map[string]any
	if err := json.Unmarshal(buf.Bytes(), &roundTrip); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"version", "run", "counts", "commands"} {
		if _, ok := roundTrip[key]; !ok {
			t.Errorf("JSON missing top-level key %q", key)
		}
	}
	run, _ := roundTrip["run"].(map[string]any)
	if run["log_path"] != "log.log" {
		t.Errorf("run.log_path = %v, want %q", run["log_path"], "log.log")
	}
	if run["socket_path"] != "/tmp/x.sock" {
		t.Errorf("run.socket_path = %v, want %q", run["socket_path"], "/tmp/x.sock")
	}
}
