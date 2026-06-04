package integration

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// resultLines returns the per-command result lines (those carrying an exit
// code) from captured stderr.
func resultLines(stderr string) []string {
	var out []string
	for ln := range strings.SplitSeq(stderr, "\n") {
		if strings.Contains(ln, "exit=") {
			out = append(out, ln)
		}
	}
	return out
}

// TestOutputFormat_AlignedColumns runs the binary piped (non-TTY → plain sink,
// fixed fallback width) and asserts every result line renders to the same width
// — i.e. the exit and duration zones line up (SC-001) — and that the columns are
// deterministic across runs (SC-005).
func TestOutputFormat_AlignedColumns(t *testing.T) {
	bin := binary(t)

	run := func() []string {
		dir := t.TempDir()
		cmd := exec.Command(bin,
			"--log-dir", dir, "--socket", shortSocketPath(t),
			"true", "false", "sleep 0.05")
		cmd.Dir = dir
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		_ = cmd.Run()
		return resultLines(stderr.String())
	}

	lines := run()
	if len(lines) != 3 {
		t.Fatalf("want 3 result lines, got %d:\n%s", len(lines), strings.Join(lines, "\n"))
	}

	width := len(lines[0])
	for _, ln := range lines {
		if len(ln) != width {
			t.Errorf("result lines are not the same width (columns misaligned):\n%s", strings.Join(lines, "\n"))
			break
		}
	}

	joined := strings.Join(lines, "\n")
	for _, want := range []string{"OK", "exit=0", "FAILED", "exit=1", "dur="} {
		if !strings.Contains(joined, want) {
			t.Errorf("result lines missing %q:\n%s", want, joined)
		}
	}

	// Determinism: a second run produces lines of the same fixed width.
	for _, ln := range run() {
		if len(ln) != width {
			t.Errorf("non-deterministic column width across runs: got %d, want %d (%q)", len(ln), width, ln)
		}
	}
}

// TestOutputFormat_BaseNameAndLogFidelity asserts a path-invoked program is
// displayed by its base name (FR-002 / SC-003) while the per-command log retains
// the full original command text including the path (FR-010 / SC-006).
func TestOutputFormat_BaseNameAndLogFidelity(t *testing.T) {
	bin := binary(t)
	base := t.TempDir()

	cmd := exec.Command(bin,
		"--log-dir", base, "--socket", shortSocketPath(t),
		"/bin/echo runqmarker")
	cmd.Dir = base
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("run: %v; stderr=%q", err, stderr.String())
	}

	// Display: base name shown, directory stripped.
	if !strings.Contains(stderr.String(), "echo runqmarker") {
		t.Errorf("stderr should show base-name command 'echo runqmarker':\n%s", stderr.String())
	}
	if strings.Contains(stderr.String(), "/bin/echo") {
		t.Errorf("stderr should not contain the program directory path:\n%s", stderr.String())
	}

	// Log fidelity: the per-command file keeps the full path.
	dir := runSubdir(t, base)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read run dir: %v", err)
	}
	var foundFullPath bool
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "/bin/echo") {
			foundFullPath = true
		}
	}
	if !foundFullPath {
		t.Errorf("per-command log should retain the full path '/bin/echo'; not found in %s", dir)
	}
}

// TestOutputFormat_QuietSilent asserts --quiet suppresses all per-command status
// output on stderr (FR-012).
func TestOutputFormat_QuietSilent(t *testing.T) {
	bin := binary(t)
	dir := t.TempDir()
	cmd := exec.Command(bin,
		"--quiet", "--log-dir", dir, "--socket", shortSocketPath(t),
		"true", "echo hi")
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("run: %v; stderr=%q", err, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("--quiet should emit nothing on stderr, got:\n%s", stderr.String())
	}
}
