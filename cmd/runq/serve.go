package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/sgaunet/runq/internal/config"
	"github.com/sgaunet/runq/internal/exitcode"
	"github.com/sgaunet/runq/internal/ipc"
	"github.com/sgaunet/runq/internal/logwriter"
	"github.com/sgaunet/runq/internal/runner"
)

const serveLongHelp = `serve runs runq as a persistent listener: it binds the
per-user submission socket, waits for commands forwarded by other runq
invocations, and runs them in parallel. Unlike a plain runq run, it does NOT
exit when its queue drains — it stays ready for more work until you stop it.

serve takes no commands of its own; forward them with plain 'runq <command>'.

Stopping:

  Ctrl+C / SIGTERM   Graceful shutdown: stop accepting new submissions, send
                     SIGTERM to in-flight commands, wait up to --kill-grace,
                     then SIGKILL any stragglers and exit.
  second Ctrl+C      Force: SIGKILL any remaining children immediately and exit.
  runq stop          Same graceful shutdown (for a serve with no terminal).

Exit codes:

  0   stopped while idle (no command in flight or queued)
  2   usage error (e.g. commands passed to serve)
  10  stopped while commands were in flight or queued
  11  the per-session log directory could not be created
  12  another runq instance already owns the socket
`

// newServeCmd builds the `serve` subcommand. It shares the root command's
// resolved Config (cfg) so the inherited persistent flags (--parallel,
// --kill-grace, --log-dir, --socket, …) drive the listener.
func newServeCmd(ctx context.Context, cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "serve",
		Short:         "Run a persistent listener for forwarded commands",
		Long:          serveLongHelp,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(c *cobra.Command, _ []string) error {
			applyEnvOverrides(c.Flags())
			if os.Getenv("NO_COLOR") != "" {
				cfg.NoColor = true
			}
			if err := cfg.Validate(); err != nil {
				return exitErr{code: exitcode.Usage, err: err}
			}
			// Defensive: serve is a pure listener (FR-002). Command-source
			// flags are root-local so this normally cannot be reached, but
			// guard anyway in case that ever changes.
			if cfg.FromFile != "" || cfg.FromStdin {
				return exitErr{
					code: exitcode.Usage,
					err:  fmt.Errorf("serve takes no commands; forward them with plain 'runq <command>' instead"),
				}
			}
			return runServe(ctx, *cfg)
		},
	}
	return cmd
}

// runServe is the persistent-listener lifecycle. It binds the socket, runs
// forwarded commands until a signal or `runq stop`, then shuts down
// gracefully (a 2nd signal forces).
func runServe(ctx context.Context, cfg config.Config) error {
	if cfg.LogDir == "" {
		return exitErr{
			code: exitcode.LogWriteFailed,
			err:  fmt.Errorf("log directory could not be resolved; set --log-dir or RUNQ_LOG_DIR"),
		}
	}

	// Single-instance election (FR-007/FR-009). A live owner means refuse —
	// serve must not silently become a forwarder.
	decision, derr := ipc.Resolve(ctx, cfg.SocketPath)
	if derr != nil {
		return exitErr{code: exitcode.SocketConflict, err: derr}
	}
	if decision.Role == ipc.RoleForwarder {
		if decision.ForwarderConn != nil {
			_ = decision.ForwarderConn.Close()
		}
		return exitErr{
			code: exitcode.SocketConflict,
			err: fmt.Errorf(
				"a runq instance is already listening on %s; not starting a second one (stop it first or pass --socket)",
				cfg.SocketPath),
		}
	}

	// One per-session log directory (FR-003a).
	run, err := logwriter.OpenRun(cfg.LogDir, time.Now())
	if err != nil {
		return exitErr{code: exitcode.LogWriteFailed, err: err}
	}
	defer func() { _ = run.Close() }()
	cfg.LogDir = run.Dir()
	if cfg.Verbose {
		fmt.Fprintf(os.Stderr, "runq serve: log dir %s\n", cfg.LogDir)
	}

	sink := selectSink(cfg, os.Stderr)
	defer func() { _ = sink.Close() }()

	// runCtx → graceful shutdown (1st signal / runq stop).
	// forceCtx → forced shutdown (2nd signal): immediate SIGKILL.
	runCtx, runCancel := context.WithCancel(context.Background())
	defer runCancel()
	forceCtx, forceCancel := context.WithCancel(context.Background())
	defer forceCancel()

	r := runner.New(runner.Options{
		Parallelism:    cfg.Parallel,
		QueueCap:       cfg.MaxQueue,
		Shell:          !cfg.NoShell,
		DefaultTimeout: cfg.Timeout,
		KillGrace:      cfg.KillGrace,
		Linger:         true,
		ForceCtx:       forceCtx,
		Sink:           sink,
		Log:            run,
	})

	// beginGraceful is the single shutdown entry point shared by the 1st
	// signal and `runq stop`. It samples whether work was in flight (to pick
	// the exit code, FR-018), refuses new submissions (FR-012), and cancels
	// the run context (which drains: SIGTERM in-flight, SIGKILL after grace).
	var (
		srv          *ipc.Server
		shutdownOnce sync.Once
		hadWork      bool
	)
	beginGraceful := func() {
		shutdownOnce.Do(func() {
			hadWork = r.InFlight() > 0
			if !cfg.Quiet {
				fmt.Fprintln(os.Stderr, "runq serve: shutting down…")
			}
			if srv != nil {
				srv.BeginShutdown()
			}
			runCancel()
		})
	}

	srv, err = ipc.Listen(cfg.SocketPath, ipcAdapter{r: r, onStop: beginGraceful}, os.Stderr)
	if err != nil {
		return exitErr{code: exitcode.SocketConflict, err: fmt.Errorf("listen %s: %w", cfg.SocketPath, err)}
	}
	srvCtx, srvCancel := context.WithCancel(context.Background())
	defer srvCancel()
	go srv.Serve(srvCtx)

	// Staged signals: 1st → graceful, 2nd → force, 3rd → hard exit.
	stopSignals := installServeSignals(ctx, beginGraceful, forceCancel)
	defer stopSignals()

	if !cfg.Quiet {
		fmt.Fprintf(os.Stderr,
			"runq serve: listening on %s (pid %d); waiting for submissions — Ctrl+C to stop\n",
			srv.Path(), os.Getpid())
	}

	counts := r.Run(runCtx)

	// Tear down the listener and remove the socket file (FR-010).
	srvCancel()
	_ = srv.Close()

	if err := writeSummary(os.Stdout, cfg, counts, r); err != nil {
		return exitErr{code: exitcode.Failed, err: fmt.Errorf("summary: %w", err)}
	}

	// Exit code reflects ONLY the stop condition (clarify Q2 / FR-018):
	// work in flight at the stop → cancelled; idle → ok. Session command
	// failures and log errors are reported in the summary but do not change
	// the code.
	if hadWork {
		return exitErr{code: exitcode.Cancelled}
	}
	return nil
}

// installServeSignals wires staged shutdown. The 1st SIGINT/SIGTERM (or the
// inherited ctx being cancelled, e.g. by a parent) calls onFirst (graceful);
// a 2nd signal calls onSecond (force); a 3rd exits hard. Only real signals on
// the channel advance the stage count — the inherited ctx is a redundant
// graceful trigger (onFirst is expected to be idempotent). Returns a stop
// func that unregisters and ends the watcher.
func installServeSignals(ctx context.Context, onFirst, onSecond func()) func() {
	sigCh := make(chan os.Signal, 4)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		signals := 0
		ctxDone := ctx.Done()
		for {
			select {
			case <-ctxDone:
				ctxDone = nil // a cancelled ctx stays done; fire once
				onFirst()     // idempotent
			case <-sigCh:
				signals++
				switch signals {
				case 1:
					onFirst()
				case 2:
					onSecond()
				default:
					os.Exit(exitcode.Cancelled)
				}
			case <-done:
				return
			}
		}
	}()
	return func() {
		signal.Stop(sigCh)
		close(done)
	}
}
