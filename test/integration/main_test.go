package integration

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

var (
	binaryOnce sync.Once
	binaryPath string
	binaryErr  error
)

// shortSocketPath returns a /tmp-based path short enough for the macOS
// SUN_PATH limit (104 bytes). t.TempDir() on macOS lives under
// /var/folders/... and routinely exceeds it.
func shortSocketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "runq-it-")
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "s.sock")
}

// binary lazily builds the runq binary into a temp directory and returns
// its absolute path. The build is shared across tests in this package.
func binary(t *testing.T) string {
	t.Helper()
	binaryOnce.Do(func() {
		dir, err := os.MkdirTemp("", "runq-integ-")
		if err != nil {
			binaryErr = err
			return
		}
		out := filepath.Join(dir, "runq")
		cmd := exec.Command("go", "build", "-o", out, "../../cmd/runq")
		buildOut, err := cmd.CombinedOutput()
		if err != nil {
			binaryErr = fmt.Errorf("go build: %v: %s", err, string(buildOut))
			return
		}
		binaryPath = out
	})
	if binaryErr != nil {
		t.Fatalf("could not build runq: %v", binaryErr)
	}
	return binaryPath
}
