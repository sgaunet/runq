package integration

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Documented exit codes (mirrors internal/exitcode; the integration package
// must not import internal packages, so they are restated here).
const (
	codeOK             = 0
	codeCancelled      = 10
	codeSocketConflict = 12
	codeForwardFailed  = 14
)

// serveProc wraps a backgrounded `runq serve` process with captured streams
// and a non-blocking exit check.
type serveProc struct {
	cmd  *exec.Cmd
	out  bytes.Buffer
	err  bytes.Buffer
	done chan struct{}
}

func startServe(t *testing.T, socket, logDir string, extra ...string) *serveProc {
	t.Helper()
	bin := binary(t)
	args := append([]string{"serve", "--socket", socket, "--log-dir", logDir}, extra...)
	sp := &serveProc{cmd: exec.Command(bin, args...), done: make(chan struct{})}
	sp.cmd.Stdout = &sp.out
	sp.cmd.Stderr = &sp.err
	if err := sp.cmd.Start(); err != nil {
		t.Fatalf("start serve: %v", err)
	}
	go func() { _ = sp.cmd.Wait(); close(sp.done) }()
	t.Cleanup(func() {
		if sp.cmd.Process != nil {
			_ = sp.cmd.Process.Kill()
		}
	})
	waitForSocket(t, socket)
	return sp
}

func (sp *serveProc) running() bool {
	select {
	case <-sp.done:
		return false
	default:
		return true
	}
}

func (sp *serveProc) signal(t *testing.T, sig os.Signal) {
	t.Helper()
	if err := sp.cmd.Process.Signal(sig); err != nil {
		t.Fatalf("signal %v: %v", sig, err)
	}
}

func (sp *serveProc) waitExit(t *testing.T, d time.Duration) int {
	t.Helper()
	select {
	case <-sp.done:
		return sp.cmd.ProcessState.ExitCode()
	case <-time.After(d):
		t.Fatalf("serve did not exit within %v; stderr=%q", d, sp.err.String())
		return -1
	}
}

func waitForSocket(t *testing.T, socket string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(socket); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("socket %s did not appear", socket)
}

func forwardCmd(t *testing.T, socket string, cmds ...string) *exec.Cmd {
	t.Helper()
	args := append([]string{"--socket", socket}, cmds...)
	return exec.Command(binary(t), args...)
}

func mustForward(t *testing.T, socket string, cmds ...string) {
	t.Helper()
	c := forwardCmd(t, socket, cmds...)
	var eb bytes.Buffer
	c.Stderr = &eb
	if err := c.Run(); err != nil {
		t.Fatalf("forward %v failed: %v; stderr=%q", cmds, err, eb.String())
	}
}

// readSingleLog returns the content of the one .log file in dir (for runs of
// a single command).
func readSingleLog(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	var logs []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".log") {
			logs = append(logs, e.Name())
		}
	}
	if len(logs) != 1 {
		t.Fatalf("want exactly 1 log file in %s, got %v", dir, logs)
	}
	data, err := os.ReadFile(filepath.Join(dir, logs[0]))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestServe_PersistentListener (US1): serve stays alive while idle, runs two
// forwarded waves with a gap, logs them under one session directory, and
// exits 0 on an idle Ctrl+C.
func TestServe_PersistentListener(t *testing.T) {
	base := t.TempDir()
	socket := shortSocketPath(t)
	sp := startServe(t, socket, base)

	time.Sleep(400 * time.Millisecond)
	if !sp.running() {
		t.Fatalf("serve exited while idle; stderr=%q", sp.err.String())
	}

	mustForward(t, socket, "echo wave1a", "echo wave1b")
	time.Sleep(500 * time.Millisecond)
	if !sp.running() {
		t.Fatalf("serve exited after wave1 drained; stderr=%q", sp.err.String())
	}

	mustForward(t, socket, "echo wave2")
	time.Sleep(400 * time.Millisecond)

	sp.signal(t, syscall.SIGINT)
	if code := sp.waitExit(t, 5*time.Second); code != codeOK {
		t.Errorf("idle serve stop: exit = %d, want %d; stderr=%q", code, codeOK, sp.err.String())
	}

	dir := runSubdir(t, base)
	for _, slug := range []string{"echo-wave1a", "echo-wave1b", "echo-wave2"} {
		_ = readFileWithSlug(t, dir, slug) // fatals if missing
	}
}

// TestServe_GracefulShutdown_PropagatesAndExits10 (US2): a 1st Ctrl+C sends
// SIGTERM to the in-flight child (which runs its cleanup) and serve exits 10.
func TestServe_GracefulShutdown_PropagatesAndExits10(t *testing.T) {
	base := t.TempDir()
	socket := shortSocketPath(t)
	sp := startServe(t, socket, base, "--kill-grace", "5s")

	mustForward(t, socket, `trap 'echo CLEANUP; exit 0' TERM; sleep 30`)
	time.Sleep(600 * time.Millisecond)

	sp.signal(t, syscall.SIGINT)
	if code := sp.waitExit(t, 8*time.Second); code != codeCancelled {
		t.Errorf("serve exit = %d, want %d (cancelled); stderr=%q", code, codeCancelled, sp.err.String())
	}

	got := readSingleLog(t, runSubdir(t, base))
	if !strings.Contains(got, "CLEANUP") {
		t.Errorf("child cleanup line not captured — SIGTERM not propagated?\n%s", got)
	}
}

// TestServe_SecondSignalForces (US2): with a child that ignores SIGTERM and a
// long kill-grace, a 2nd Ctrl+C forces a fast exit (well under the grace).
func TestServe_SecondSignalForces(t *testing.T) {
	base := t.TempDir()
	socket := shortSocketPath(t)
	sp := startServe(t, socket, base, "--kill-grace", "60s")

	mustForward(t, socket, `trap '' TERM; sleep 300`)
	time.Sleep(600 * time.Millisecond)

	sp.signal(t, syscall.SIGINT) // graceful begins; child ignores SIGTERM
	time.Sleep(300 * time.Millisecond)
	start := time.Now()
	sp.signal(t, syscall.SIGINT) // force
	code := sp.waitExit(t, 5*time.Second)
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("force exit took %v, want < 3s despite kill-grace 60s", elapsed)
	}
	if code != codeCancelled {
		t.Errorf("serve exit = %d, want %d", code, codeCancelled)
	}
}

// TestServe_IdleStopExitsZero (US2): stopping while idle exits 0 even if an
// earlier command in the session failed (clarify Q2 / SC-005).
func TestServe_IdleStopExitsZero(t *testing.T) {
	base := t.TempDir()
	socket := shortSocketPath(t)
	sp := startServe(t, socket, base)

	mustForward(t, socket, "false") // fails, then serve returns to idle
	time.Sleep(500 * time.Millisecond)
	if !sp.running() {
		t.Fatalf("serve exited unexpectedly; stderr=%q", sp.err.String())
	}

	sp.signal(t, syscall.SIGINT)
	if code := sp.waitExit(t, 5*time.Second); code != codeOK {
		t.Errorf("idle stop after a failed command: exit = %d, want %d", code, codeOK)
	}
}

// TestServe_PendingCancelledOnShutdown (FR-015): queued-but-unstarted commands
// are cancelled, not launched, when serve shuts down.
func TestServe_PendingCancelledOnShutdown(t *testing.T) {
	base := t.TempDir()
	socket := shortSocketPath(t)
	sp := startServe(t, socket, base, "--parallel", "1", "--kill-grace", "2s")

	mustForward(t, socket, "sleep 30", "echo pend1", "echo pend2")
	time.Sleep(600 * time.Millisecond)

	sp.signal(t, syscall.SIGINT)
	if code := sp.waitExit(t, 8*time.Second); code != codeCancelled {
		t.Errorf("serve exit = %d, want %d", code, codeCancelled)
	}
	if !strings.Contains(sp.out.String(), "3 cancelled") {
		t.Errorf("summary should show 3 cancelled (pending not launched), got: %q", sp.out.String())
	}
	// Only the started command produced a log; the two pending ones never ran.
	dir := runSubdir(t, base)
	entries, _ := os.ReadDir(dir)
	logs := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".log") {
			logs++
		}
	}
	if logs != 1 {
		t.Errorf("want exactly 1 log file (only the started command), got %d", logs)
	}
}

// TestServe_RefusesSubmissionsDuringShutdown (FR-012): while draining, a new
// forward is refused (non-zero exit).
func TestServe_RefusesSubmissionsDuringShutdown(t *testing.T) {
	base := t.TempDir()
	socket := shortSocketPath(t)
	sp := startServe(t, socket, base, "--kill-grace", "10s")

	// A child that ignores SIGTERM keeps serve in DRAINING for the grace window.
	mustForward(t, socket, `trap '' TERM; sleep 30`)
	time.Sleep(600 * time.Millisecond)
	sp.signal(t, syscall.SIGINT) // begin graceful → BeginShutdown refuses new
	time.Sleep(400 * time.Millisecond)

	c := forwardCmd(t, socket, "echo nope")
	var ce bytes.Buffer
	c.Stderr = &ce
	if err := c.Run(); err == nil {
		t.Errorf("forward during shutdown should be refused; stderr=%q", ce.String())
	}

	sp.signal(t, syscall.SIGINT) // force to finish the test quickly
	sp.waitExit(t, 5*time.Second)
}

// TestServe_RunqStopGraceful (FR-011a): `runq stop` triggers the same graceful
// shutdown as Ctrl+C; the stop client exits 0.
func TestServe_RunqStopGraceful(t *testing.T) {
	base := t.TempDir()
	socket := shortSocketPath(t)
	sp := startServe(t, socket, base)

	stop := exec.Command(binary(t), "stop", "--socket", socket)
	var se bytes.Buffer
	stop.Stderr = &se
	if err := stop.Run(); err != nil {
		t.Fatalf("runq stop: %v; stderr=%q", err, se.String())
	}
	if code := stop.ProcessState.ExitCode(); code != codeOK {
		t.Errorf("stop exit = %d, want %d", code, codeOK)
	}
	if code := sp.waitExit(t, 5*time.Second); code != codeOK {
		t.Errorf("serve exit after stop = %d, want %d (idle)", code, codeOK)
	}
}

// TestServe_RunqStop_NoInstance: `runq stop` with no live instance exits 14.
func TestServe_RunqStop_NoInstance(t *testing.T) {
	socket := shortSocketPath(t)
	stop := exec.Command(binary(t), "stop", "--socket", socket)
	var se bytes.Buffer
	stop.Stderr = &se
	if err := stop.Run(); err == nil {
		t.Fatal("stop with no instance should fail")
	}
	if code := stop.ProcessState.ExitCode(); code != codeForwardFailed {
		t.Errorf("stop exit = %d, want %d; stderr=%q", code, codeForwardFailed, se.String())
	}
}

// TestServe_DuplicateRefused (US3 / SC-006): a 2nd serve refuses with exit 12
// and the first is unaffected.
func TestServe_DuplicateRefused(t *testing.T) {
	base := t.TempDir()
	socket := shortSocketPath(t)
	sp := startServe(t, socket, base)

	dup := exec.Command(binary(t), "serve", "--socket", socket, "--log-dir", t.TempDir())
	var de bytes.Buffer
	dup.Stderr = &de
	if err := dup.Run(); err == nil {
		t.Fatal("second serve should refuse to start")
	}
	if code := dup.ProcessState.ExitCode(); code != codeSocketConflict {
		t.Errorf("dup serve exit = %d, want %d; stderr=%q", code, codeSocketConflict, de.String())
	}
	if !strings.Contains(de.String(), "already listening") {
		t.Errorf("dup serve message unclear: %q", de.String())
	}
	if !sp.running() {
		t.Errorf("first serve died after a duplicate attempt; stderr=%q", sp.err.String())
	}

	mustForward(t, socket, "echo stillworks")
	sp.signal(t, syscall.SIGINT)
	sp.waitExit(t, 5*time.Second)
}

// TestServe_ReclaimsStaleSocket (US3 / FR-009): after a serve is killed
// ungracefully (stale socket left behind), a new serve reclaims it.
func TestServe_ReclaimsStaleSocket(t *testing.T) {
	base := t.TempDir()
	socket := shortSocketPath(t)
	sp := startServe(t, socket, base)
	_ = sp.cmd.Process.Kill() // SIGKILL: deferred socket cleanup does not run
	sp.waitExit(t, 5*time.Second)

	sp2 := startServe(t, socket, t.TempDir())
	time.Sleep(500 * time.Millisecond)
	if !sp2.running() {
		t.Fatalf("serve did not start over a stale socket; stderr=%q", sp2.err.String())
	}
	mustForward(t, socket, "echo reclaimed") // proves it is a live listener
	sp2.signal(t, syscall.SIGINT)
	sp2.waitExit(t, 5*time.Second)
}
