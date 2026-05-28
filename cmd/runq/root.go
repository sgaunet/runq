package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/sgaunet/runq/internal/config"
	"github.com/sgaunet/runq/internal/exitcode"
)

const longHelp = `runq runs many commands in parallel with a live per-command
bullet UI and a local submission socket for forwarding more commands into a
running instance.

Two roles, picked automatically:

  Runner    — no live socket found. Opens the socket, runs commands, accepts
              forwarded submissions, exits when the queue drains.
  Forwarder — a live socket owned by you was found. Sends commands to the
              running instance and exits.

Streams:

  stdout = data only (a summary; --output=text|json)
  stderr = humans (live UI, progress, errors)

Configuration precedence: flags > env > defaults.

Security warning: by default, each command is evaluated by /bin/sh -c, so
shell metacharacters are interpreted. Use --no-shell to run argv directly.
Because forwarded commands go through the same path, treat the submission
socket as carrying shell strings.

Exit codes:

  0   all commands succeeded (runner) / submission ack'd (forwarder)
  1   at least one command failed
  2   usage error
  10  cancelled (SIGINT/SIGTERM)
  11  log file could not be written
  12  socket conflict that could not be resolved
  13  forwarder: queue full
  14  forwarder: failed to reach the running instance
`

// Execute runs the root command with the given args. It does not call
// os.Exit; callers translate the returned exit code.
func Execute(ctx context.Context, args []string, bi buildInfo) (int, error) {
	root := newRootCmd(ctx, bi)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		var ec exitErr
		if errors.As(err, &ec) {
			return ec.code, ec.err
		}
		return exitcode.Usage, err
	}
	return exitcode.OK, nil
}

// exitErr carries a specific exit code from a RunE handler.
type exitErr struct {
	code int
	err  error
}

func (e exitErr) Error() string {
	if e.err == nil {
		return exitcode.String(e.code)
	}
	return e.err.Error()
}
func (e exitErr) Unwrap() error { return e.err }

func newRootCmd(ctx context.Context, bi buildInfo) *cobra.Command {
	cfg := config.Defaults()
	cfg.SocketPath = config.DefaultSocketPath()

	root := &cobra.Command{
		Use:           "runq [flags] [command...]",
		Short:         "Run many commands in parallel with a live bullet UI",
		Long:          longHelp,
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			applyEnvOverrides(cmd.Flags())
			cfg.Args = args
			if os.Getenv("NO_COLOR") != "" {
				cfg.NoColor = true
			}
			if err := cfg.Validate(); err != nil {
				return exitErr{code: exitcode.Usage, err: err}
			}
			if !cfg.FromStdin && cfg.FromFile == "" && len(cfg.Args) == 0 {
				return exitErr{
					code: exitcode.Usage,
					err:  fmt.Errorf("no commands supplied (positional args, --from-file, or --from-stdin required)"),
				}
			}
			return runRunner(ctx, cfg)
		},
	}

	flags := root.PersistentFlags()
	flags.IntVarP(&cfg.Parallel, "parallel", "p", cfg.Parallel, "max commands running concurrently (1..1000)")
	flags.BoolVar(&cfg.NoShell, "no-shell", cfg.NoShell, "execute commands as argv (no shell expansion)")
	flags.DurationVar(&cfg.Timeout, "timeout", cfg.Timeout, "per-command timeout (0 = unlimited)")
	flags.DurationVar(&cfg.KillGrace, "kill-grace", cfg.KillGrace, "grace between SIGTERM and SIGKILL on cancel/timeout")
	flags.IntVar(&cfg.MaxQueue, "max-queue", cfg.MaxQueue, "max pending commands (forwarder submissions over this limit are refused)")
	flags.StringVar(&cfg.LogPath, "log", cfg.LogPath, "log file path (runner only; auto-uniquified on collision)")
	flags.StringVar(&cfg.SocketPath, "socket", cfg.SocketPath, "unix socket path")
	flags.StringVar(&cfg.FromFile, "from-file", "", "read commands from PATH, one per line (# and blank lines skipped)")
	flags.BoolVar(&cfg.FromStdin, "from-stdin", false, "read commands from stdin, one per line")
	flags.StringVar(&cfg.OutputFormat, "output", cfg.OutputFormat, "summary format: text or json")
	flags.BoolVar(&cfg.Quiet, "quiet", cfg.Quiet, "suppress live UI and per-command status (errors and summary still emit)")
	flags.BoolVar(&cfg.Verbose, "verbose", cfg.Verbose, "extra diagnostics on stderr")
	flags.BoolVar(&cfg.NoColor, "no-color", cfg.NoColor, "disable ANSI color (also honored via $NO_COLOR)")

	root.AddCommand(newVersionCmd(bi))
	root.AddCommand(newStopCmd(ctx))
	return root
}

// envBindings maps a flag name to the env var that backs it. The mapping is
// part of the public contract (contracts/cli.md).
var envBindings = map[string]string{
	"parallel":   "RUNQ_PARALLEL",
	"no-shell":   "RUNQ_NO_SHELL",
	"timeout":    "RUNQ_TIMEOUT",
	"kill-grace": "RUNQ_KILL_GRACE",
	"max-queue":  "RUNQ_MAX_QUEUE",
	"log":        "RUNQ_LOG",
	"socket":     "RUNQ_SOCKET",
	"output":     "RUNQ_OUTPUT",
	"quiet":      "RUNQ_QUIET",
	"verbose":    "RUNQ_VERBOSE",
}

// applyEnvOverrides sets flags from environment variables only when the flag
// was not explicitly set on the command line. Precedence: flag > env >
// default.
func applyEnvOverrides(flags *pflag.FlagSet) {
	for flagName, envName := range envBindings {
		f := flags.Lookup(flagName)
		if f == nil || f.Changed {
			continue
		}
		val, ok := os.LookupEnv(envName)
		if !ok {
			continue
		}
		// For bool flags, accept truthy values (1, true, yes) case-insensitively.
		if f.Value.Type() == "bool" {
			if isTruthy(val) {
				_ = f.Value.Set("true")
			}
			continue
		}
		_ = f.Value.Set(val)
	}
}

func isTruthy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}
