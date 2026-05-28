// Command runq runs many shell or argv commands in parallel, with a live
// per-command bullet UI and an implicit local Unix socket for forwarding
// additional commands into a running instance. See README.md and the spec
// under specs/001-parallel-cmd-runner/.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/sgaunet/runq/internal/exitcode"
)

// These are populated by goreleaser at build time via -ldflags -X.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	code, err := Execute(ctx, os.Args[1:], buildInfo{version: version, commit: commit, date: date})
	if err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(os.Stderr, "runq: %v\n", err)
	}
	os.Exit(code)
}

// buildInfo is plumbed into the root command so version reporting stays
// dependency-free.
type buildInfo struct {
	version string
	commit  string
	date    string
}

// ensure exitcode is referenced even if Execute is stubbed during early
// development; this lets `go build` succeed.
var _ = exitcode.OK
