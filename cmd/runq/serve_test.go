package main

import (
	"context"
	"testing"

	"github.com/sgaunet/runq/internal/exitcode"
)

// TestServe_RejectsPositionalArgs verifies the serve subcommand's
// argument-parsing contract (constitution Principle V): serve is a pure
// listener and takes no commands (FR-002), so positional args are a usage
// error. This path returns before binding the socket, so it does not block.
func TestServe_RejectsPositionalArgs(t *testing.T) {
	code, err := Execute(context.Background(), []string{"serve", "echo", "hi"}, buildInfo{})
	if code != exitcode.Usage {
		t.Errorf("`serve echo hi` exit = %d, want %d (usage); err=%v", code, exitcode.Usage, err)
	}
}

// TestServe_RejectsCommandSourceFlags verifies that command-source flags are
// not valid for serve (they are root-local, FR-002). The unknown-flag error
// resolves to the usage exit code.
func TestServe_RejectsCommandSourceFlags(t *testing.T) {
	for _, args := range [][]string{
		{"serve", "--from-file", "/tmp/does-not-matter"},
		{"serve", "--from-stdin"},
	} {
		code, err := Execute(context.Background(), args, buildInfo{})
		if code != exitcode.Usage {
			t.Errorf("`%v` exit = %d, want %d (usage); err=%v", args, code, exitcode.Usage, err)
		}
	}
}
