package exec_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sgaunet/runq/internal/exec"
)

func TestRun_ShellMode_OK(t *testing.T) {
	var out bytes.Buffer
	res := exec.Run(context.Background(), context.Background(), exec.Spec{
		Text:      "echo hello && echo world",
		Shell:     true,
		KillGrace: time.Second,
	}, &out)
	if res.ExitCode != 0 || res.Reason != "ok" {
		t.Fatalf("res=%+v, want exit=0 reason=ok", res)
	}
	got := out.String()
	if !strings.Contains(got, "hello") || !strings.Contains(got, "world") {
		t.Errorf("output = %q, want both hello and world", got)
	}
}

func TestRun_ShellMode_FailureExitCode(t *testing.T) {
	var out bytes.Buffer
	res := exec.Run(context.Background(), context.Background(), exec.Spec{
		Text:      "exit 7",
		Shell:     true,
		KillGrace: time.Second,
	}, &out)
	if res.ExitCode != 7 || res.Reason != "failed" {
		t.Errorf("res=%+v, want exit=7 reason=failed", res)
	}
}

func TestRun_ArgvMode(t *testing.T) {
	var out bytes.Buffer
	res := exec.Run(context.Background(), context.Background(), exec.Spec{
		Argv:      []string{"echo", "argv", "mode"},
		Shell:     false,
		KillGrace: time.Second,
	}, &out)
	if res.ExitCode != 0 {
		t.Fatalf("res=%+v", res)
	}
	if got := strings.TrimSpace(out.String()); got != "argv mode" {
		t.Errorf("output = %q, want %q", got, "argv mode")
	}
}

func TestRun_ArgvMode_RejectsEmpty(t *testing.T) {
	var out bytes.Buffer
	res := exec.Run(context.Background(), context.Background(), exec.Spec{Argv: nil, Shell: false}, &out)
	if res.Reason != "spawn-error" {
		t.Errorf("res=%+v, want spawn-error", res)
	}
}

func TestRun_StdinIsDevNull(t *testing.T) {
	// A child that reads from stdin and prints how many bytes it got. With
	// stdin connected to /dev/null, wc -c should print 0.
	var out bytes.Buffer
	res := exec.Run(context.Background(), context.Background(), exec.Spec{
		Text:      "wc -c",
		Shell:     true,
		KillGrace: time.Second,
	}, &out)
	if res.ExitCode != 0 {
		t.Fatalf("res=%+v", res)
	}
	if !strings.Contains(out.String(), "0") {
		t.Errorf("wc -c stdout = %q, want output containing 0", out.String())
	}
}

func TestRun_Timeout_TimedOut(t *testing.T) {
	var out bytes.Buffer
	start := time.Now()
	res := exec.Run(context.Background(), context.Background(), exec.Spec{
		Text:      "sleep 5",
		Shell:     true,
		Timeout:   200 * time.Millisecond,
		KillGrace: 200 * time.Millisecond,
	}, &out)
	elapsed := time.Since(start)
	if res.Reason != "timed-out" {
		t.Errorf("res=%+v, want timed-out", res)
	}
	if elapsed > 2*time.Second {
		t.Errorf("elapsed = %v, expected sub-second", elapsed)
	}
}

func TestRun_CtxCancel_Cancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var out bytes.Buffer
	done := make(chan exec.Result, 1)
	go func() {
		done <- exec.Run(ctx, context.Background(), exec.Spec{
			Text:      "sleep 5",
			Shell:     true,
			KillGrace: 200 * time.Millisecond,
		}, &out)
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case res := <-done:
		if res.Reason != "cancelled" {
			t.Errorf("res=%+v, want cancelled", res)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("exec.Run did not return after cancel")
	}
}

// TestRun_ForceCtx_KillsImmediately verifies that cancelling forceCtx
// SIGKILLs the child well inside a long KillGrace window, so a serve
// listener's 2nd Ctrl+C does not have to wait out the grace.
func TestRun_ForceCtx_KillsImmediately(t *testing.T) {
	forceCtx, force := context.WithCancel(context.Background())
	var out bytes.Buffer
	done := make(chan exec.Result, 1)
	start := time.Now()
	go func() {
		// A child that ignores SIGTERM; only SIGKILL stops it. KillGrace is
		// deliberately long so a force is the only way to finish quickly.
		done <- exec.Run(context.Background(), forceCtx, exec.Spec{
			Text:      "trap '' TERM; sleep 30",
			Shell:     true,
			KillGrace: 30 * time.Second,
		}, &out)
	}()
	time.Sleep(150 * time.Millisecond)
	force()
	select {
	case res := <-done:
		if res.Reason != "cancelled" {
			t.Errorf("res=%+v, want cancelled", res)
		}
		if elapsed := time.Since(start); elapsed > 5*time.Second {
			t.Errorf("force kill took %v, want well under KillGrace (30s)", elapsed)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("exec.Run did not return after force-cancel")
	}
}
