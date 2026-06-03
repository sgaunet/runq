package logwriter_test

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sgaunet/runq/internal/logwriter"
)

func TestOpenRun_CreatesPerRunDir(t *testing.T) {
	base := filepath.Join(t.TempDir(), "logs")
	run, err := logwriter.OpenRun(base, time.Date(2026, 5, 28, 14, 30, 0, 0, time.Local))
	if err != nil {
		t.Fatalf("OpenRun: %v", err)
	}
	defer run.Close()

	info, err := os.Stat(run.Dir())
	if err != nil {
		t.Fatalf("run dir not created: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("run dir is not a directory")
	}
	if filepath.Dir(run.Dir()) != base {
		t.Errorf("run dir %q not under base %q", run.Dir(), base)
	}
	if !strings.Contains(filepath.Base(run.Dir()), "_run-") {
		t.Errorf("run dir name %q missing _run- segment", filepath.Base(run.Dir()))
	}
}

func TestRecord_HeaderBodyFooter(t *testing.T) {
	run, err := logwriter.OpenRun(t.TempDir(), time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	defer run.Close()

	start := time.Unix(0, 0).UTC()
	rec, err := run.NewRecord("c-0001", `echo "hi"`, "cli", start)
	if err != nil {
		t.Fatalf("NewRecord: %v", err)
	}
	if _, err := rec.Write([]byte("hi\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := rec.Finish(time.Unix(1, 0).UTC(), "0", time.Second); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	data, err := os.ReadFile(rec.Path())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "=== begin c-0001") {
		t.Errorf("missing begin header: %q", s)
	}
	if !strings.Contains(s, "=== end   c-0001") {
		t.Errorf("missing end footer: %q", s)
	}
	if !strings.Contains(s, `\"hi\"`) {
		t.Errorf("header did not escape quotes: %q", s)
	}
	if !strings.Contains(s, "hi\n=== end") {
		t.Errorf("body not contiguous with footer: %q", s)
	}
}

// TestRecord_LargeBodyStreams asserts a Record accepts a 10 MiB body without
// excessive latency — i.e. it streams rather than buffering.
func TestRecord_LargeBodyStreams(t *testing.T) {
	run, err := logwriter.OpenRun(t.TempDir(), time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	defer run.Close()

	rec, err := run.NewRecord("c-0001", "noisy", "cli", time.Unix(0, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	const size = 10 * 1024 * 1024
	body := make([]byte, size)
	if _, err := rand.Read(body); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, err := rec.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := rec.Finish(time.Unix(0, 0).UTC(), "0", time.Second); err != nil {
		t.Fatal(err)
	}
	if dur := time.Since(start); dur > 5*time.Second {
		t.Errorf("10 MiB write took %v, suspiciously slow", dur)
	}
	info, err := os.Stat(rec.Path())
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() < int64(size) {
		t.Errorf("file size = %d, want >= %d", info.Size(), size)
	}
}

// TestNewRecord_RetryOnCollision verifies that NewRecord retries when an
// O_EXCL collision occurs. We pre-create 200 FileName-shaped files whose names
// exhaust a swath of the id space by seeding the run directory with files
// matching FileName(start, text, <hex-id>). Because randomID draws from 2^32
// space and we only block 200 names the test cannot guarantee a retry happens —
// but it does prove that pre-existing files never cause NewRecord to fail or to
// silently open the same file for writing.
func TestNewRecord_RetryOnCollision(t *testing.T) {
	run, err := logwriter.OpenRun(t.TempDir(), time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	defer run.Close()

	const (
		cmdText = "echo hi"
		N       = 200 // pre-seeded files
		workers = 50  // concurrent NewRecord calls
	)
	start := time.Unix(0, 0).UTC()

	// Pre-create N files whose names match the FileName shape (random ids).
	for i := range N {
		id := fmt.Sprintf("%08x", i)
		name := logwriter.FileName(start, cmdText, id)
		p := filepath.Join(run.Dir(), name)
		f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatalf("pre-seed file %d: %v", i, err)
		}
		_ = f.Close()
	}

	// Now run workers concurrent NewRecord calls and confirm each succeeds
	// with a distinct path that did NOT overwrite a pre-seeded file.
	var mu sync.Mutex
	paths := make(map[string]struct{}, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec, err := run.NewRecord("c-0001", cmdText, "cli", start)
			if err != nil {
				t.Errorf("NewRecord: %v", err)
				return
			}
			if err := rec.Finish(start, "0", time.Second); err != nil {
				t.Errorf("Finish: %v", err)
			}
			mu.Lock()
			paths[rec.Path()] = struct{}{}
			mu.Unlock()
		}()
	}
	wg.Wait()

	if len(paths) != workers {
		t.Errorf("distinct paths = %d, want %d (collision not resolved)", len(paths), workers)
	}
	// Every file opened by NewRecord must contain a header (not be a pre-seeded empty file).
	for p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(data, []byte("=== begin")) {
			t.Errorf("file %s missing header — may be pre-seeded file opened for writing", p)
		}
	}
}

// TestNewRecord_DistinctFilesForIdenticalText verifies the random id makes
// identical command text yield distinct, non-overwriting files — including
// when records are created concurrently.
func TestNewRecord_DistinctFilesForIdenticalText(t *testing.T) {
	run, err := logwriter.OpenRun(t.TempDir(), time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	defer run.Close()

	const N = 64
	start := time.Unix(0, 0).UTC()
	var mu sync.Mutex
	paths := make(map[string]struct{}, N)
	var wg sync.WaitGroup
	for range N {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec, err := run.NewRecord("c-0001", "sleep 50", "cli", start)
			if err != nil {
				t.Errorf("NewRecord: %v", err)
				return
			}
			if _, err := rec.Write([]byte("body\n")); err != nil {
				t.Errorf("Write: %v", err)
			}
			if err := rec.Finish(start, "0", time.Second); err != nil {
				t.Errorf("Finish: %v", err)
			}
			mu.Lock()
			paths[rec.Path()] = struct{}{}
			mu.Unlock()
		}()
	}
	wg.Wait()

	if len(paths) != N {
		t.Errorf("distinct file paths = %d, want %d (id collision)", len(paths), N)
	}
	entries, err := os.ReadDir(run.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != N {
		t.Errorf("files on disk = %d, want %d", len(entries), N)
	}
	// Every file must contain the full body (none overwritten/truncated).
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(run.Dir(), e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(data, []byte("body\n=== end")) {
			t.Errorf("file %s missing complete body: %q", e.Name(), data)
		}
	}
}
