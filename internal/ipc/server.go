package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

// Handler describes what the server forwards into the runner. The runner
// supplies concrete implementations when constructing the Server.
type Handler interface {
	// Snapshot returns lightweight info for the hello-ack.
	Snapshot() RunnerInfo
	// Submit enqueues commands; returns the ids assigned and an error.
	// The runner is expected to map its internal queue-full error onto
	// ErrQueueFull from this package.
	Submit(items []SubmitItem) ([]string, error)
	// Stop asks the runner to drain its queue and exit.
	Stop()
}

// ErrQueueFull is returned by Handler.Submit when the pending queue is at
// capacity. The server translates it into a SubmitAck with code
// CodeQueueFull.
var ErrQueueFull = errors.New("queue full")

// Server accepts forwarder connections on a Unix domain socket and
// dispatches submit / stop requests to the supplied Handler.
type Server struct {
	path string
	ln   *net.UnixListener
	h    Handler

	mu       sync.Mutex
	shutdown bool
	stderrW  io.Writer
}

// Listen binds a Unix socket at path (creating it with mode 0600). Returns
// an error if the bind fails (e.g., path in use by a live runner — the
// caller can disambiguate with the discover package). stderrW receives
// diagnostic lines (e.g., peer-uid rejections). Pass nil to silence.
func Listen(path string, h Handler, stderrW io.Writer) (*Server, error) {
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = ln.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("chmod %s: %w", path, err)
	}
	if stderrW == nil {
		stderrW = io.Discard
	}
	return &Server{path: path, ln: ln, h: h, stderrW: stderrW}, nil
}

// Path returns the bound socket path.
func (s *Server) Path() string { return s.path }

// Serve loops accepting connections until ctx is cancelled or Close is
// called. It does not return until the listener has been closed.
func (s *Server) Serve(ctx context.Context) {
	// Cancel propagation: close the listener when ctx is done so Accept
	// returns.
	stopWatch := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = s.ln.Close()
		case <-stopWatch:
		}
	}()
	defer close(stopWatch)

	for {
		conn, err := s.ln.AcceptUnix()
		if err != nil {
			// If ctx was cancelled, exit quietly.
			if ctx.Err() != nil {
				return
			}
			if errors.Is(err, net.ErrClosed) {
				return
			}
			fmt.Fprintf(s.stderrW, "runq: accept error: %v\n", err)
			return
		}
		go s.handle(ctx, conn)
	}
}

// Close shuts the listener and removes the socket file.
func (s *Server) Close() error {
	s.mu.Lock()
	s.shutdown = true
	s.mu.Unlock()
	err := s.ln.Close()
	_ = os.Remove(s.path)
	return err
}

func (s *Server) handle(ctx context.Context, c *net.UnixConn) {
	defer c.Close()

	// Enforce same-user via kernel-attested peer uid.
	uid, err := peerUID(c)
	if err != nil {
		fmt.Fprintf(s.stderrW, "runq: peer credential check failed: %v\n", err)
		return
	}
	if uid != os.Getuid() {
		fmt.Fprintf(s.stderrW, "runq: refused connection from uid=%d (expected %d)\n", uid, os.Getuid())
		return
	}

	// Deadlines.
	_ = c.SetReadDeadline(time.Now().Add(30 * time.Second))
	_ = c.SetWriteDeadline(time.Now().Add(30 * time.Second))

	br := bufio.NewReaderSize(c, MaxMessageBytes+1)

	// Read the hello.
	hello, code, err := readRequest(br)
	if err != nil {
		s.writeErr(c, KindHelloAck, code, err)
		return
	}
	if hello.Kind != KindHello {
		s.writeErr(c, KindHelloAck, CodeBadRequest, fmt.Errorf("expected hello, got %q", hello.Kind))
		return
	}

	s.mu.Lock()
	down := s.shutdown
	s.mu.Unlock()
	ack := HelloAck{Version: Version, Kind: KindHelloAck, OK: !down}
	if down {
		ack.Code = CodeShuttingDown
		ack.Message = "runner is shutting down"
	} else {
		ack.Runner = s.h.Snapshot()
	}
	if err := writeLine(c, ack); err != nil || down {
		return
	}

	// Read the follow-up: submit OR stop.
	req, code, err := readRequest(br)
	if err != nil {
		s.writeErr(c, KindSubmitAck, code, err)
		return
	}
	switch req.Kind {
	case KindSubmit:
		s.handleSubmit(c, req)
	case KindStop:
		s.handleStop(c)
		// After acking stop, ask the runner to drain.
		s.h.Stop()
	default:
		s.writeErr(c, KindSubmitAck, CodeBadRequest, fmt.Errorf("unexpected kind %q after hello", req.Kind))
	}
	_ = ctx // unused but kept for future use (per-request cancellation)
}

func (s *Server) handleSubmit(c *net.UnixConn, req *Request) {
	ids, err := s.h.Submit(req.Commands)
	acc := make([]AcceptedItem, len(ids))
	for i, id := range ids {
		acc[i] = AcceptedItem{ID: id}
	}
	ack := SubmitAck{Version: Version, Kind: KindSubmitAck, Accepted: acc, OK: err == nil}
	if err != nil {
		if errors.Is(err, ErrQueueFull) {
			ack.Code = CodeQueueFull
			ack.Message = fmt.Sprintf("queue full; accepted %d, refused %d", len(acc), len(req.Commands)-len(acc))
		} else {
			ack.Code = CodeBadRequest
			ack.Message = err.Error()
		}
	}
	_ = writeLine(c, ack)
}

func (s *Server) handleStop(c *net.UnixConn) {
	_ = writeLine(c, StopAck{Version: Version, Kind: KindStopAck, OK: true})
}

func (s *Server) writeErr(c *net.UnixConn, kind, code string, err error) {
	switch kind {
	case KindHelloAck:
		_ = writeLine(c, HelloAck{Version: Version, Kind: KindHelloAck, OK: false, Code: code, Message: err.Error()})
	default:
		_ = writeLine(c, SubmitAck{Version: Version, Kind: KindSubmitAck, OK: false, Code: code, Message: err.Error()})
	}
}

// readRequest reads one JSONL request honoring MaxMessageBytes.
func readRequest(br *bufio.Reader) (*Request, string, error) {
	line, err := readLine(br, MaxMessageBytes)
	if err != nil {
		if errors.Is(err, errLineTooLong) {
			return nil, CodeMessageTooLarge, err
		}
		return nil, CodeBadRequest, err
	}
	var req Request
	if err := json.Unmarshal(line, &req); err != nil {
		return nil, CodeBadRequest, fmt.Errorf("invalid JSON: %w", err)
	}
	if code, err := req.Validate(); err != nil {
		return nil, code, err
	}
	return &req, "", nil
}

var errLineTooLong = errors.New("line exceeds maximum message size")

// readLine reads up to max bytes including the terminating '\n'. Returns
// the line WITHOUT the newline. Returns errLineTooLong if the limit is
// reached before a newline.
func readLine(br *bufio.Reader, max int) ([]byte, error) {
	var buf []byte
	for {
		b, err := br.ReadByte()
		if err != nil {
			return nil, err
		}
		if b == '\n' {
			return buf, nil
		}
		if len(buf)+1 > max {
			// Drain the rest of the line so the buffer is consistent.
			for {
				bb, e := br.ReadByte()
				if e != nil || bb == '\n' {
					break
				}
			}
			return nil, errLineTooLong
		}
		buf = append(buf, b)
	}
}

// writeLine encodes v as JSONL and writes it to w.
func writeLine(w io.Writer, v any) error {
	buf, err := EncodeLine(v)
	if err != nil {
		return err
	}
	_, err = w.Write(buf)
	return err
}

// Helpful when integrating with logs.
var _ = strings.Builder{}
