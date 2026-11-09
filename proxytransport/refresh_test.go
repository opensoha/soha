package proxytransport

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

type refreshTestConn struct {
	net.Conn
	deadline    time.Time
	deadlineErr error
	closed      bool
}

func (c *refreshTestConn) SetDeadline(deadline time.Time) error {
	if c.deadlineErr != nil {
		return c.deadlineErr
	}
	c.deadline = deadline
	return nil
}
func (c *refreshTestConn) Close() error { c.closed = true; return nil }

func TestHandlerRefreshPrunesClosedConnectionWithoutFailingOtherLease(t *testing.T) {
	h, err := New(map[string]string{"node": strings.Repeat("p", 32)}, func(context.Context, string, string) (net.Conn, error) { return nil, nil }, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = h.Close() }()
	// Model a connection closed by node revocation, before its unregister path
	// acquires h.mu. The wrapped sentinel verifies errors.Is, not error equality.
	revoked := &refreshTestConn{deadlineErr: fmt.Errorf("deadline: %w", net.ErrClosed), closed: true}
	retained := &refreshTestConn{}
	h.connections[revoked] = struct{}{}
	h.connections[retained] = struct{}{}
	until := time.Now().Add(2 * time.Minute)
	if err := h.Refresh(until); err != nil {
		t.Fatalf("revoked A failed B renewal: %v", err)
	}
	if _, exists := h.connections[revoked]; exists {
		t.Fatal("closed connection not pruned")
	}
	if len(h.connections) != 1 || !retained.deadline.Equal(until) || !h.Ready() {
		t.Fatal("retained B not renewed")
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	if !retained.closed || h.Refresh(time.Now().Add(time.Minute)) == nil || h.Ready() {
		t.Fatal("closed handler revived")
	}
}

func TestHandlerRefreshPreservesOtherDeadlineFailure(t *testing.T) {
	h, err := New(map[string]string{"node": strings.Repeat("p", 32)}, func(context.Context, string, string) (net.Conn, error) { return nil, nil }, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = h.Close() }()
	failure := errors.New("deadline update failed")
	retained := &refreshTestConn{deadlineErr: failure}
	h.connections[retained] = struct{}{}
	if err := h.Refresh(time.Now().Add(2 * time.Minute)); !errors.Is(err, failure) {
		t.Fatalf("deadline failure hidden: %v", err)
	}
	if _, exists := h.connections[retained]; !exists {
		t.Fatal("non-closed connection incorrectly pruned")
	}
}
