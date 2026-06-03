package integration

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestForward_HappyPath starts a runner that sleeps long enough for a
// second invocation to detect it and forward a command.
func TestForward_HappyPath(t *testing.T) {
	bin := binary(t)
	dir := t.TempDir()
	socket := shortSocketPath(t)

	// Start the runner in the background. Its initial batch sleeps 3s so
	// the forwarder has ample time to forward.
	runner := exec.Command(bin,
		"--socket", socket,
		"--log-dir", dir,
		"sleep 3 && echo first",
	)
	runner.Dir = dir
	var runnerOut, runnerErr bytes.Buffer
	runner.Stdout = &runnerOut
	runner.Stderr = &runnerErr
	if err := runner.Start(); err != nil {
		t.Fatalf("start runner: %v", err)
	}

	// Wait for the socket to appear (up to 2s).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(socket); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Run the forwarder. (A forwarder does not open a log dir of its own.)
	fwd := exec.Command(bin,
		"--socket", socket,
		"echo forwarded",
	)
	fwd.Dir = dir
	var fwdOut, fwdErr bytes.Buffer
	fwd.Stdout = &fwdOut
	fwd.Stderr = &fwdErr
	if err := fwd.Run(); err != nil {
		t.Fatalf("forwarder failed: %v; stderr=%q", err, fwdErr.String())
	}
	if fwd.ProcessState.ExitCode() != 0 {
		t.Errorf("forwarder exit = %d, want 0", fwd.ProcessState.ExitCode())
	}
	if !strings.Contains(fwdErr.String(), "forwarded") {
		t.Errorf("forwarder stderr missing confirmation: %q", fwdErr.String())
	}
	// Forwarder must NOT have written to stdout.
	if fwdOut.Len() != 0 {
		t.Errorf("forwarder wrote to stdout: %q", fwdOut.String())
	}

	// Wait for the runner to finish.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = runner.Wait()
	}()
	wg.Wait()

	if runner.ProcessState.ExitCode() != 0 {
		t.Errorf("runner exit = %d, want 0; stderr=%q", runner.ProcessState.ExitCode(), runnerErr.String())
	}

	// The runner's per-run directory should contain a file for BOTH the
	// initial command and the forwarded command (FR: forwarded submissions
	// are logged under the same per-command scheme).
	runDir := runSubdir(t, dir)
	initial := readFileWithSlug(t, runDir, "sleep-3-echo-first")
	if !strings.Contains(initial, `"sleep 3 && echo first"`) {
		t.Errorf("initial command file missing its frame:\n%s", initial)
	}
	forwarded := readFileWithSlug(t, runDir, "echo-forwarded")
	if !strings.Contains(forwarded, `"echo forwarded"`) {
		t.Errorf("forwarded command file missing its frame:\n%s", forwarded)
	}
	if !strings.Contains(forwarded, "src=socket") {
		t.Errorf("forwarded command file missing src=socket label:\n%s", forwarded)
	}
}

// TestForward_NoRunner_ExitsAsRunner verifies that without a running
// instance, the invocation simply becomes the runner and executes
// normally (no forward attempt failure).
func TestForward_NoRunner_BehavesAsRunner(t *testing.T) {
	bin := binary(t)
	dir := t.TempDir()
	socket := shortSocketPath(t)

	cmd := exec.Command(bin, "--socket", socket, "--log-dir", dir, "echo hi")
	cmd.Dir = dir
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(stdout.String(), "1 ok") {
		t.Errorf("summary missing 1 ok: %q", stdout.String())
	}
}

// TestStop_Drains starts a runner with a long-running batch, calls
// `runq stop`, and verifies the runner exits cleanly.
func TestStop_Drains(t *testing.T) {
	bin := binary(t)
	dir := t.TempDir()
	socket := shortSocketPath(t)

	runner := exec.Command(bin,
		"--socket", socket,
		"--log-dir", dir,
		"sleep 2 && echo done",
	)
	runner.Dir = dir
	if err := runner.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}

	// Wait for the socket.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(socket); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	stop := exec.Command(bin, "stop", "--socket", socket)
	stop.Dir = dir
	var stopErr bytes.Buffer
	stop.Stderr = &stopErr
	if err := stop.Run(); err != nil {
		t.Fatalf("stop: %v; stderr=%q", err, stopErr.String())
	}
	if stop.ProcessState.ExitCode() != 0 {
		t.Errorf("stop exit = %d, want 0", stop.ProcessState.ExitCode())
	}

	if err := runner.Wait(); err != nil {
		t.Fatalf("runner wait: %v", err)
	}
	if runner.ProcessState.ExitCode() != 0 {
		t.Errorf("runner exit = %d, want 0", runner.ProcessState.ExitCode())
	}
}
