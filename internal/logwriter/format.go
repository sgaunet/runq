// Package logwriter writes one framed log file per executed command into a
// per-run directory (see Run/Record in run.go). Each file contains a single
// contiguous record — a begin header, the command's stdout+stderr streamed
// verbatim, and an end footer. Because every command owns its own file, no
// cross-command byte interleaving is possible.
package logwriter

import (
	"fmt"
	"time"
)

// Frame grammar for each per-command file. One record per file:
//
//	record  ::= header LF body footer
//	header  ::= "=== begin " ID " · " ISO8601 " · " QUOTED-TEXT " · src=" SOURCE " ===" LF
//	footer  ::= "=== end   " ID " · " ISO8601 " · exit=" EXIT " · dur=" DURATION " ===" LF
//
// ID          a stable per-runner identifier, e.g. "c-0042"
// ISO8601     RFC3339 with nanoseconds in UTC, e.g. "2026-05-28T14:32:00.123456789Z"
//             (the file NAME carries a local-time timestamp; the header is UTC by design)
// QUOTED-TEXT the command text rendered with Go's %q verb (always single-line)
// SOURCE      one of: cli | file | stdin | socket
// EXIT        printable form of the outcome:
//               "0"           — successful exit
//               "<int>"       — non-zero exit code
//               "signal-N"    — killed by signal N
//               "cancelled"   — context cancelled before completion
//               "timed-out"   — per-command deadline exceeded
//               "spawn-error" — child could not be started (e.g. argv empty)
// DURATION    Go time.Duration string between started and ended
//
// The body is the child's stdout and stderr, interleaved in arrival order,
// written verbatim with no re-encoding. To read one command's output, just
// cat its file — the whole file is that command's record.

// BuildHeader produces the "=== begin ..." line including a trailing newline.
// The command text is rendered with %q, which yields a single-line Go-quoted
// string that handles backslashes, quotes, and non-printable characters in one
// pass.
func BuildHeader(id, text string, source string, start time.Time) []byte {
	return fmt.Appendf(nil, "=== begin %s · %s · %q · src=%s ===\n",
		id, start.UTC().Format(time.RFC3339Nano), text, source)
}

// BuildFooter produces the "=== end ..." line including a trailing newline.
// exitField is the printable representation (e.g. "0", "1", "signal-15",
// "cancelled", "timed-out", "spawn-error").
func BuildFooter(id string, end time.Time, exitField string, dur time.Duration) []byte {
	return fmt.Appendf(nil, "=== end   %s · %s · exit=%s · dur=%s ===\n",
		id, end.UTC().Format(time.RFC3339Nano), exitField, dur)
}

// EscapeText returns the command text in a single-line, double-quoted form
// using Go's %q formatter. This handles backslashes, double-quotes,
// non-printable characters, and invalid UTF-8 bytes identically to BuildHeader.
// Exported so downstream consumers that parse the frame format share one source
// of truth.
func EscapeText(s string) string {
	return fmt.Sprintf("%q", s)
}
