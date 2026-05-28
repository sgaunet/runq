package logwriter_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sgaunet/runq/internal/logwriter"
)

func TestOpen_AndWriteRecord(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log.log")
	w, chosen, err := logwriter.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if chosen != path {
		t.Errorf("chosen = %q, want %q", chosen, path)
	}
	defer w.Close()

	header := logwriter.BuildHeader("c-0001", `echo "hi"`, "cli", time.Unix(0, 0).UTC())
	footer := logwriter.BuildFooter("c-0001", time.Unix(1, 0).UTC(), "0", time.Second)
	if err := w.WriteRecord(header, footer, strings.NewReader("hi\n")); err != nil {
		t.Fatalf("WriteRecord: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Contains(got, []byte(`=== begin c-0001`)) {
		t.Errorf("missing begin header in %q", got)
	}
	if !bytes.Contains(got, []byte(`=== end   c-0001`)) {
		t.Errorf("missing end footer in %q", got)
	}
	if !bytes.Contains(got, []byte(`\"hi\"`)) {
		t.Errorf("header did not escape quotes; got %q", got)
	}
	if !bytes.Contains(got, []byte("hi\n=== end")) {
		t.Errorf("body not contiguous with footer in %q", got)
	}
}

func TestOpen_AutoUniquifyOnLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log.log")

	w1, chosen1, err := logwriter.Open(path)
	if err != nil {
		t.Fatalf("Open #1: %v", err)
	}
	defer w1.Close()
	if chosen1 != path {
		t.Errorf("first chosen = %q, want %q", chosen1, path)
	}

	w2, chosen2, err := logwriter.Open(path)
	if err != nil {
		t.Fatalf("Open #2: %v", err)
	}
	defer w2.Close()
	if chosen2 == path {
		t.Errorf("second chosen = %q, want uniquified", chosen2)
	}
	if !strings.Contains(chosen2, ".1.") {
		t.Errorf("second chosen = %q, expected .1. suffix", chosen2)
	}
}

func TestWriteRecord_ConcurrentContiguity(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log.log")
	w, _, err := logwriter.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer w.Close()

	const N = 50
	var wg sync.WaitGroup
	for i := range N {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := "c-" + padInt(i, 4)
			body := strings.Repeat("X", 1024) // 1 KiB
			header := logwriter.BuildHeader(id, "test", "cli", time.Unix(0, 0))
			footer := logwriter.BuildFooter(id, time.Unix(1, 0), "0", time.Second)
			if err := w.WriteRecord(header, footer, strings.NewReader(body)); err != nil {
				t.Errorf("WriteRecord: %v", err)
			}
		}(i)
	}
	wg.Wait()
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	// Each record's body (1 KiB of 'X') must appear between matching begin
	// and end markers — never interleaved with another record's marker.
	records := strings.Split(string(data), "=== begin ")
	if len(records)-1 != N {
		t.Errorf("found %d begin markers, want %d", len(records)-1, N)
	}
	for i, rec := range records {
		if i == 0 {
			continue // before first marker
		}
		if !strings.Contains(rec, "=== end") {
			t.Errorf("record %d missing end marker: %q", i, head(rec, 60))
		}
		// Between header line and footer there should be exactly 1024 X's
		// preceded by a newline and followed by a newline.
		body := rec[strings.Index(rec, "\n")+1:]
		body = body[:strings.Index(body, "=== end")]
		body = strings.TrimRight(body, "\n")
		if body != strings.Repeat("X", 1024) {
			t.Errorf("record %d body interleaved: len=%d", i, len(body))
			break
		}
	}
}

func padInt(n, width int) string {
	s := ""
	if n == 0 {
		s = "0"
	}
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	for len(s) < width {
		s = "0" + s
	}
	return s
}

func head(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
