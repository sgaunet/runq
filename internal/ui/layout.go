package ui

import (
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Zone-budget constants for the aligned status line. See
// specs/004-align-command-output/data-model.md. Every status line is a fixed
// sequence of zones; only the command zone width varies (adaptive on a TTY,
// fixed when piped), so the exit and duration columns stack vertically.
const (
	plainPrefix = "runq: " // emitted by the plain (non-TTY) layout before the id

	idWidth     = 6  // fits c-0001..c-9999 (idGen format c-NNNN)
	statusWidth = 9  // longest plain label "SPAWN-ERR"
	exitWidth   = 8  // "exit=255" — exit codes 0..255
	durWidth    = 10 // "dur=1h2m3s"
	gap         = 2  // spaces between command/exit and exit/duration zones

	minCmd      = 20 // command-zone floor on narrow terminals (FR-005)
	maxCmd      = 72 // command-zone cap so columns don't drift wide (FR-004)
	fallbackCmd = 48 // fixed command width when terminal width is unknown (FR-006)

	bulletsPrefixReserve = 3 // allowance for the spinner glyph the library draws

	ellipsis    = "…" // truncation marker (one column, U+2026)
	placeholder = "—" // absent value (U+2014)
	noteMax     = 40  // head-truncate a spawn-error note so it can't break layout
)

// StatusKind enumerates the lifecycle states a status line can represent.
type StatusKind int

// Lifecycle states, mirroring the Sink event methods.
const (
	StatusQueued StatusKind = iota
	StatusStarted
	StatusSuccess
	StatusFailure
	StatusCancelled
	StatusTimedOut
	StatusSpawnError
)

func statusLabel(st StatusKind) string {
	switch st {
	case StatusQueued:
		return "QUEUED"
	case StatusStarted:
		return "STARTED"
	case StatusSuccess:
		return "OK"
	case StatusFailure:
		return "FAILED"
	case StatusCancelled:
		return "CANCEL"
	case StatusTimedOut:
		return "TIMEOUT"
	case StatusSpawnError:
		return "SPAWN-ERR"
	default:
		return ""
	}
}

// Layout holds the resolved zone budget for one run and renders status lines.
// It is a value type; both the plain and bullets sinks hold a copy.
type Layout struct {
	CmdWidth int  // resolved width of the command zone
	Plain    bool // true: "runq: <id> STATUS ..." prefix; false: bullets label
}

// fixedZonesTotal is the combined width of every non-command zone, used to size
// the command zone against the available terminal width. It must match the zone
// assembly in Render.
func fixedZonesTotal(plain bool) int {
	// id + ' ' + gap-before-exit + exit + gap + duration
	total := idWidth + 1 + gap + exitWidth + gap + durWidth
	if plain {
		// prefix + status label + the space after it
		total += len(plainPrefix) + statusWidth + 1
	}
	return total
}

// Resolve computes the command-zone width for the given terminal width. A
// non-positive termWidth (output is not a terminal) selects the fixed fallback,
// giving deterministic, byte-stable piped output (FR-006). Otherwise the command
// zone grows with the terminal up to maxCmd and never below minCmd (FR-004/005).
func Resolve(termWidth int, plain bool) Layout {
	l := Layout{Plain: plain}
	if termWidth <= 0 {
		l.CmdWidth = fallbackCmd
		return l
	}
	avail := termWidth - fixedZonesTotal(plain)
	if !plain {
		avail -= bulletsPrefixReserve
	}
	switch {
	case avail < minCmd:
		l.CmdWidth = minCmd
	case avail > maxCmd:
		l.CmdWidth = maxCmd
	default:
		l.CmdWidth = avail
	}
	return l
}

// Render assembles one aligned status line. exit and dur are nil for states that
// have no such value (rendered as a right-aligned em-dash placeholder). note is
// appended after the duration zone when non-empty (the spawn-error message) and
// is head-truncated so it cannot break the column layout.
func (l Layout) Render(id string, st StatusKind, command string, exit *int, dur *time.Duration, note string) string {
	cmd := truncateWidth(program(command), l.CmdWidth)

	var b strings.Builder
	if l.Plain {
		b.WriteString(plainPrefix)
		b.WriteString(padRight(id, idWidth))
		b.WriteByte(' ')
		b.WriteString(padRight(statusLabel(st), statusWidth))
		b.WriteByte(' ')
	} else {
		b.WriteString(padRight(id, idWidth))
		b.WriteByte(' ')
	}
	b.WriteString(padRight(cmd, l.CmdWidth))
	b.WriteString(strings.Repeat(" ", gap))
	b.WriteString(alignRight(exitField(exit), exitWidth))
	b.WriteString(strings.Repeat(" ", gap))
	b.WriteString(alignRight(durField(dur), durWidth))
	if note != "" {
		b.WriteByte(' ')
		b.WriteString(truncateWidth(note, noteMax))
	}
	return b.String()
}

func exitField(exit *int) string {
	if exit == nil {
		return placeholder
	}
	return "exit=" + strconv.Itoa(*exit)
}

func durField(dur *time.Duration) string {
	if dur == nil {
		return placeholder
	}
	return "dur=" + dur.Round(time.Millisecond).String()
}

// program returns command with the leading program's directory stripped, so
// "/usr/local/bin/git commit" displays as "git commit". Arguments are kept
// verbatim (original spacing preserved); a first token without a path separator
// is returned unchanged. Empty input yields an empty string.
func program(command string) string {
	trimmed := strings.TrimLeft(command, " \t")
	if trimmed == "" {
		return ""
	}
	prog := trimmed
	rest := ""
	if i := strings.IndexAny(trimmed, " \t"); i >= 0 {
		prog = trimmed[:i]
		rest = trimmed[i:] // separating whitespace + args, verbatim
	}
	return baseName(prog) + rest
}

// baseName returns the final path segment of prog (everything after the last
// '/'), handling a trailing slash by falling back to the segment before it.
func baseName(prog string) string {
	i := strings.LastIndexByte(prog, '/')
	if i < 0 {
		return prog
	}
	if base := prog[i+1:]; base != "" {
		return base
	}
	trimmed := strings.TrimRight(prog, "/")
	if j := strings.LastIndexByte(trimmed, '/'); j >= 0 {
		return trimmed[j+1:]
	}
	return trimmed
}

// truncateWidth head-truncates s to n display cells (counted in runes),
// appending an ellipsis when it overflows. It never splits a multi-byte
// character. A string that already fits is returned unchanged.
func truncateWidth(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	if n < 2 {
		// No room for content plus the marker; emit just the marker.
		return ellipsis
	}
	runes := []rune(s)
	return string(runes[:n-1]) + ellipsis
}

// padRight left-aligns s in a field of n cells, padding with spaces. A string
// at or beyond n cells is returned unchanged.
func padRight(s string, n int) string {
	if w := utf8.RuneCountInString(s); w < n {
		return s + strings.Repeat(" ", n-w)
	}
	return s
}

// alignRight right-aligns s in a field of n cells, padding with spaces. A string
// at or beyond n cells is returned unchanged.
func alignRight(s string, n int) string {
	if w := utf8.RuneCountInString(s); w < n {
		return strings.Repeat(" ", n-w) + s
	}
	return s
}
