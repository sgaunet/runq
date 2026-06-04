package ui_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/sgaunet/runq/internal/ui"
)

func TestPlain_EmitsOnStderrWriter(t *testing.T) {
	var buf bytes.Buffer
	p := ui.NewPlain(&buf, ui.Resolve(0, true))
	p.OnQueued("c-0001", "echo hi")
	p.OnStart("c-0001", "echo hi")
	p.OnSuccess("c-0001", "echo hi", 0, 12*time.Millisecond)
	got := buf.String()
	for _, want := range []string{"QUEUED", "STARTED", "OK", "c-0001", "exit=0", "dur="} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q in %q", want, got)
		}
	}
	// New aligned format drops the legacy "·" separators.
	if strings.Contains(got, "·") {
		t.Errorf("output should not contain legacy '·' separator: %q", got)
	}
}

func TestPlain_QuietSinkIsSilent(t *testing.T) {
	var buf bytes.Buffer
	q := ui.Quiet{}
	q.OnQueued("c-0001", "echo hi")
	q.OnStart("c-0001", "echo hi")
	q.OnSuccess("c-0001", "echo hi", 0, time.Millisecond)
	if buf.Len() != 0 {
		t.Errorf("Quiet sink wrote %q", buf.String())
	}
	if err := q.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}
