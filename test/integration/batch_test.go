// Package integration runs end-to-end tests against the built runq binary.
package integration

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBatch_MixedOutcomes(t *testing.T) {
	bin := binary(t)
	dir := t.TempDir()

	cmd := exec.Command(bin,
		"--log-dir", dir,
		"--socket", shortSocketPath(t),
		"echo ok1",
		"echo ok2",
		"false",
		"sleep 0.05 && echo slept",
		"exit 9",
	)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	// Exit code 1: at least one command failed.
	if cmd.ProcessState.ExitCode() != 1 {
		t.Fatalf("exit code = %d, want 1; stdout=%q stderr=%q err=%v",
			cmd.ProcessState.ExitCode(), stdout.String(), stderr.String(), err)
	}

	// Stdout should contain exactly the summary line and nothing more.
	stdoutStr := strings.TrimSpace(stdout.String())
	if !strings.HasPrefix(stdoutStr, "runq:") {
		t.Errorf("stdout should start with summary, got %q", stdoutStr)
	}
	if strings.Count(stdoutStr, "\n") > 0 {
		t.Errorf("stdout has multiple lines:\n%s", stdoutStr)
	}
	if !strings.Contains(stdoutStr, "3 ok") || !strings.Contains(stdoutStr, "2 failed") {
		t.Errorf("summary missing expected counts: %q", stdoutStr)
	}

	// Stderr should NOT be empty (non-TTY: per-command status lines).
	if stderr.Len() == 0 {
		t.Errorf("stderr is empty; expected progress lines")
	}
	if !strings.Contains(stderr.String(), "c-0001") {
		t.Errorf("stderr missing id c-0001: %q", stderr.String())
	}

	// One per-command file should exist, each holding exactly one frame.
	runDir := runSubdir(t, dir)
	entries, err := os.ReadDir(runDir)
	if err != nil {
		t.Fatalf("read run dir: %v", err)
	}
	if len(entries) != 5 {
		t.Errorf("per-command file count = %d, want 5", len(entries))
	}
	var begins, ends int
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(runDir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		begins += strings.Count(string(data), "=== begin ")
		ends += strings.Count(string(data), "=== end   ")
	}
	if begins != 5 || ends != 5 {
		t.Errorf("frame counts begin=%d end=%d, want 5/5", begins, ends)
	}
}

func TestBatch_JSONSummary(t *testing.T) {
	bin := binary(t)
	dir := t.TempDir()

	cmd := exec.Command(bin, "--output=json",
		"--log-dir", dir, "--socket", shortSocketPath(t),
		"echo a", "echo b", "false")
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	_ = cmd.Run()

	if cmd.ProcessState.ExitCode() != 1 {
		t.Fatalf("exit code = %d, want 1", cmd.ProcessState.ExitCode())
	}
	out := stdout.String()
	if !strings.Contains(out, `"version":1`) {
		t.Errorf("expected version=1 in JSON: %q", out)
	}
	if !strings.Contains(out, `"succeeded":2`) {
		t.Errorf("expected succeeded=2: %q", out)
	}
	if !strings.Contains(out, `"failed":1`) {
		t.Errorf("expected failed=1: %q", out)
	}
}

func TestBatch_UsageErrorWhenNoInput(t *testing.T) {
	bin := binary(t)
	cmd := exec.Command(bin)
	cmd.Dir = t.TempDir()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	_ = cmd.Run()
	if cmd.ProcessState.ExitCode() != 2 {
		t.Errorf("exit code = %d, want 2 (usage)", cmd.ProcessState.ExitCode())
	}
	if !strings.Contains(stderr.String(), "no commands supplied") {
		t.Errorf("stderr missing usage hint: %q", stderr.String())
	}
}

func TestBatch_FromFile(t *testing.T) {
	bin := binary(t)
	dir := t.TempDir()
	listing := filepath.Join(dir, "cmds.txt")
	if err := os.WriteFile(listing, []byte("# comment\necho one\n\necho two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	logDir := filepath.Join(dir, "logs")
	cmd := exec.Command(bin, "--from-file", listing, "--log-dir", logDir, "--socket", shortSocketPath(t))
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("Run: %v; stderr=%q", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "2 ok") {
		t.Errorf("summary missing 2 ok: %q", stdout.String())
	}
	// Per-command files must record their source as the file.
	runDir := runSubdir(t, logDir)
	entries, err := os.ReadDir(runDir)
	if err != nil {
		t.Fatal(err)
	}
	var fileSrcCount int
	for _, e := range entries {
		data, _ := os.ReadFile(filepath.Join(runDir, e.Name()))
		if strings.Contains(string(data), "src=file") {
			fileSrcCount++
		}
	}
	if fileSrcCount != 2 {
		t.Errorf("files with src=file = %d, want 2 (one per from-file command) in %s", fileSrcCount, runDir)
	}
}
