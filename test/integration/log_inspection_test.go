package integration

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestLogInspection_AfterMixedRun exercises US3: after a run including
// stdout, stderr, success, failure, and timed-out commands, the log file
// has one well-formed frame per command and the footer fields are
// correct.
func TestLogInspection_AfterMixedRun(t *testing.T) {
	bin := binary(t)
	dir := t.TempDir()
	logPath := filepath.Join(dir, "cli-executed.log")

	cmd := exec.Command(bin,
		"--log", logPath,
		"--timeout", "200ms",
		"--kill-grace", "100ms",
		"echo to-stdout",
		"echo to-stderr >&2",
		"false",
		"sleep 2", // expected to time out
	)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	_ = cmd.Run()

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	s := string(data)

	// One begin, one end per submitted command.
	if got := strings.Count(s, "=== begin "); got != 4 {
		t.Errorf("begin count = %d, want 4", got)
	}
	if got := strings.Count(s, "=== end   "); got != 4 {
		t.Errorf("end count = %d, want 4", got)
	}

	// The to-stdout command's body must contain its line.
	if !strings.Contains(s, "to-stdout\n=== end") {
		t.Errorf("missing stdout body for echo to-stdout in:\n%s", s)
	}

	// The stderr command's body must contain its line.
	if !strings.Contains(s, "to-stderr\n=== end") {
		t.Errorf("missing stderr body for echo to-stderr in:\n%s", s)
	}

	// `false` should record exit=1.
	if !strings.Contains(s, `"false"`) {
		t.Errorf("missing false frame header: %s", s)
	}

	// `sleep 2` should record exit=timed-out (with --timeout 200ms).
	if !strings.Contains(s, "exit=timed-out") {
		t.Errorf("missing timed-out footer entry in:\n%s", s)
	}

	// Every footer must have exit= and dur= present.
	for _, line := range strings.Split(s, "\n") {
		if !strings.HasPrefix(line, "=== end   ") {
			continue
		}
		if !strings.Contains(line, "exit=") {
			t.Errorf("footer missing exit=: %q", line)
		}
		if !strings.Contains(line, "dur=") {
			t.Errorf("footer missing dur=: %q", line)
		}
	}
}
