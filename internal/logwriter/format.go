package logwriter

// Frame grammar for the log file. Each record is a single contiguous
// block in the file. Concurrent commands never interleave at the byte
// level: see writer.go's WriteRecord, which holds an exclusive mutex
// while writing the header, body, and footer of one record.
//
//   record  ::= header LF body footer
//   header  ::= "=== begin " ID " · " ISO8601 " · " QUOTED-TEXT " · src=" SOURCE " ===" LF
//   footer  ::= "=== end   " ID " · " ISO8601 " · exit=" EXIT " · dur=" DURATION " ===" LF
//
// ID         a stable per-runner identifier, e.g. "c-0042"
// ISO8601    RFC3339 with nanoseconds in UTC, e.g. "2026-05-27T14:32:00.123456789Z"
// QUOTED-TEXT the command text rendered with Go's %q verb (always single-line)
// SOURCE     one of: cli | file | stdin | socket
// EXIT       printable form of the outcome:
//              "0"           — successful exit
//              "<int>"       — non-zero exit code
//              "signal-N"    — killed by signal N
//              "cancelled"   — context cancelled before completion
//              "timed-out"   — per-command deadline exceeded
//              "spawn-error" — child could not be started (e.g. argv empty)
// DURATION   Go time.Duration string between started and ended
//
// The body is the child's stdout and stderr, interleaved in arrival
// order, written verbatim with no re-encoding.
//
// To extract one command's output from the log file:
//
//   awk '/^=== begin c-0042 /,/^=== end   c-0042 /' cli-executed.log
//
// To list all failures:
//
//   grep '^=== end' cli-executed.log | grep -v 'exit=0 '

// EscapeText returns the command text in a single-line, double-quoted
// form by deferring to Go's %q formatter. This is the same function used
// internally by BuildHeader; it is exported so that callers needing to
// reproduce or parse the frame format have a single source of truth.
func EscapeText(s string) string {
	// Re-use Go's %q verb so escaping stays consistent across header
	// production and any downstream consumer.
	return goQuoted(s)
}

func goQuoted(s string) string {
	// Go's fmt %q is the canonical implementation. Localized helper to
	// avoid pulling fmt into hot paths (BuildHeader already uses fmt).
	const hex = "0123456789abcdef"
	buf := make([]byte, 0, len(s)+2)
	buf = append(buf, '"')
	for _, r := range s {
		switch {
		case r == '"':
			buf = append(buf, '\\', '"')
		case r == '\\':
			buf = append(buf, '\\', '\\')
		case r == '\n':
			buf = append(buf, '\\', 'n')
		case r == '\r':
			buf = append(buf, '\\', 'r')
		case r == '\t':
			buf = append(buf, '\\', 't')
		case r < 0x20 || r == 0x7f:
			buf = append(buf, '\\', 'x', hex[byte(r)>>4], hex[byte(r)&0xf]) //nolint:gosec // G115: r is < 0x80 in this branch, conversion is safe
		default:
			buf = append(buf, []byte(string(r))...)
		}
	}
	buf = append(buf, '"')
	return string(buf)
}
