package integration

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestLogDir_XDGStateHomeAutoCreated exercises US2: with no --log-dir, logs
// land under $XDG_STATE_HOME/runq/logs, the tree is auto-created from a clean
// environment, and the location is independent of the working directory.
func TestLogDir_XDGStateHomeAutoCreated(t *testing.T) {
	bin := binary(t)
	state := t.TempDir() // clean: runq/logs does not exist yet
	workdir := t.TempDir()

	cmd := exec.Command(bin,
		"--verbose",
		"--socket", shortSocketPath(t),
		"echo from-anywhere",
	)
	cmd.Dir = workdir // arbitrary CWD, different from the log dir
	cmd.Env = append(os.Environ(), "XDG_STATE_HOME="+state)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", err, stderr.String())
	}

	base := filepath.Join(state, "runq", "logs")
	if _, err := os.Stat(base); err != nil {
		t.Fatalf("XDG log base not auto-created at %s: %v", base, err)
	}
	dir := runSubdir(t, base)
	if s := readFileWithSlug(t, dir, "echo-from-anywhere"); !strings.Contains(s, "from-anywhere\n=== end") {
		t.Errorf("missing command body:\n%s", s)
	}
	// FR-016: verbose reports the resolved run directory (subdir, not just base) on stderr.
	if !strings.Contains(stderr.String(), "log dir") {
		t.Errorf("verbose stderr missing 'log dir' report:\n%s", stderr.String())
	}
	if !strings.Contains(stderr.String(), dir) {
		t.Errorf("verbose stderr does not name the run subdir %s:\n%s", dir, stderr.String())
	}
}

// TestLogDir_FlagOverridesEnv verifies precedence: --log-dir (flag) wins over
// RUNQ_LOG_DIR (env), which would win over the XDG default.
func TestLogDir_FlagOverridesEnv(t *testing.T) {
	bin := binary(t)
	flagDir := filepath.Join(t.TempDir(), "flag")
	envDir := filepath.Join(t.TempDir(), "env")

	cmd := exec.Command(bin,
		"--log-dir", flagDir,
		"--socket", shortSocketPath(t),
		"echo hi",
	)
	cmd.Env = append(os.Environ(), "RUNQ_LOG_DIR="+envDir)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("run: %v\n%s", err, stderr.String())
	}

	if _, err := os.Stat(flagDir); err != nil {
		t.Errorf("flag dir %s not used: %v", flagDir, err)
	}
	if _, err := os.Stat(envDir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("env dir %s should not exist when --log-dir is set (got err=%v)", envDir, err)
	}
}

// TestLogDir_EnvOverridesDefault verifies RUNQ_LOG_DIR is used when no flag is
// given.
func TestLogDir_EnvOverridesDefault(t *testing.T) {
	bin := binary(t)
	envDir := filepath.Join(t.TempDir(), "envlogs")

	cmd := exec.Command(bin, "--socket", shortSocketPath(t), "echo hi")
	cmd.Env = append(os.Environ(), "RUNQ_LOG_DIR="+envDir)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("run: %v\n%s", err, stderr.String())
	}
	if _, err := os.Stat(envDir); err != nil {
		t.Errorf("RUNQ_LOG_DIR dir %s not used: %v", envDir, err)
	}
}

// TestLogDir_WriteFailureExitsNonZero exercises FR-015: when the log directory
// cannot be created, runq surfaces a clear stderr error and exits with the
// documented log-write-failed code (11) rather than silently discarding.
func TestLogDir_WriteFailureExitsNonZero(t *testing.T) {
	bin := binary(t)
	// Make a regular file, then point --log-dir beneath it: MkdirAll must fail
	// with ENOTDIR on every supported platform.
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	badDir := filepath.Join(blocker, "logs")

	cmd := exec.Command(bin, "--log-dir", badDir, "--socket", shortSocketPath(t), "echo hi")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()

	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("expected non-zero exit, got err=%v\nstderr:\n%s", err, stderr.String())
	}
	if ee.ExitCode() != 11 {
		t.Errorf("exit code = %d, want 11 (log-write-failed)", ee.ExitCode())
	}
	if !strings.Contains(stderr.String(), "log directory") {
		t.Errorf("stderr missing clear log-directory error:\n%s", stderr.String())
	}
}
