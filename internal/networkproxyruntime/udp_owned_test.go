package networkproxyruntime

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestUDPOwnedCloseCancelsPendingDialAndPreventsLateSocket(t *testing.T) {
	owner := newOwnedPackets()
	entered, returned := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(returned)
		_, _ = owner.dial(context.Background(), func(ctx context.Context) (net.Conn, error) { close(entered); <-ctx.Done(); return nil, ctx.Err() }, func(context.Context) error { return nil })
	}()
	<-entered
	closed := make(chan error, 1)
	go func() { closed <- owner.close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("stop did not cancel pending UDP dial")
	}
	<-returned
	called := false
	if c, err := owner.dial(context.Background(), func(context.Context) (net.Conn, error) { called = true; return nil, nil }, func(context.Context) error { return nil }); err == nil || c != nil || called {
		t.Fatal("closed owner admitted a late packet socket")
	}
	if owner.activeCount() != 0 {
		t.Fatal("owned UDP resources remained after close")
	}
}

type udpCloseFailure struct{ net.Conn }

func (c udpCloseFailure) Close() error {
	_ = c.Conn.Close()
	return errors.New("fixture close result unknown")
}

func TestUDPOwnedCloseFailureCannotBecomeSuccessfulZeroProof(t *testing.T) {
	owner := newOwnedPackets()
	left, right := net.Pipe()
	defer right.Close()
	if _, err := owner.dial(context.Background(), func(context.Context) (net.Conn, error) { return udpCloseFailure{left}, nil }, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if owner.close() == nil {
		t.Fatal("UDP close error was hidden behind a zero count")
	}
}

func TestUDPOwnedCloseRetainsLateSocketFailure(t *testing.T) {
	owner := newOwnedPackets()
	entered, release := make(chan struct{}), make(chan struct{})
	left, right := net.Pipe()
	defer right.Close()
	returned := make(chan struct{})
	go func() {
		defer close(returned)
		_, _ = owner.dial(context.Background(), func(context.Context) (net.Conn, error) { close(entered); <-release; return udpCloseFailure{left}, nil }, func(context.Context) error { return nil })
	}()
	<-entered
	closed := make(chan error, 1)
	go func() { closed <- owner.close() }()
	<-owner.ctx.Done()
	close(release)
	select {
	case err := <-closed:
		if err == nil {
			t.Fatal("late socket close failure became successful stop")
		}
	case <-time.After(time.Second):
		t.Fatal("stop did not wait for its pending socket")
	}
	<-returned
}
