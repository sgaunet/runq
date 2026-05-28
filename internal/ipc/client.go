package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"
)

// ForwarderErr distinguishes the cases the caller needs to act on.
type ForwarderErr struct {
	Code    string // wire code from the server, when applicable
	Message string
	Err     error
}

func (e *ForwarderErr) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	}
	return e.Message
}
func (e *ForwarderErr) Unwrap() error { return e.Err }

// IsQueueFull reports whether err signals a queue-full refusal.
func IsQueueFull(err error) bool {
	var fe *ForwarderErr
	if errors.As(err, &fe) {
		return fe.Code == CodeQueueFull
	}
	return false
}

// Forward connects to a runner at socketPath, sends a submit with items,
// and returns the resulting SubmitAck. Connect, handshake, and submit are
// each guarded by short deadlines.
func Forward(ctx context.Context, socketPath string, items []SubmitItem) (*SubmitAck, error) {
	conn, err := dial(ctx, socketPath)
	if err != nil {
		return nil, &ForwarderErr{Message: fmt.Sprintf("dial %s: %v", socketPath, err), Err: err}
	}
	defer conn.Close()

	br := bufio.NewReaderSize(conn, MaxMessageBytes+1)

	// Handshake.
	if err := writeLine(conn, Request{Version: Version, Kind: KindHello}); err != nil {
		return nil, &ForwarderErr{Message: fmt.Sprintf("write hello: %v", err), Err: err}
	}
	helloAck, err := readHelloAck(br)
	if err != nil {
		return nil, err
	}
	if !helloAck.OK {
		return nil, &ForwarderErr{Code: helloAck.Code, Message: helloAck.Message}
	}

	// Submit.
	if err := writeLine(conn, Request{Version: Version, Kind: KindSubmit, Commands: items}); err != nil {
		return nil, &ForwarderErr{Message: fmt.Sprintf("write submit: %v", err), Err: err}
	}
	submitAck, err := readSubmitAck(br)
	if err != nil {
		return nil, err
	}
	if !submitAck.OK && submitAck.Code != CodeQueueFull {
		// Hard failure that isn't queue-full.
		return submitAck, &ForwarderErr{Code: submitAck.Code, Message: submitAck.Message}
	}
	if !submitAck.OK {
		// Queue-full but with possibly partial acceptance — return the
		// ack so the caller can inspect, plus the error.
		return submitAck, &ForwarderErr{Code: submitAck.Code, Message: submitAck.Message}
	}
	return submitAck, nil
}

// Stop connects to the runner and asks it to drain and exit.
func Stop(ctx context.Context, socketPath string) (*StopAck, error) {
	conn, err := dial(ctx, socketPath)
	if err != nil {
		return nil, &ForwarderErr{Message: fmt.Sprintf("dial %s: %v", socketPath, err), Err: err}
	}
	defer conn.Close()
	br := bufio.NewReaderSize(conn, MaxMessageBytes+1)

	if err := writeLine(conn, Request{Version: Version, Kind: KindHello}); err != nil {
		return nil, &ForwarderErr{Message: fmt.Sprintf("write hello: %v", err), Err: err}
	}
	helloAck, err := readHelloAck(br)
	if err != nil {
		return nil, err
	}
	if !helloAck.OK {
		return nil, &ForwarderErr{Code: helloAck.Code, Message: helloAck.Message}
	}

	if err := writeLine(conn, Request{Version: Version, Kind: KindStop}); err != nil {
		return nil, &ForwarderErr{Message: fmt.Sprintf("write stop: %v", err), Err: err}
	}
	var ack StopAck
	line, err := readLine(br, MaxMessageBytes)
	if err != nil {
		return nil, &ForwarderErr{Message: fmt.Sprintf("read stop-ack: %v", err), Err: err}
	}
	if err := json.Unmarshal(line, &ack); err != nil {
		return nil, &ForwarderErr{Message: fmt.Sprintf("parse stop-ack: %v", err), Err: err}
	}
	return &ack, nil
}

func dial(ctx context.Context, path string) (*net.UnixConn, error) {
	d := net.Dialer{Timeout: 5 * time.Second}
	rawConn, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		return nil, err
	}
	uc, ok := rawConn.(*net.UnixConn)
	if !ok {
		_ = rawConn.Close()
		return nil, errors.New("unexpected non-unix connection type")
	}
	_ = uc.SetDeadline(time.Now().Add(30 * time.Second))
	return uc, nil
}

func readHelloAck(br *bufio.Reader) (*HelloAck, error) {
	line, err := readLine(br, MaxMessageBytes)
	if err != nil {
		return nil, &ForwarderErr{Message: fmt.Sprintf("read hello-ack: %v", err), Err: err}
	}
	var ack HelloAck
	if err := json.Unmarshal(line, &ack); err != nil {
		return nil, &ForwarderErr{Message: fmt.Sprintf("parse hello-ack: %v", err), Err: err}
	}
	if ack.Version != Version {
		return nil, &ForwarderErr{
			Code:    CodeUnsupportedVersion,
			Message: fmt.Sprintf("server version %d, client version %d", ack.Version, Version),
		}
	}
	return &ack, nil
}

func readSubmitAck(br *bufio.Reader) (*SubmitAck, error) {
	line, err := readLine(br, MaxMessageBytes)
	if err != nil {
		return nil, &ForwarderErr{Message: fmt.Sprintf("read submit-ack: %v", err), Err: err}
	}
	var ack SubmitAck
	if err := json.Unmarshal(line, &ack); err != nil {
		return nil, &ForwarderErr{Message: fmt.Sprintf("parse submit-ack: %v", err), Err: err}
	}
	return &ack, nil
}
