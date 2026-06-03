package logwriter_test

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/sgaunet/runq/internal/logwriter"
)

func TestSlug(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"sleep 50", "sleep-50"},
		{"curl https://example.com", "curl-https-example-com"},
		{"make build -j4", "make-build-j4"},
		{`echo "a/b|c"`, "echo-a-b-c"},
		{"multi   spaces", "multi-spaces"}, // runs of non-alnum collapse to one '-'
		{"  ", "cmd"},
		{"", "cmd"},
		{"!!!", "cmd"},
		{"UPPER Case", "upper-case"},
		{"--leading-and-trailing--", "leading-and-trailing"},
	}
	for _, tc := range cases {
		if got := logwriter.Slug(tc.in); got != tc.want {
			t.Errorf("Slug(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSlug_TruncatesLongInput(t *testing.T) {
	long := "echo " + strings.Repeat("x", 400)
	got := logwriter.Slug(long)
	if len(got) > 80 {
		t.Errorf("slug length = %d, want <= 80", len(got))
	}
	if strings.HasSuffix(got, "-") {
		t.Errorf("slug should not end with '-': %q", got)
	}
	if got == "" {
		t.Errorf("slug unexpectedly empty")
	}
}

var fileNameRe = regexp.MustCompile(`^[0-9]{8}-[0-9]{6}_[a-z0-9-]+_[0-9a-f]+\.log$`)

func TestFileName_Shape(t *testing.T) {
	start := time.Date(2026, 5, 28, 14, 30, 22, 0, time.Local)
	name := logwriter.FileName(start, "sleep 50", "a1b2c3")
	if name != "20260528-143022_sleep-50_a1b2c3.log" {
		t.Errorf("FileName = %q, want %q", name, "20260528-143022_sleep-50_a1b2c3.log")
	}
	if !fileNameRe.MatchString(name) {
		t.Errorf("FileName %q does not match grammar %s", name, fileNameRe)
	}
}

func TestFileName_StaysWithinNameMax(t *testing.T) {
	start := time.Date(2026, 5, 28, 14, 30, 22, 0, time.Local)
	name := logwriter.FileName(start, strings.Repeat("y", 500), "deadbeef")
	if len(name) >= 255 {
		t.Errorf("filename length = %d, must be < 255 (NAME_MAX)", len(name))
	}
}
