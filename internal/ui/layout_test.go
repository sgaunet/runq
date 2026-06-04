package ui_test

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/sgaunet/runq/internal/ui"
)

func ptrInt(n int) *int                     { return &n }
func ptrDur(d time.Duration) *time.Duration { return &d }

// TestResolve covers the width-resolution algorithm: fallback, floor, cap,
// mid-range, and the plain/bullets fixed-zone difference (FR-004/005/006).
func TestResolve(t *testing.T) {
	tests := []struct {
		name      string
		termWidth int
		plain     bool
		want      int
	}{
		{"fallback when width unknown (plain)", 0, true, 48},
		{"fallback when width unknown (bullets)", 0, false, 48},
		{"fallback when negative", -1, false, 48},
		{"narrow clamps to floor", 40, false, 20},
		{"wide clamps to cap", 200, false, 72},
		{"mid-range bullets", 100, false, 68}, // 100 - 29 - 3
		{"mid-range plain", 100, true, 55},    // 100 - 45
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ui.Resolve(tc.termWidth, tc.plain)
			if got.CmdWidth != tc.want {
				t.Errorf("Resolve(%d, %v).CmdWidth = %d, want %d", tc.termWidth, tc.plain, got.CmdWidth, tc.want)
			}
			if got.Plain != tc.plain {
				t.Errorf("Resolve(%d, %v).Plain = %v, want %v", tc.termWidth, tc.plain, got.Plain, tc.plain)
			}
		})
	}
	// Plain reserves more fixed width (prefix + status word) than bullets, so
	// its command zone is narrower at the same terminal width.
	if p, b := ui.Resolve(100, true).CmdWidth, ui.Resolve(100, false).CmdWidth; p >= b {
		t.Errorf("plain CmdWidth (%d) should be < bullets CmdWidth (%d)", p, b)
	}
}

// TestRender_ColumnsAlign verifies the exit and duration zones occupy the same
// columns regardless of command length (SC-001 / FR-001 / FR-007). Because every
// zone is fixed width and the right-aligned duration zone ends the line, two
// rows with different command and value widths render to the same total length —
// which means every zone boundary lines up.
func TestRender_ColumnsAlign(t *testing.T) {
	l := ui.Resolve(0, true) // plain, fixed width
	short := l.Render("c-0001", ui.StatusSuccess, "true", ptrInt(0), ptrDur(time.Second), "")
	long := l.Render("c-0002", ui.StatusFailure, "sleep 0.5 && echo done", ptrInt(1), ptrDur(12400*time.Millisecond), "")

	if len(short) != len(long) {
		t.Errorf("rows are not the same width (zones misaligned):\n%q (%d)\n%q (%d)", short, len(short), long, len(long))
	}
	// The exit zone (8 cols) and duration zone (10 cols) are the last two fixed
	// fields; their start columns are a fixed distance from the line end.
	for _, line := range []string{short, long} {
		exitZoneStart := len(line) - durWidthTest - gapTest - exitWidthTest
		if !strings.HasPrefix(line[exitZoneStart:], "  exit=") && !strings.HasPrefix(line[exitZoneStart:], " exit=") {
			t.Errorf("exit zone not where expected in %q", line)
		}
	}
}

// Mirror of the unexported zone constants for offset assertions.
const (
	exitWidthTest = 8
	durWidthTest  = 10
	gapTest       = 2
)

// TestRender_LabelsAndSeparators checks labels are retained and the legacy "·"
// separators are gone (clarify / FR-007).
func TestRender_LabelsAndSeparators(t *testing.T) {
	l := ui.Resolve(0, true)
	line := l.Render("c-0001", ui.StatusSuccess, "echo hi", ptrInt(0), ptrDur(time.Second), "")
	if !strings.Contains(line, "exit=0") || !strings.Contains(line, "dur=") {
		t.Errorf("missing exit=/dur= labels: %q", line)
	}
	if strings.Contains(line, "·") {
		t.Errorf("legacy '·' separator present: %q", line)
	}
}

// TestRender_Placeholders checks each StatusKind renders the em-dash where a
// value is absent, while present values are shown (FR-008 / clarify).
func TestRender_Placeholders(t *testing.T) {
	l := ui.Resolve(0, true)
	dur := 30 * time.Second

	queued := l.Render("c-0001", ui.StatusQueued, "x", nil, nil, "")
	if strings.Contains(queued, "exit=") || strings.Contains(queued, "dur=") {
		t.Errorf("queued row should have no exit=/dur=: %q", queued)
	}
	if c := strings.Count(queued, "—"); c != 2 {
		t.Errorf("queued row should have 2 em-dash placeholders, got %d: %q", c, queued)
	}

	timed := l.Render("c-0002", ui.StatusTimedOut, "sleep 99", nil, &dur, "")
	if !strings.Contains(timed, "dur=") {
		t.Errorf("timed-out row should show dur=: %q", timed)
	}
	if c := strings.Count(timed, "—"); c != 1 { // exit absent only
		t.Errorf("timed-out row should have 1 em-dash (exit), got %d: %q", c, timed)
	}
}

// TestRender_BaseName checks the program directory is stripped while arguments
// are preserved verbatim (FR-002 / SC-003).
func TestRender_BaseName(t *testing.T) {
	l := ui.Layout{CmdWidth: 60, Plain: true}
	line := l.Render("c-0001", ui.StatusSuccess, `/usr/local/bin/git commit -m "x"`, ptrInt(0), ptrDur(time.Second), "")
	if !strings.Contains(line, `git commit -m "x"`) {
		t.Errorf("expected base-name command, got %q", line)
	}
	if strings.Contains(line, "/usr/local/bin") {
		t.Errorf("directory path should be stripped: %q", line)
	}
}

// TestRender_Truncation checks head-truncation with the ellipsis, including that
// a multi-byte (wide) command is never split mid-rune (FR-003 / FR-009).
func TestRender_Truncation(t *testing.T) {
	// ASCII overflow: ends with the ellipsis, no path leaks.
	l := ui.Layout{CmdWidth: 12, Plain: false}
	line := l.Render("c-0001", ui.StatusSuccess, "/bin/go test ./internal/runner/exec_test.go", ptrInt(0), ptrDur(time.Second), "")
	if !strings.Contains(line, "…") {
		t.Errorf("expected ellipsis in truncated line: %q", line)
	}

	// Multi-byte overflow: keep CmdWidth-1 wide runes + ellipsis; stay valid UTF-8.
	wide := strings.Repeat("世", 100)
	wl := ui.Layout{CmdWidth: 10, Plain: false}
	wline := wl.Render("c-0001", ui.StatusSuccess, wide, ptrInt(0), ptrDur(time.Second), "")
	if !utf8.ValidString(wline) {
		t.Errorf("truncation split a multi-byte rune (invalid UTF-8): %q", wline)
	}
	if got := strings.Count(wline, "世"); got != 9 {
		t.Errorf("expected 9 wide runes kept (CmdWidth-1), got %d: %q", got, wline)
	}
	if !strings.Contains(wline, "…") {
		t.Errorf("expected ellipsis after wide truncation: %q", wline)
	}
}

// TestRender_Fits checks a command within the zone is shown in full, no ellipsis.
func TestRender_Fits(t *testing.T) {
	l := ui.Layout{CmdWidth: 40, Plain: true}
	line := l.Render("c-0001", ui.StatusSuccess, "echo hello", ptrInt(0), ptrDur(time.Second), "")
	if strings.Contains(line, "…") {
		t.Errorf("short command should not be truncated: %q", line)
	}
	if !strings.Contains(line, "echo hello") {
		t.Errorf("missing full command: %q", line)
	}
}

// TestRender_SpawnErrorNote checks the spawn-error message is appended as a
// trailing, bounded note while the zones stay aligned (FR-008 edge / C1).
func TestRender_SpawnErrorNote(t *testing.T) {
	l := ui.Resolve(0, true)
	queued := l.Render("c-0001", ui.StatusQueued, "true", nil, nil, "")
	spawn := l.Render("c-0002", ui.StatusSpawnError, "nope", nil, nil, "err=exec: \"nope\": not found")
	if !strings.Contains(spawn, "err=") {
		t.Errorf("spawn-error note missing: %q", spawn)
	}
	// The fixed-zone portion of the spawn-error row (everything before the
	// appended " err=" note) must be byte-identical in shape to a queued row of
	// the same command: same prefix length and same placeholder columns.
	fixed := spawn[:strings.Index(spawn, " err=")]
	if len(fixed) != len(queued) {
		t.Errorf("spawn-error fixed zones differ in width from a queued row:\n%q (%d)\n%q (%d)",
			fixed, len(fixed), queued, len(queued))
	}
	if strings.Index(fixed, "—") != strings.Index(queued, "—") ||
		strings.LastIndex(fixed, "—") != strings.LastIndex(queued, "—") {
		t.Errorf("spawn-error placeholders misaligned with a queued row:\n%q\n%q", fixed, queued)
	}
}

// TestRender_NoEscapeSequences verifies runq emits no ANSI escapes itself, so
// width math equals visible width (FR-012). Color is applied by the UI library.
func TestRender_NoEscapeSequences(t *testing.T) {
	l := ui.Resolve(0, true)
	line := l.Render("c-0001", ui.StatusFailure, "false", ptrInt(1), ptrDur(time.Second), "")
	if strings.ContainsRune(line, 0x1b) {
		t.Errorf("Render output contains an ESC byte: %q", line)
	}
}
