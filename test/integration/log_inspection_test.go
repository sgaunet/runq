package integration

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runSubdir returns the single per-run subdirectory created under base.
func runSubdir(t *testing.T, base string) string {
	t.Helper()
	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatalf("read base log dir %s: %v", base, err)
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	if len(dirs) != 1 {
		t.Fatalf("want exactly one run subdir under %s, found %v", base, dirs)
	}
	return filepath.Join(base, dirs[0])
}

// readFileWithSlug returns the content of the single file in dir whose name
// contains the given slug fragment.
func readFileWithSlug(t *testing.T, dir, slug string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read run dir %s: %v", dir, err)
	}
	var match string
	for _, e := range entries {
		if strings.Contains(e.Name(), "_"+slug+"_") {
			if match != "" {
				t.Fatalf("multiple files match slug %q: %s and %s", slug, match, e.Name())
			}
			match = e.Name()
		}
	}
	if match == "" {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Fatalf("no file matches slug %q; files: %v", slug, names)
	}
	data, err := os.ReadFile(filepath.Join(dir, match))
	if err != nil {
		t.Fatalf("read %s: %v", match, err)
	}
	return string(data)
}

// TestLogInspection_PerCommandFiles exercises US1: after a run including
// stdout, stderr, a failure, and a timed-out command, there is exactly one
// well-formed per-command file, each name encodes its command, and each file
// holds that command's output framed by begin/end markers.
func TestLogInspection_PerCommandFiles(t *testing.T) {
	bin := binary(t)
	base := t.TempDir()

	cmd := exec.Command(bin,
		"--log-dir", base,
		"--socket", shortSocketPath(t),
		"--timeout", "200ms",
		"--kill-grace", "100ms",
		"echo to-stdout",
		"echo to-stderr >&2",
		"false",
		"sleep 2", // expected to time out
	)
	var out bytes.Buffer
	cmd.Stdout = &out
	_ = cmd.Run()

	dir := runSubdir(t, base)

	// One file per submitted command.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read run dir: %v", err)
	}
	if len(entries) != 4 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("file count = %d, want 4; files: %v", len(entries), names)
	}

	// Each file is named <ts>_<slug>_<id>.log and contains exactly one record.
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".log") {
			t.Errorf("file %q missing .log suffix", e.Name())
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		s := string(data)
		if strings.Count(s, "=== begin ") != 1 || strings.Count(s, "=== end   ") != 1 {
			t.Errorf("file %q is not exactly one record:\n%s", e.Name(), s)
		}
		if !strings.Contains(s, "dur=") {
			t.Errorf("file %q footer missing dur= field:\n%s", e.Name(), s)
		}
	}

	// stdout command: recognizable name + body.
	if s := readFileWithSlug(t, dir, "echo-to-stdout"); !strings.Contains(s, "to-stdout\n=== end") {
		t.Errorf("echo-to-stdout file missing body:\n%s", s)
	}
	// stderr command: body captured too.
	if s := readFileWithSlug(t, dir, "echo-to-stderr-2"); !strings.Contains(s, "to-stderr\n=== end") {
		t.Errorf("echo-to-stderr file missing body:\n%s", s)
	}
	// false: exit=1.
	if s := readFileWithSlug(t, dir, "false"); !strings.Contains(s, "exit=1 ") {
		t.Errorf("false file missing exit=1:\n%s", s)
	}
	// sleep 2: timed out under --timeout 200ms.
	if s := readFileWithSlug(t, dir, "sleep-2"); !strings.Contains(s, "exit=timed-out") {
		t.Errorf("sleep-2 file missing timed-out footer:\n%s", s)
	}
}
