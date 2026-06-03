package config

import (
	"os"
	"path/filepath"
)

// DefaultLogDir returns the base directory for per-command logs per
// contracts/logging.md:
//
//   - $XDG_STATE_HOME/runq/logs when $XDG_STATE_HOME is set, non-empty, and absolute;
//   - otherwise $HOME/.local/state/runq/logs when $HOME is non-empty and absolute.
//
// Per the XDG Base Directory specification, relative values of XDG_STATE_HOME
// MUST be ignored. Similarly, a relative HOME would resolve against the process
// CWD and produce a wrong location, so it is also ignored. An empty string is
// returned when neither variable yields a usable absolute base ("unresolved");
// callers such as Validate will surface this as an explicit error.
//
// The directory is not created here; logwriter.OpenRun creates the per-run
// subdirectory (and any missing parents) on demand.
func DefaultLogDir() string {
	return defaultLogDir(os.Getenv)
}

func defaultLogDir(getenv func(string) string) string {
	if dir := getenv("XDG_STATE_HOME"); dir != "" && filepath.IsAbs(dir) {
		return filepath.Join(dir, "runq", "logs")
	}
	if home := getenv("HOME"); home != "" && filepath.IsAbs(home) {
		return filepath.Join(home, ".local", "state", "runq", "logs")
	}
	return ""
}
