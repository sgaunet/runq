package integration

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

// TestHelp_ContainsContractStrings verifies that --help advertises the
// full CLI contract: every documented flag, every exit code, the
// stdout/stderr callout, and the shell-mode security warning.
//
// This is a doc-drift guard: if a future change removes one of these
// from --help, the test fails before docs and reality silently diverge.
func TestHelp_ContainsContractStrings(t *testing.T) {
	bin := binary(t)
	cmd := exec.Command(bin, "--help")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("--help failed: %v; stderr=%q", err, stderr.String())
	}
	out := stdout.String() + stderr.String()

	// Every advertised flag must appear in the help text.
	flags := []string{
		"--parallel", "--no-shell", "--timeout", "--kill-grace",
		"--max-queue", "--log-dir", "--socket", "--from-file",
		"--from-stdin", "--output", "--quiet", "--verbose", "--no-color",
	}
	for _, f := range flags {
		if !strings.Contains(out, f) {
			t.Errorf("--help missing flag %q", f)
		}
	}

	// Every advertised exit code must appear.
	for _, e := range []string{"0  ", "1  ", "2  ", "10 ", "11 ", "12 ", "13 ", "14 "} {
		if !strings.Contains(out, e) {
			t.Errorf("--help missing exit code line starting with %q", e)
		}
	}

	// Stream contract callout must be present.
	for _, s := range []string{"stdout", "stderr"} {
		if !strings.Contains(out, s) {
			t.Errorf("--help missing stream callout for %q", s)
		}
	}

	// Security warning about shell mode must be present.
	if !strings.Contains(out, "Security warning") && !strings.Contains(out, "security") {
		t.Errorf("--help missing shell-mode security warning")
	}

	// Runner/forwarder roles must be described.
	for _, role := range []string{"Runner", "Forwarder"} {
		if !strings.Contains(out, role) {
			t.Errorf("--help missing role description for %q", role)
		}
	}
}
