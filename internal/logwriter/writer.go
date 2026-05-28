// Package logwriter appends framed per-command records to a single file.
// Each record (begin header + body + end footer) is committed under a
// single mutex so concurrent commands never interleave at the byte level.
package logwriter

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// Writer is an append-only framed log writer. A Writer is safe for
// concurrent use by multiple goroutines.
type Writer struct {
	path string
	mu   sync.Mutex
	f    *os.File
}

// Open creates or appends to the file at path. If a sibling lockfile shows
// another live writer is already holding this path, Open chooses a unique
// path by appending a numeric suffix (cli-executed.1.log, .2.log, …) and
// returns the path it ultimately picked.
//
// The lockfile is path+".lock" and is removed on Close.
func Open(path string) (*Writer, string, error) {
	chosen := path
	for i := range 100 {
		w, err := tryOpen(chosen)
		if err == nil {
			return w, chosen, nil
		}
		if !isPathInUse(err) {
			return nil, "", err
		}
		chosen = uniquify(path, i+1)
	}
	return nil, "", fmt.Errorf("could not open a unique log file based on %q (100 attempts)", path)
}

// tryOpen acquires the lock and opens the file in append mode.
func tryOpen(path string) (*Writer, error) {
	lockPath := path + ".lock"
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) //nolint:gosec // G304: log path is operator-supplied by design
	if err != nil {
		return nil, err
	}
	_ = lock.Close()

	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // G304: log path is operator-supplied by design
	if err != nil {
		_ = os.Remove(lockPath)
		return nil, err
	}
	return &Writer{path: path, f: f}, nil
}

func isPathInUse(err error) bool {
	// O_EXCL on the lockfile returns os.ErrExist when another writer holds it.
	return os.IsExist(err)
}

func uniquify(base string, n int) string {
	// Insert ".N" before the extension if present, otherwise append.
	if dot := strings.LastIndex(base, "."); dot > 0 && dot < len(base)-1 {
		return fmt.Sprintf("%s.%d%s", base[:dot], n, base[dot:])
	}
	return fmt.Sprintf("%s.%d", base, n)
}

// Close flushes, closes the file, and releases the lockfile.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	var err error
	if w.f != nil {
		err = w.f.Close()
		w.f = nil
	}
	_ = os.Remove(w.path + ".lock")
	return err
}

// WriteRecord writes one framed record. body is copied verbatim. The
// caller has already escaped the header/footer for single-line shape.
func (w *Writer) WriteRecord(header, footer []byte, body io.Reader) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return fmt.Errorf("logwriter: closed")
	}
	if _, err := w.f.Write(header); err != nil {
		return err
	}
	if body != nil {
		if _, err := io.Copy(w.f, body); err != nil {
			return err
		}
	}
	if _, err := w.f.Write(footer); err != nil {
		return err
	}
	return nil
}

// BuildHeader produces the "=== begin ..." line including a trailing
// newline. The command text is rendered with %q, which yields a single-line
// Go-quoted string that handles backslashes, quotes, and non-printable
// characters in one pass.
func BuildHeader(id, text string, source string, start time.Time) []byte {
	return fmt.Appendf(nil, "=== begin %s · %s · %q · src=%s ===\n",
		id, start.UTC().Format(time.RFC3339Nano), text, source)
}

// BuildFooter produces the "=== end ..." line including a trailing
// newline. exitField is the printable representation (e.g. "0", "1",
// "signal-15", "cancelled", "timed-out", "spawn-error").
func BuildFooter(id string, end time.Time, exitField string, dur time.Duration) []byte {
	return fmt.Appendf(nil, "=== end   %s · %s · exit=%s · dur=%s ===\n",
		id, end.UTC().Format(time.RFC3339Nano), exitField, dur)
}
