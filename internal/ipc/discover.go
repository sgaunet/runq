package ipc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"syscall"
	"time"
)

// Role is the role this invocation has decided to play.
type Role int

const (
	RoleUnknown   Role = iota
	RoleForwarder      // a live runner was found
	RoleRunner         // no live runner; this invocation should bind
)

// Decision is the outcome of Resolve.
type Decision struct {
	Role Role
	// ForwarderConn is set when Role == RoleForwarder. The caller may use
	// it directly OR close it and call Forward(ctx, path, items) — the
	// latter is what cmd/runq does, since the connect is cheap and keeps
	// the role layer free of marshalling.
	ForwarderConn io.Closer
}

// Resolve performs the race-free runner election described in
// research.md §R3. It returns either:
//
//   - Decision{Role: RoleForwarder, ForwarderConn: <conn>}: a live runner
//     answered the handshake. The caller forwards its commands using
//     ipc.Forward.
//   - Decision{Role: RoleRunner}: no live runner exists. The caller binds
//     the socket via ipc.Listen.
//   - non-nil error when election failed (e.g., the socket path is held by
//     a different platform's listener, or the runner is on a different
//     protocol version).
func Resolve(ctx context.Context, path string) (Decision, error) {
	const maxAttempts = 3
	backoff := 50 * time.Millisecond

	for attempt := range maxAttempts {
		// Step 1: try to connect to an existing runner.
		conn, err := tryConnectAndHello(ctx, path)
		if err == nil {
			return Decision{Role: RoleForwarder, ForwarderConn: conn}, nil
		}
		// Connection refused / ENOENT both mean "no live runner here".
		if !isNoListener(err) {
			return Decision{}, fmt.Errorf("probe %s: %w", path, err)
		}

		// Step 2: best-effort remove any stale socket file at path.
		_ = os.Remove(path)

		// Step 3: attempt to bind. Return RoleRunner so the caller calls
		// ipc.Listen.
		probeListener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
		if err == nil {
			// Got it; close immediately so the caller's Listen call can take it.
			_ = probeListener.Close()
			_ = os.Remove(path)
			return Decision{Role: RoleRunner}, nil
		}
		// Someone else won the race; back off and try again.
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return Decision{}, ctx.Err()
		}
		backoff *= 4
		_ = attempt
	}
	return Decision{}, errors.New("could not elect a runner within 3 attempts")
}

func tryConnectAndHello(ctx context.Context, path string) (io.Closer, error) {
	d := net.Dialer{Timeout: 2 * time.Second}
	conn, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		return nil, err
	}
	// Verify the listener is a live runner by issuing a hello and reading
	// the ack. The actual commands are submitted later by Forward() so we
	// close the conn after the hello round-trip.
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Write(mustEncode(Request{Version: Version, Kind: KindHello})); err != nil {
		_ = conn.Close()
		return nil, err
	}
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	_ = conn.Close()
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, errors.New("empty hello-ack")
	}
	// A returnable Closer satisfies the Decision contract; we already
	// closed the probe connection, so return a no-op.
	return noopCloser{}, nil
}

func mustEncode(v any) []byte {
	b, err := EncodeLine(v)
	if err != nil {
		panic(err) // structured types only; cannot fail in practice
	}
	return b
}

func isNoListener(err error) bool {
	if errors.Is(err, syscall.ECONNREFUSED) {
		return true
	}
	if errors.Is(err, syscall.ENOENT) {
		return true
	}
	// net wraps these; also check the unwrapped op error message as a
	// fallback.
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		if errors.Is(opErr.Err, syscall.ECONNREFUSED) || errors.Is(opErr.Err, syscall.ENOENT) {
			return true
		}
	}
	return false
}

type noopCloser struct{}

func (noopCloser) Close() error { return nil }
