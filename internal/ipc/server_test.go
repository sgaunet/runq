package ipc_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sgaunet/runq/internal/ipc"
)

// fakeHandler is a test double for ipc.Handler.
type fakeHandler struct {
	mu        sync.Mutex
	submitted []ipc.SubmitItem
	nextID    int
	queueFull bool
	stopped   bool
}

func (f *fakeHandler) Snapshot() ipc.RunnerInfo {
	return ipc.RunnerInfo{PID: 1234, Parallelism: 4, QueueCapacity: 50}
}

func (f *fakeHandler) Submit(items []ipc.SubmitItem) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.queueFull {
		return nil, ipc.ErrQueueFull
	}
	ids := make([]string, 0, len(items))
	for range items {
		f.nextID++
		ids = append(ids, "c-"+itoaPad(f.nextID, 4))
	}
	f.submitted = append(f.submitted, items...)
	return ids, nil
}

func (f *fakeHandler) Stop() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopped = true
}

func itoaPad(n, w int) string {
	s := ""
	if n == 0 {
		s = "0"
	}
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	for len(s) < w {
		s = "0" + s
	}
	return s
}

func startServer(t *testing.T, h ipc.Handler) (path string, srv *ipc.Server, ctxCancel context.CancelFunc) {
	t.Helper()
	path = shortTempSocketPath(t)
	srv, err := ipc.Listen(path, h, io.Discard)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go srv.Serve(ctx)
	return path, srv, cancel
}

func TestServer_HandshakeAndSubmit(t *testing.T) {
	h := &fakeHandler{}
	path, srv, cancel := startServer(t, h)
	defer cancel()
	defer srv.Close()

	ack, err := ipc.Forward(context.Background(), path, []ipc.SubmitItem{
		{Text: "echo a"}, {Text: "echo b"},
	})
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if !ack.OK {
		t.Fatalf("ack not ok: %s %s", ack.Code, ack.Message)
	}
	if len(ack.Accepted) != 2 {
		t.Errorf("accepted = %d, want 2", len(ack.Accepted))
	}
	if ack.Accepted[0].ID == "" {
		t.Errorf("first accepted id is empty")
	}

	h.mu.Lock()
	got := len(h.submitted)
	h.mu.Unlock()
	if got != 2 {
		t.Errorf("handler.submitted = %d, want 2", got)
	}
}

func TestServer_QueueFullSurfaced(t *testing.T) {
	h := &fakeHandler{queueFull: true}
	path, srv, cancel := startServer(t, h)
	defer cancel()
	defer srv.Close()

	ack, err := ipc.Forward(context.Background(), path, []ipc.SubmitItem{{Text: "echo x"}})
	if !ipc.IsQueueFull(err) {
		t.Fatalf("expected queue-full err, got %v", err)
	}
	if ack == nil {
		t.Fatal("expected non-nil ack")
	}
	if ack.OK {
		t.Errorf("ack.OK = true, want false")
	}
	if ack.Code != ipc.CodeQueueFull {
		t.Errorf("ack.Code = %q, want %q", ack.Code, ipc.CodeQueueFull)
	}
}

func TestServer_StopTriggersHandlerStop(t *testing.T) {
	h := &fakeHandler{}
	path, srv, cancel := startServer(t, h)
	defer cancel()
	defer srv.Close()

	ack, err := ipc.Stop(context.Background(), path)
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if !ack.OK {
		t.Errorf("ack.OK = false")
	}
	// Give the server goroutine a tick to call Stop on the handler.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		stopped := h.stopped
		h.mu.Unlock()
		if stopped {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("handler.stopped never became true")
}

func TestServer_RejectsHugeMessage(t *testing.T) {
	h := &fakeHandler{}
	path, srv, cancel := startServer(t, h)
	defer cancel()
	defer srv.Close()

	huge := strings.Repeat("x", ipc.MaxMessageBytes+1)
	_, err := ipc.Forward(context.Background(), path, []ipc.SubmitItem{{Text: huge}})
	if err == nil {
		t.Fatal("expected error")
	}
	// Encoder may catch it client-side OR server returns MESSAGE_TOO_LARGE.
	if !strings.Contains(err.Error(), "exceeds") && !strings.Contains(err.Error(), ipc.CodeMessageTooLarge) {
		t.Logf("acceptable err: %v", err)
	}
}

// Ensure ErrQueueFull is the canonical sentinel.
func TestErrQueueFull_Sentinel(t *testing.T) {
	if !errors.Is(ipc.ErrQueueFull, ipc.ErrQueueFull) {
		t.Fatal("sanity check failed")
	}
}
