package ipc_test

import (
	"os"
	"path/filepath"
	"testing"
)

// shortTempSocketPath returns a short path suitable for Unix domain
// sockets on macOS, where SUN_PATH is limited to 104 bytes. t.TempDir()
// paths under /var/folders can exceed that on macOS.
func shortTempSocketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "runq-test-")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "s.sock")
}
