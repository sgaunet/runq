package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/sgaunet/runq/internal/config"
	"github.com/sgaunet/runq/internal/exitcode"
	"github.com/sgaunet/runq/internal/ipc"
	"github.com/sgaunet/runq/internal/logwriter"
	"github.com/sgaunet/runq/internal/runner"
	"github.com/sgaunet/runq/internal/ui"
)

// runRunner first attempts to discover a live runner via the socket. If
// one exists, it acts as a forwarder. Otherwise it becomes the runner.
func runRunner(ctx context.Context, cfg config.Config) error {
	cmds, err := buildInitialCommands(cfg)
	if err != nil {
		return exitErr{code: exitcode.Usage, err: err}
	}

	// Try forwarder role first.
	decision, derr := ipc.Resolve(ctx, cfg.SocketPath)
	if derr != nil {
		if cfg.Verbose {
			fmt.Fprintf(os.Stderr, "runq: role resolution: %v\n", derr)
		}
		return exitErr{code: exitcode.SocketConflict, err: derr}
	}
	if decision.Role == ipc.RoleForwarder {
		_ = decision.ForwarderConn.Close()
		return forward(ctx, cfg, cmds)
	}

	// Runner role.
	return runAsRunner(ctx, cfg, cmds)
}

// forward submits the provided commands to a running instance.
func forward(ctx context.Context, cfg config.Config, cmds []runner.Spec) error {
	items := make([]ipc.SubmitItem, len(cmds))
	for i := range cmds {
		items[i] = ipc.SubmitItem{Text: cmds[i].Text}
	}
	ack, err := ipc.Forward(ctx, cfg.SocketPath, items)
	if err != nil {
		if ipc.IsQueueFull(err) {
			if ack != nil {
				fmt.Fprintf(os.Stderr, "runq: queue full; %d of %d command(s) accepted\n",
					len(ack.Accepted), len(cmds))
			} else {
				fmt.Fprintf(os.Stderr, "runq: queue full: %v\n", err)
			}
			return exitErr{code: exitcode.QueueFull, err: err}
		}
		fmt.Fprintf(os.Stderr, "runq: forward failed: %v\n", err)
		return exitErr{code: exitcode.ForwardFailed, err: err}
	}
	fmt.Fprintf(os.Stderr, "runq: forwarded %d command(s) to running instance\n",
		len(ack.Accepted))
	return nil
}

func runAsRunner(ctx context.Context, cfg config.Config, cmds []runner.Spec) error {
	// Guard against an unresolvable log directory (e.g. XDG_STATE_HOME and
	// HOME both unset). config.DefaultLogDir returns "" in that case; treat
	// it as a hard failure per FR-015.
	if cfg.LogDir == "" {
		return exitErr{
			code: exitcode.LogWriteFailed,
			err:  fmt.Errorf("log directory could not be resolved; set --log-dir or RUNQ_LOG_DIR"),
		}
	}

	// Create the per-run log directory under the XDG state dir (or override).
	run, err := logwriter.OpenRun(cfg.LogDir, time.Now())
	if err != nil {
		return exitErr{code: exitcode.LogWriteFailed, err: err}
	}
	defer func() { _ = run.Close() }()
	// Report (and surface in the JSON summary) the concrete run directory.
	cfg.LogDir = run.Dir()
	if cfg.Verbose {
		fmt.Fprintf(os.Stderr, "runq: log dir %s\n", cfg.LogDir)
	}

	sink := selectSink(cfg, os.Stderr)
	defer func() { _ = sink.Close() }()

	r := runner.New(runner.Options{
		Parallelism:    cfg.Parallel,
		QueueCap:       cfg.MaxQueue,
		Shell:          !cfg.NoShell,
		DefaultTimeout: cfg.Timeout,
		KillGrace:      cfg.KillGrace,
		Sink:           sink,
		Log:            run,
	})

	if _, err := r.Submit(cmds); err != nil {
		return exitErr{code: exitcode.QueueFull, err: err}
	}

	// Bind the submission socket using an adapter so the runner package
	// doesn't depend on ipc.Handler's exact method shape.
	srv, err := ipc.Listen(cfg.SocketPath, ipcAdapter{r: r, onStop: r.Close}, os.Stderr)
	if err != nil {
		// If we couldn't bind even after Resolve said the path was free,
		// this is a real socket conflict.
		return exitErr{code: exitcode.SocketConflict, err: fmt.Errorf("listen %s: %w", cfg.SocketPath, err)}
	}
	if cfg.Verbose {
		fmt.Fprintf(os.Stderr, "runq: listening on %s (pid=%d)\n", srv.Path(), os.Getpid())
	}

	// Serve in the background.
	srvCtx, srvCancel := context.WithCancel(context.Background())
	defer srvCancel()
	go srv.Serve(srvCtx)

	// Per FR-021: runner exits when queue drains. Run() blocks until
	// then or until ctx is cancelled.
	counts := r.Run(ctx)

	// Stop the listener and remove the socket file.
	srvCancel()
	_ = srv.Close()

	if err := writeSummary(os.Stdout, cfg, counts, r); err != nil {
		return exitErr{code: exitcode.Failed, err: fmt.Errorf("summary: %w", err)}
	}

	if ctx.Err() != nil {
		return exitErr{code: exitcode.Cancelled, err: ctx.Err()}
	}
	// Log failures take precedence: a run that cannot write its log files is
	// not trustworthy even if commands appeared to succeed.
	if counts.LogErrors > 0 {
		return exitErr{code: exitcode.LogWriteFailed}
	}
	if counts.Failed > 0 || counts.TimedOut > 0 || counts.SpawnErrors > 0 {
		return exitErr{code: exitcode.Failed}
	}
	return nil
}

// buildInitialCommands assembles the initial set of commands from
// positional args / --from-file / --from-stdin per FR-001.
func buildInitialCommands(cfg config.Config) ([]runner.Spec, error) {
	var cmds []runner.Spec
	switch {
	case cfg.NoShell && len(cfg.Args) > 0:
		cmds = append(cmds, runner.Spec{
			Text:   strings.Join(cfg.Args, " "),
			Source: runner.SourceCLI,
		})
	case len(cfg.Args) > 0:
		for _, a := range cfg.Args {
			cmds = append(cmds, runner.Spec{Text: a, Source: runner.SourceCLI})
		}
	}
	if cfg.FromFile != "" {
		fromFile, err := runner.LoadFromFile(cfg.FromFile, runner.SourceFile)
		if err != nil {
			return nil, err
		}
		cmds = append(cmds, fromFile...)
	}
	if cfg.FromStdin {
		fromStdin, err := runner.LoadFromReader(os.Stdin, runner.SourceStdin)
		if err != nil {
			return nil, err
		}
		cmds = append(cmds, fromStdin...)
	}
	if len(cmds) == 0 {
		return nil, errors.New("no commands found in arguments, file, or stdin")
	}
	return cmds, nil
}

// selectSink picks the appropriate UI Sink based on TTY status and flags, and
// resolves the aligned-output Layout. Non-TTY output uses a fixed fallback width
// (deterministic when piped); a TTY uses a command zone sized to the detected
// terminal width.
func selectSink(cfg config.Config, stderrW *os.File) ui.Sink {
	if cfg.Quiet {
		return ui.Quiet{}
	}
	fd := int(stderrW.Fd())
	if !term.IsTerminal(fd) {
		return ui.NewPlain(stderrW, ui.Resolve(0, true))
	}
	width, _, err := term.GetSize(fd)
	if err != nil {
		width = 0
	}
	return ui.NewBullets(stderrW, ui.Resolve(width, false))
}
