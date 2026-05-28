package ipc_test

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/sgaunet/runq/internal/ipc"
)

func TestResolve_NoListener_ReturnsRunner(t *testing.T) {
	path := shortTempSocketPath(t)
	dec, err := ipc.Resolve(context.Background(), path)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if dec.Role != ipc.RoleRunner {
		t.Errorf("role = %v, want RoleRunner", dec.Role)
	}
}

func TestResolve_LiveListener_ReturnsForwarder(t *testing.T) {
	h := &fakeHandler{}
	path := shortTempSocketPath(t)
	srv, err := ipc.Listen(path, h, io.Discard)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go srv.Serve(ctx)
	defer srv.Close()

	// Brief settle so the listener is fully ready.
	time.Sleep(20 * time.Millisecond)

	dec, err := ipc.Resolve(context.Background(), path)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if dec.Role != ipc.RoleForwarder {
		t.Errorf("role = %v, want RoleForwarder", dec.Role)
	}
	if dec.ForwarderConn != nil {
		_ = dec.ForwarderConn.Close()
	}
}

func TestResolve_RaceFreeElection(t *testing.T) {
	// Five goroutines call Resolve against the same fresh path. Without
	// a surviving listener, each elects RoleRunner. Under heavy
	// contention some callers may exhaust the 3-attempt retry cap and
	// return an error — that is acceptable behavior; the guarantee is
	// "no panic, no inconsistent role assignment", not "every caller
	// always succeeds in a 5-way race". This test asserts that:
	//   - at least one caller completes successfully, and
	//   - every successful caller observes RoleRunner (since no real
	//     listener ever stays bound).
	path := shortTempSocketPath(t)

	const N = 5
	var wg sync.WaitGroup
	var mu sync.Mutex
	roles := make([]ipc.Role, 0, N)

	for range N {
		wg.Add(1)
		go func() {
			defer wg.Done()
			dec, err := ipc.Resolve(context.Background(), path)
			if err != nil {
				return
			}
			mu.Lock()
			roles = append(roles, dec.Role)
			mu.Unlock()
		}()
	}
	wg.Wait()

	if len(roles) == 0 {
		t.Fatal("no caller succeeded; expected at least one")
	}
	for i, r := range roles {
		if r != ipc.RoleRunner {
			t.Errorf("roles[%d] = %v, want RoleRunner (no live listener)", i, r)
		}
	}
}
