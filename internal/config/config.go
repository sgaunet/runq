// Package config holds the runtime configuration resolved from flags,
// environment variables, and built-in defaults. The resolution order is:
// flags > environment > defaults. The constitution requires this order to be
// documented in --help.
package config

import (
	"errors"
	"fmt"
	"time"
)

// Output formats accepted by --output.
const (
	OutputText = "text"
	OutputJSON = "json"
)

// Bounds for validation.
const (
	MinParallel = 1
	MaxParallel = 1000
	MinMaxQueue = 1
)

// Config is the fully-resolved runtime configuration for one invocation.
type Config struct {
	Parallel     int
	NoShell      bool
	Timeout      time.Duration
	KillGrace    time.Duration
	MaxQueue     int
	LogPath      string
	SocketPath   string
	FromFile     string
	FromStdin    bool
	OutputFormat string
	Quiet        bool
	Verbose      bool
	NoColor      bool
	Args         []string
}

// Defaults returns a Config populated with the defaults from
// contracts/cli.md. SocketPath is left empty; callers should fill it via
// DefaultSocketPath() so the resolution can be overridden by tests.
func Defaults() Config {
	return Config{
		Parallel:     10,
		KillGrace:    5 * time.Second,
		MaxQueue:     50,
		LogPath:      "cli-executed.log",
		OutputFormat: OutputText,
	}
}

// Validate checks Config invariants and returns a user-facing error
// describing the first violation. It does not mutate.
func (c Config) Validate() error {
	if c.Parallel < MinParallel || c.Parallel > MaxParallel {
		return fmt.Errorf("--parallel must be in [%d,%d], got %d", MinParallel, MaxParallel, c.Parallel)
	}
	if c.MaxQueue < MinMaxQueue {
		return fmt.Errorf("--max-queue must be >= %d, got %d", MinMaxQueue, c.MaxQueue)
	}
	if c.KillGrace < 0 {
		return errors.New("--kill-grace must be >= 0")
	}
	if c.Timeout < 0 {
		return errors.New("--timeout must be >= 0")
	}
	switch c.OutputFormat {
	case OutputText, OutputJSON:
	default:
		return fmt.Errorf("--output must be %q or %q, got %q", OutputText, OutputJSON, c.OutputFormat)
	}
	if c.FromStdin && c.FromFile != "" {
		return errors.New("--from-stdin and --from-file are mutually exclusive")
	}
	if c.FromStdin && len(c.Args) > 0 {
		return errors.New("positional command arguments and --from-stdin are mutually exclusive")
	}
	return nil
}
