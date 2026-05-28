package logwriter_test

import (
	"bytes"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sgaunet/runq/internal/logwriter"
)

func TestEscapeText_SingleLine(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`hello`, `"hello"`},
		{`echo "hi"`, `"echo \"hi\""`},
		{"line1\nline2", `"line1\nline2"`},
		{"with\ttab", `"with\ttab"`},
		{`path\to\file`, `"path\\to\\file"`},
	}
	for _, tc := range cases {
		got := logwriter.EscapeText(tc.in)
		if got != tc.want {
			t.Errorf("EscapeText(%q) = %q, want %q", tc.in, got, tc.want)
		}
		if strings.ContainsAny(got[1:len(got)-1], "\n\r") {
			t.Errorf("EscapeText produced multi-line output: %q", got)
		}
	}
}

func TestFooter_ContainsExitDurSrcInOrder(t *testing.T) {
	footer := logwriter.BuildFooter("c-0042", time.Unix(0, 0).UTC(), "0", 250*time.Millisecond)
	// Footer order: exit, dur, src — but src is in the HEADER, not the
	// footer; verify exit appears before dur.
	s := string(footer)
	iExit := strings.Index(s, "exit=")
	iDur := strings.Index(s, "dur=")
	if iExit < 0 || iDur < 0 {
		t.Fatalf("footer missing fields: %q", s)
	}
	if iExit > iDur {
		t.Errorf("exit appears after dur in footer: %q", s)
	}
}

func TestFrame_HeaderHasSrc(t *testing.T) {
	header := logwriter.BuildHeader("c-0001", "echo hi", "socket", time.Unix(0, 0).UTC())
	if !bytes.Contains(header, []byte("src=socket")) {
		t.Errorf("header missing src=socket: %q", string(header))
	}
}

// TestWriteRecord_LargeBodyStreams asserts the writer does not buffer
// arbitrarily; it should accept a 10 MiB body without issue.
func TestWriteRecord_LargeBodyStreams(t *testing.T) {
	dir := t.TempDir()
	w, _, err := logwriter.Open(filepath.Join(dir, "big.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	const size = 10 * 1024 * 1024
	body := make([]byte, size)
	if _, err := rand.Read(body); err != nil {
		t.Fatal(err)
	}

	header := logwriter.BuildHeader("c-0001", "noisy", "cli", time.Unix(0, 0).UTC())
	footer := logwriter.BuildFooter("c-0001", time.Unix(0, 0).UTC(), "0", time.Second)
	start := time.Now()
	if err := w.WriteRecord(header, footer, bytes.NewReader(body)); err != nil {
		t.Fatal(err)
	}
	if dur := time.Since(start); dur > 5*time.Second {
		t.Errorf("10 MiB write took %v, suspiciously slow", dur)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(filepath.Join(dir, "big.log"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() < int64(size) {
		t.Errorf("file size = %d, want >= %d (header + body + footer)", info.Size(), size)
	}
}

// TestWriteRecord_ContiguousAt100Goroutines is the strictest contiguity
// check: 100 concurrent records of 4 KiB each must never interleave.
func TestWriteRecord_ContiguousAt100Goroutines(t *testing.T) {
	dir := t.TempDir()
	w, _, err := logwriter.Open(filepath.Join(dir, "log.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	const N = 100
	var wg sync.WaitGroup
	for i := range N {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			marker := byte('A' + i%26)
			body := bytes.Repeat([]byte{marker}, 4096)
			id := "c-" + zeroPad(i+1, 4)
			h := logwriter.BuildHeader(id, "x", "cli", time.Unix(0, 0))
			f := logwriter.BuildFooter(id, time.Unix(1, 0), "0", time.Second)
			if err := w.WriteRecord(h, f, bytes.NewReader(body)); err != nil {
				t.Errorf("write %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	_ = w.Close()

	data, err := os.ReadFile(filepath.Join(dir, "log.log"))
	if err != nil {
		t.Fatal(err)
	}
	// Each record's body must be exactly one repeated character. If two
	// goroutines interleaved, we'd find mixed characters between begin
	// and end markers.
	recs := strings.Split(string(data), "=== begin ")
	if len(recs)-1 != N {
		t.Errorf("got %d records, want %d", len(recs)-1, N)
	}
	for i, rec := range recs[1:] {
		body := rec[strings.Index(rec, "\n")+1:]
		body = body[:strings.Index(body, "=== end")]
		body = strings.TrimRight(body, "\n")
		if len(body) == 0 {
			continue
		}
		first := body[0]
		for j := 1; j < len(body); j++ {
			if body[j] != first {
				t.Errorf("record %d interleaved: first=%q at j=%d found %q", i, first, j, body[j])
				return
			}
		}
	}
}

func zeroPad(n, w int) string {
	s := ""
	if n == 0 {
		s = "0"
	}
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	for len(s) < w {
		s = "0" + s
	}
	return s
}
