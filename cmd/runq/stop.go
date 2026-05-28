package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/sgaunet/runq/internal/config"
	"github.com/sgaunet/runq/internal/exitcode"
	"github.com/sgaunet/runq/internal/ipc"
)

func newStopCmd(ctx context.Context) *cobra.Command {
	var socketPath string
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Ask the running instance to drain its queue and exit",
		Long: `stop connects to the running runq instance and asks it to drain its
pending queue, finish in-flight commands, and exit. Useful when the runner
was started in the background or has no controlling terminal.

Exits 0 on successful drain request, 14 if no runner is reachable.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if socketPath == "" {
				socketPath = config.DefaultSocketPath()
			}
			ack, err := ipc.Stop(ctx, socketPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "runq: stop failed: %v\n", err)
				return exitErr{code: exitcode.ForwardFailed, err: err}
			}
			if !ack.OK {
				fmt.Fprintf(os.Stderr, "runq: stop refused: %s: %s\n", ack.Code, ack.Message)
				return exitErr{code: exitcode.ForwardFailed}
			}
			fmt.Fprintln(os.Stderr, "runq: stop acknowledged; runner will drain and exit")
			return nil
		},
	}
	cmd.Flags().StringVar(&socketPath, "socket", "", "unix socket path (default: $XDG_RUNTIME_DIR/runq.sock or /tmp/runq-$UID.sock)")
	return cmd
}
