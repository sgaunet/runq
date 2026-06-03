package logwriter

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Run is a per-invocation log destination: a directory holding one file per
// command. Create one with OpenRun, obtain a Record per command via NewRecord,
// and Close it when the run completes. A Run is safe for concurrent use —
// each NewRecord opens an independent file, so there is no shared write path
// to serialize and no cross-command byte interleaving is possible.
type Run struct {
	dir string
}

// Dir returns the per-run directory path.
func (r *Run) Dir() string { return r.dir }

// OpenRun creates the per-run subdirectory
//
//	<baseDir>/<run-ts>_run-<rand>
//
// (run-ts in local time) and returns a Run that writes per-command files into
// it. baseDir and all parents are created with mode 0700.
func OpenRun(baseDir string, runStart time.Time) (*Run, error) {
	rnd, err := randomHex(runRandBytes)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(baseDir, runStart.Format(fileTimeLayout)+"_run-"+rnd)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create log directory %s: %w", dir, err)
	}
	return &Run{dir: dir}, nil
}

// Close releases Run-level resources. Per-command files are owned and closed
// by their Record; this is currently a no-op kept for symmetry.
func (r *Run) Close() error { return nil }

// maxNewRecordAttempts bounds the retry loop in NewRecord. A collision requires
// two goroutines to draw the same 32-bit random id at the same second for the
// same slug — vanishingly rare in practice; 100 attempts is more than enough.
const maxNewRecordAttempts = 100

// NewRecord opens a fresh per-command log file in the run directory, writes the
// framed header, and returns a Record. The caller streams the command's
// stdout+stderr to the Record (an io.Writer), then calls Finish exactly once.
//
// The file is opened with O_EXCL so that a collision (same timestamp + slug +
// random id) is detected rather than silently producing an interleaved file.
// On collision NewRecord regenerates the random id and retries up to
// maxNewRecordAttempts times before returning an error.
func (r *Run) NewRecord(id, text, source string, start time.Time) (*Record, error) {
	for range maxNewRecordAttempts {
		rid, err := randomID()
		if err != nil {
			return nil, err
		}
		path := filepath.Join(r.dir, FileName(start, text, rid))
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) //nolint:gosec // G304: log path derives from an operator-supplied command by design
		if os.IsExist(err) {
			// True id collision: regenerate and retry.
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("create log file %s: %w", path, err)
		}
		if _, err := f.Write(BuildHeader(id, text, source, start)); err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("write log header %s: %w", path, err)
		}
		return &Record{path: path, id: id, f: f}, nil
	}
	return nil, fmt.Errorf("create log file: exhausted %d attempts resolving id collisions", maxNewRecordAttempts)
}

// Record is a single command's log file. It implements io.Writer for the
// command body (stdout+stderr, streamed in arrival order). Call Finish exactly
// once to write the footer and close the file.
type Record struct {
	path string
	id   string
	f    *os.File
}

// Path returns this record's file path.
func (rec *Record) Path() string { return rec.path }

// Write appends body bytes to the file. It satisfies io.Writer so the executor
// can stream child output directly to disk without buffering it in memory.
func (rec *Record) Write(p []byte) (int, error) {
	if rec.f == nil {
		return 0, fmt.Errorf("logwriter: record %s already finished", rec.id)
	}
	return rec.f.Write(p)
}

// Finish writes the framed footer and closes the file. After Finish the Record
// must not be used again.
func (rec *Record) Finish(end time.Time, exitField string, dur time.Duration) error {
	if rec.f == nil {
		return fmt.Errorf("logwriter: record %s already finished", rec.id)
	}
	_, werr := rec.f.Write(BuildFooter(rec.id, end, exitField, dur))
	cerr := rec.f.Close()
	rec.f = nil
	if werr != nil {
		return fmt.Errorf("write log footer %s: %w", rec.path, werr)
	}
	if cerr != nil {
		return fmt.Errorf("close log file %s: %w", rec.path, cerr)
	}
	return nil
}
