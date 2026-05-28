// Package ipc implements the JSONL-framed Unix-socket protocol used by
// runq instances to discover each other and forward commands. The wire
// format is documented in
// specs/001-parallel-cmd-runner/contracts/ipc-protocol.md.
package ipc

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Protocol constants.
const (
	Version              = 1
	MaxMessageBytes      = 64 * 1024
	MaxCommandsPerSubmit = 64
)

// Error codes returned by the server on the wire. See
// contracts/ipc-protocol.md.
const (
	CodeUnsupportedVersion = "UNSUPPORTED_VERSION"
	CodeMessageTooLarge    = "MESSAGE_TOO_LARGE"
	CodeBadRequest         = "BAD_REQUEST"
	CodeTooManyCommands    = "TOO_MANY_COMMANDS"
	CodeEmptyText          = "EMPTY_TEXT"
	CodeInvalidTimeout     = "INVALID_TIMEOUT"
	CodeQueueFull          = "QUEUE_FULL"
	CodeShuttingDown       = "SHUTTING_DOWN"
)

// Request kinds.
const (
	KindHello  = "hello"
	KindSubmit = "submit"
	KindStop   = "stop"
)

// Response kinds.
const (
	KindHelloAck  = "hello-ack"
	KindSubmitAck = "submit-ack"
	KindStopAck   = "stop-ack"
)

// Request is the common envelope sent by the client. Exactly one of the
// kind-specific fields is populated.
type Request struct {
	Version  int          `json:"version"`
	Kind     string       `json:"kind"`
	Commands []SubmitItem `json:"commands,omitempty"`
}

// SubmitItem describes one command in a submit request.
type SubmitItem struct {
	Text    string `json:"text"`
	Timeout string `json:"timeout,omitempty"` // Go duration string; empty/absent = inherit
}

// HelloAck is the response to a hello request.
type HelloAck struct {
	Version int        `json:"version"`
	Kind    string     `json:"kind"`
	OK      bool       `json:"ok"`
	Code    string     `json:"code,omitempty"`
	Message string     `json:"message,omitempty"`
	Runner  RunnerInfo `json:"runner,omitempty"`
}

// RunnerInfo is the lightweight self-description the runner returns in
// hello-ack so the forwarder knows what it's talking to.
type RunnerInfo struct {
	PID           int    `json:"pid"`
	StartedAt     string `json:"started_at"`
	Parallelism   int    `json:"parallelism"`
	QueueCapacity int    `json:"queue_capacity"`
	QueuePending  int    `json:"queue_pending"`
	Running       int    `json:"running"`
}

// SubmitAck is the response to a submit request.
type SubmitAck struct {
	Version  int            `json:"version"`
	Kind     string         `json:"kind"`
	OK       bool           `json:"ok"`
	Code     string         `json:"code,omitempty"`
	Message  string         `json:"message,omitempty"`
	Accepted []AcceptedItem `json:"accepted"`
}

// AcceptedItem reports the id assigned to a forwarded command.
type AcceptedItem struct {
	ID string `json:"id"`
}

// StopAck is the response to a stop request.
type StopAck struct {
	Version int    `json:"version"`
	Kind    string `json:"kind"`
	OK      bool   `json:"ok"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// Validate checks a Request against the protocol rules and returns the
// wire error code on failure (empty when valid).
func (r *Request) Validate() (string, error) {
	if r.Version != Version {
		return CodeUnsupportedVersion, fmt.Errorf("version %d not supported (this server speaks %d)", r.Version, Version)
	}
	switch r.Kind {
	case KindHello, KindStop:
		// no per-kind body
		return "", nil
	case KindSubmit:
		if len(r.Commands) == 0 {
			return CodeBadRequest, errors.New("submit: empty commands array")
		}
		if len(r.Commands) > MaxCommandsPerSubmit {
			return CodeTooManyCommands, fmt.Errorf("submit: %d commands > max %d", len(r.Commands), MaxCommandsPerSubmit)
		}
		for i, c := range r.Commands {
			if strings.TrimSpace(c.Text) == "" {
				return CodeEmptyText, fmt.Errorf("submit: commands[%d].text is empty", i)
			}
			if c.Timeout != "" {
				if _, err := time.ParseDuration(c.Timeout); err != nil {
					return CodeInvalidTimeout, fmt.Errorf("submit: commands[%d].timeout %q: %w", i, c.Timeout, err)
				}
			}
		}
		return "", nil
	default:
		return CodeBadRequest, fmt.Errorf("unknown kind %q", r.Kind)
	}
}

// EncodeLine returns the JSON encoding of v followed by '\n'.
func EncodeLine(v any) ([]byte, error) {
	buf, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if len(buf)+1 > MaxMessageBytes {
		return nil, fmt.Errorf("encoded message exceeds %d bytes", MaxMessageBytes)
	}
	return append(buf, '\n'), nil
}
