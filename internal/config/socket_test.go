package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeInfo struct {
	name  string
	mode  os.FileMode
	isDir bool
	sys   any
}

func (f fakeInfo) Name() string       { return f.name }
func (f fakeInfo) Size() int64        { return 0 }
func (f fakeInfo) Mode() os.FileMode  { return f.mode }
func (f fakeInfo) ModTime() time.Time { return time.Time{} }
func (f fakeInfo) IsDir() bool        { return f.isDir }
func (f fakeInfo) Sys() any           { return f.sys }

func TestDefaultSocketPath_XDGPreferred(t *testing.T) {
	// Real filesystem check: use a TempDir that we know is mode 0700 owned
	// by us; XDG_RUNTIME_DIR points there.
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	getenv := func(k string) string {
		if k == "XDG_RUNTIME_DIR" {
			return dir
		}
		return ""
	}
	got := defaultSocketPath(getenv, os.Stat, os.Getuid)
	want := filepath.Join(dir, "runq.sock")
	if got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
}

func TestDefaultSocketPath_FallbackOnMissingXDG(t *testing.T) {
	getenv := func(string) string { return "" }
	got := defaultSocketPath(getenv, os.Stat, func() int { return 4242 })
	want := "/tmp/runq-4242.sock"
	if got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
}

func TestDefaultSocketPath_FallbackOnWrongPerms(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer func() { _ = os.Chmod(dir, 0o700) }() // restore for t.TempDir cleanup
	getenv := func(k string) string {
		if k == "XDG_RUNTIME_DIR" {
			return dir
		}
		return ""
	}
	got := defaultSocketPath(getenv, os.Stat, func() int { return 4242 })
	if strings.Contains(got, dir) {
		t.Errorf("expected fallback to /tmp because XDG dir is 0755, got %q", got)
	}
	if got != "/tmp/runq-4242.sock" {
		t.Errorf("path = %q, want /tmp/runq-4242.sock", got)
	}
}

func TestDefaultSocketPath_FallbackOnMissingDir(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	getenv := func(k string) string {
		if k == "XDG_RUNTIME_DIR" {
			return missing
		}
		return ""
	}
	got := defaultSocketPath(getenv, os.Stat, func() int { return 7 })
	if got != "/tmp/runq-7.sock" {
		t.Errorf("path = %q, want fallback", got)
	}
}

// Compile-time guard so the fakeInfo type can be used by other tests if needed.
var _ os.FileInfo = fakeInfo{}
var _ = fmt.Sprintf
