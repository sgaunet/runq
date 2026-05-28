# runq

`runq` is a small, single-binary Go CLI for running many shell or argv commands
in parallel, with a live per-command bullet UI, a configurable concurrency cap
(default 10), and an implicit local Unix socket that lets a second `runq`
invocation forward extra commands into the running instance.

## Install

```bash
go install github.com/sgaunet/runq/cmd/runq@latest
```

## Quickstart

See [specs/001-parallel-cmd-runner/quickstart.md](specs/001-parallel-cmd-runner/quickstart.md).

## Log file format

`runq` appends one framed record per command to its log file (default
`cli-executed.log` in the current working directory; override with `--log`
or `RUNQ_LOG`).

```text
=== begin <id> · <iso8601-start> · "<command-text>" · src=<source> ===
<command's stdout and stderr, interleaved in arrival order, verbatim>
=== end   <id> · <iso8601-end>   · exit=<outcome> · dur=<duration> ===
```

- `<id>` — stable per-run identifier (`c-0001`, `c-0002`, …).
- `<iso8601>` — RFC3339 with nanoseconds, in UTC.
- `<command-text>` — the command as executed, rendered with Go's `%q` so
  it's single-line, double-quoted, and escape-safe.
- `<source>` — `cli`, `file`, `stdin`, or `socket` (the latter for
  commands forwarded by a second `runq` invocation).
- `<outcome>` — `0` on success, the numeric exit code on failure, or one
  of `signal-N`, `cancelled`, `timed-out`, `spawn-error`.
- `<duration>` — Go `time.Duration` between started and ended.

Concurrent commands never interleave at the byte level — each record is
written under a single exclusive lock, so the body between matching
begin/end markers is exactly what the command produced.

To extract one command's output:

```bash
awk '/^=== begin c-0042 /,/^=== end   c-0042 /' cli-executed.log
```

To list all failures:

```bash
grep '^=== end' cli-executed.log | grep -v 'exit=0 '
```

There is **no built-in size cap**. For long-running operators, rotate the
file externally with `logrotate` or your platform's equivalent.

## Principles

This project follows the principles in
[.specify/memory/constitution.md](.specify/memory/constitution.md):

- Single-purpose static binary, no runtime dependencies.
- Idiomatic Go: thin CLI wrappers, logic in plain packages.
- Strict CLI UX contract: `stdout` is data, `stderr` is humans, documented
  exit codes, `NO_COLOR`, config precedence (flags > env > defaults).
- Reliability: context-aware cancellation, bounded I/O, no orphaned children.
- Tested releases: unit + binary-level integration tests, goreleaser, SBOM.

## License

See [LICENSE](LICENSE).
