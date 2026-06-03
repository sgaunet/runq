package integration

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestLogCollision_IdenticalParallelCommands exercises US3: identical command
// text run concurrently in one batch yields a distinct, complete file per
// execution — none overwritten, none interleaved.
func TestLogCollision_IdenticalParallelCommands(t *testing.T) {
	bin := binary(t)
	base := t.TempDir()

	const N = 4
	args := []string{"--log-dir", base, "--socket", shortSocketPath(t), "--parallel", "4"}
	for range N {
		args = append(args, "echo dup-line")
	}
	cmd := exec.Command(bin, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("run: %v\n%s", err, stderr.String())
	}

	dir := runSubdir(t, base)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != N {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Fatalf("file count = %d, want %d (collision lost data); files: %v", len(entries), N, names)
	}
	for _, e := range entries {
		if !strings.Contains(e.Name(), "_echo-dup-line_") {
			t.Errorf("unexpected file name %q", e.Name())
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(data, []byte("dup-line\n=== end")) {
			t.Errorf("file %s missing complete body:\n%s", e.Name(), data)
		}
		s := string(data)
		if strings.Count(s, "=== begin ") != 1 || strings.Count(s, "=== end   ") != 1 {
			t.Errorf("file %s contains more than one record (collision/interleave bug):\n%s", e.Name(), s)
		}
	}
}

// TestLogCollision_AcrossRuns verifies two separate invocations sharing a base
// dir each get their own run subdirectory (no clobbering).
func TestLogCollision_AcrossRuns(t *testing.T) {
	bin := binary(t)
	base := t.TempDir()

	for range 2 {
		cmd := exec.Command(bin, "--log-dir", base, "--socket", shortSocketPath(t), "echo same")
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("run: %v\n%s", err, stderr.String())
		}
	}

	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatal(err)
	}
	dirs := 0
	for _, e := range entries {
		if e.IsDir() {
			dirs++
		}
	}
	if dirs != 2 {
		t.Errorf("run subdirs = %d, want 2 (one per invocation)", dirs)
	}
}
