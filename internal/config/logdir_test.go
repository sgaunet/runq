package config

import (
	"path/filepath"
	"testing"
)

func TestDefaultLogDir_XDGStateHome(t *testing.T) {
	getenv := func(k string) string {
		if k == "XDG_STATE_HOME" {
			return "/xdg/state"
		}
		if k == "HOME" {
			return "/home/u"
		}
		return ""
	}
	got := defaultLogDir(getenv)
	want := filepath.Join("/xdg/state", "runq", "logs")
	if got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
}

func TestDefaultLogDir_FallbackToHome(t *testing.T) {
	getenv := func(k string) string {
		if k == "HOME" {
			return "/home/u"
		}
		return "" // XDG_STATE_HOME unset
	}
	got := defaultLogDir(getenv)
	want := filepath.Join("/home/u", ".local", "state", "runq", "logs")
	if got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
}

func TestDefaultLogDir_EmptyXDGTreatedAsUnset(t *testing.T) {
	getenv := func(k string) string {
		switch k {
		case "XDG_STATE_HOME":
			return "" // explicitly empty
		case "HOME":
			return "/home/u"
		}
		return ""
	}
	got := defaultLogDir(getenv)
	want := filepath.Join("/home/u", ".local", "state", "runq", "logs")
	if got != want {
		t.Errorf("path = %q, want %q (empty XDG_STATE_HOME must fall back)", got, want)
	}
}

func TestDefaultLogDir_RelativeXDGFallsBackToHome(t *testing.T) {
	getenv := func(k string) string {
		switch k {
		case "XDG_STATE_HOME":
			return "relative/state" // relative — must be ignored per XDG spec
		case "HOME":
			return "/home/u"
		}
		return ""
	}
	got := defaultLogDir(getenv)
	want := filepath.Join("/home/u", ".local", "state", "runq", "logs")
	if got != want {
		t.Errorf("path = %q, want %q (relative XDG_STATE_HOME must fall back to HOME)", got, want)
	}
}

func TestDefaultLogDir_NoXDGAndEmptyHome_ReturnsEmpty(t *testing.T) {
	getenv := func(_ string) string {
		return "" // both XDG_STATE_HOME and HOME are unset
	}
	got := defaultLogDir(getenv)
	if got != "" {
		t.Errorf("path = %q, want %q (empty HOME must yield empty string)", got, "")
	}
}

func TestDefaultLogDir_NoXDGAndRelativeHome_ReturnsEmpty(t *testing.T) {
	getenv := func(k string) string {
		switch k {
		case "HOME":
			return "relative/home" // relative HOME — must be ignored
		}
		return ""
	}
	got := defaultLogDir(getenv)
	if got != "" {
		t.Errorf("path = %q, want %q (relative HOME must yield empty string)", got, "")
	}
}
