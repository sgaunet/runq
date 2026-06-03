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

## Serve (persistent listener)

By default a `runq` run exits as soon as its queue drains. For a long-lived
target you can feed over time, use `runq serve`: it binds the per-user socket,
**stays alive while idle**, and runs whatever later `runq` invocations forward
into it.

```bash
# terminal A — start the listener (stays up until you stop it)
runq serve

# terminal B — forward commands into it, any time, from any shell
runq 'make build' 'make test'
runq './deploy.sh staging'
```

Stopping (graceful shutdown):

- **Ctrl+C / SIGTERM** — stop accepting new submissions, send `SIGTERM` to
  in-flight commands, wait up to `--kill-grace` (default 5s), then `SIGKILL`
  any stragglers and exit.
- **second Ctrl+C** — force: `SIGKILL` remaining children immediately.
- **`runq stop`** — same graceful shutdown, for a `serve` running under a
  supervisor with no controlling terminal.

`serve` takes no commands of its own — forward them with plain `runq`. All of a
session's per-command logs land under a single run directory (see
[Logs](#logs)). Exit codes: `0` when stopped while idle, `10` when stopped with
work in flight or queued, `12` if another instance already owns the socket.
See `runq serve --help` for the full contract.

## Logs

`runq` writes **one log file per command** into a per-run directory under the
XDG state directory:

```text
$XDG_STATE_HOME/runq/logs/<run-ts>_run-<rand>/<cmd-ts>_<slug>_<id>.log
# default base: ~/.local/state/runq/logs
```

The base directory is created on demand and is overridable with `--log-dir` or
`RUNQ_LOG_DIR`. Each file name encodes when the command ran, what it was, and a
unique id:

- `<cmd-ts>` / `<run-ts>` — local-time `YYYYMMDD-HHMMSS` (files sort
  chronologically).
- `<slug>` — the full command text, lowercased and reduced to a
  filesystem-safe form (`sleep 50` → `sleep-50`, truncated if very long).
- `<id>` — a short random hex token, so identical commands run in parallel or
  repeated across runs never collide.

Each file holds a single framed record:

```text
=== begin <id> · <iso8601-start> · "<command-text>" · src=<source> ===
<command's stdout and stderr, interleaved in arrival order, verbatim>
=== end   <id> · <iso8601-end>   · exit=<outcome> · dur=<duration> ===
```

- `<id>` (in the header) — the stable per-run identifier (`c-0001`, …).
- `<iso8601>` — RFC3339 with nanoseconds, in **UTC** (the file *name* uses
  local time; the header is UTC by design).
- `<command-text>` — the command as executed, rendered with Go's `%q`.
- `<source>` — `cli`, `file`, `stdin`, or `socket` (the latter for commands
  forwarded by a second `runq` invocation).
- `<outcome>` — `0` on success, the numeric exit code on failure, or one of
  `signal-N`, `cancelled`, `timed-out`, `spawn-error`.
- `<duration>` — Go `time.Duration` between started and ended.

Because each command owns its file, output streams straight to disk and no
cross-command byte interleaving is possible. Reading one command's output is
just `cat`:

```bash
cat ~/.local/state/runq/logs/<run>/<cmd-ts>_<slug>_<id>.log
```

List a run's failures:

```bash
grep -L 'exit=0 ' ~/.local/state/runq/logs/<run>/*.log
```

There is **no built-in size cap** and runq never deletes or rotates old logs.
Manage disk usage by pruning old run directories yourself (e.g. via `find` or
`logrotate`).

> **Changed in v0.x (breaking):** earlier versions wrote a single combined
> `cli-executed.log` in the working directory via `--log`/`RUNQ_LOG`. That file
> and those flags are gone; use the per-command files under `--log-dir` /
> `RUNQ_LOG_DIR` instead.

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
