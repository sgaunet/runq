package exitcode_test

import (
	"testing"

	"github.com/sgaunet/runq/internal/exitcode"
)

func TestCodes_StableValues(t *testing.T) {
	cases := []struct {
		name string
		got  int
		want int
	}{
		{"OK", exitcode.OK, 0},
		{"Failed", exitcode.Failed, 1},
		{"Usage", exitcode.Usage, 2},
		{"Cancelled", exitcode.Cancelled, 10},
		{"LogWriteFailed", exitcode.LogWriteFailed, 11},
		{"SocketConflict", exitcode.SocketConflict, 12},
		{"QueueFull", exitcode.QueueFull, 13},
		{"ForwardFailed", exitcode.ForwardFailed, 14},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %d, want %d (contract change requires MAJOR bump)", tc.name, tc.got, tc.want)
		}
	}
}

func TestString_KnownCodes(t *testing.T) {
	cases := map[int]string{
		exitcode.OK:             "ok",
		exitcode.Failed:         "failed",
		exitcode.Usage:          "usage",
		exitcode.Cancelled:      "cancelled",
		exitcode.LogWriteFailed: "log-write-failed",
		exitcode.SocketConflict: "socket-conflict",
		exitcode.QueueFull:      "queue-full",
		exitcode.ForwardFailed:  "forward-failed",
	}
	for code, want := range cases {
		if got := exitcode.String(code); got != want {
			t.Errorf("String(%d) = %q, want %q", code, got, want)
		}
	}
}

func TestString_UnknownCode(t *testing.T) {
	if got := exitcode.String(999); got != "unknown" {
		t.Errorf("String(999) = %q, want %q", got, "unknown")
	}
}
