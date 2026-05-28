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

	// Log file should exist and contain 5 frames.
	logPath := filepath.Join(dir, "cli-executed.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile log: %v", err)
	}
	begins := strings.Count(string(data), "=== begin ")
	ends := strings.Count(string(data), "=== end   ")
	if begins != 5 || ends != 5 {
		t.Errorf("frame counts begin=%d end=%d, want 5/5", begins, ends)
	}
}

func TestBatch_JSONSummary(t *testing.T) {
	bin := binary(t)
	dir := t.TempDir()

	cmd := exec.Command(bin, "--output=json", "echo a", "echo b", "false")
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
	cmd := exec.Command(bin, "--from-file", listing)
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
	data, _ := os.ReadFile(filepath.Join(dir, "cli-executed.log"))
	if !strings.Contains(string(data), "src=file") {
		t.Errorf("log missing src=file: %q", string(data))
	}
}
