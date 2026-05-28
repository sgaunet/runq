// Package exitcode defines the documented exit codes for runq.
//
// These values are part of the public CLI contract. Adding a new code
// requires a MAJOR version bump and an update to contracts/cli.md.
package exitcode

// Exit codes used by runq. The constitution requires every non-zero code
// to be documented in --help; see contracts/cli.md.
const (
	OK             = 0
	Failed         = 1
	Usage          = 2
	Cancelled      = 10
	LogWriteFailed = 11
	SocketConflict = 12
	QueueFull      = 13
	ForwardFailed  = 14
)

// String returns a short, stable name for an exit code. Unknown codes
// return "unknown".
func String(code int) string {
	switch code {
	case OK:
		return "ok"
	case Failed:
		return "failed"
	case Usage:
		return "usage"
	case Cancelled:
		return "cancelled"
	case LogWriteFailed:
		return "log-write-failed"
	case SocketConflict:
		return "socket-conflict"
	case QueueFull:
		return "queue-full"
	case ForwardFailed:
		return "forward-failed"
	default:
		return "unknown"
	}
}
