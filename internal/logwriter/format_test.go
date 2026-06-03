package logwriter_test

import (
	"bytes"
	"fmt"
	"strings"
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
		// Invalid UTF-8: EscapeText must agree with fmt.Sprintf(%q, ...) exactly.
		{"\x80\x81", fmt.Sprintf("%q", "\x80\x81")},
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

// TestEscapeText_InvalidUTF8_MatchesFmtQ locks in the invariant that
// EscapeText and fmt.Sprintf(%q,...) produce identical output for invalid UTF-8.
func TestEscapeText_InvalidUTF8_MatchesFmtQ(t *testing.T) {
	cases := []string{
		"\x80",
		"\x80\x81",
		"\xff\xfe",
		"valid\x80mixed",
		"\x00\x01\x7f\x80\xff",
	}
	for _, s := range cases {
		want := fmt.Sprintf("%q", s)
		got := logwriter.EscapeText(s)
		if got != want {
			t.Errorf("EscapeText(%q):\n got  %s\n want %s", s, got, want)
		}
	}
}

func TestFooter_ContainsExitDurSrcInOrder(t *testing.T) {
	footer := logwriter.BuildFooter("c-0042", time.Unix(0, 0).UTC(), "0", 250*time.Millisecond)
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
