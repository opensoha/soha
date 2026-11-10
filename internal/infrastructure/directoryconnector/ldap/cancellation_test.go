package ldap

import (
	"context"
	"net"
	"testing"
	"time"

	goldap "github.com/go-ldap/ldap/v3"
	domain "github.com/opensoha/soha/internal/domain/directorysync"
)

type cancellationCredentials struct{}

func (cancellationCredentials) GetConnectionCredential(context.Context, string) (domain.ConnectionCredential, error) {
	return domain.ConnectionCredential{Username: "owned", Password: "private-test-password"}, nil
}

func TestLDAPCancellationInterruptsBlockedBind(t *testing.T) {
	client, server := net.Pipe()
	t.Cleanup(func() { _ = client.Close(); _ = server.Close() })
	conn := goldap.NewConn(client, false)
	conn.Start()
	adapter, err := NewAdapter(cancellationCredentials{})
	if err != nil {
		t.Fatal(err)
	}
	adapter.dial = func(string, ...goldap.DialOpt) (*goldap.Conn, error) { return conn, nil }
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	read := make(chan error, 1)
	go func() { buffer := make([]byte, 4096); _, err := server.Read(buffer); read <- err }()
	result := make(chan error, 1)
	go func() {
		opened, err := adapter.connect(ctx, domain.Connection{ID: "owned", Metadata: map[string]any{"endpoint": "ldap://127.0.0.1:1"}})
		if opened != nil {
			_ = opened.Close()
		}
		result <- err
	}()
	select {
	case err := <-read:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("bind did not enter owned pipe")
	}
	cancel()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("blocked bind was accepted after cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("LDAP context cancellation did not close blocking I/O")
	}
}

func TestLDAPAlreadyCanceledContextNeverDials(t *testing.T) {
	adapter, err := NewAdapter(cancellationCredentials{})
	if err != nil {
		t.Fatal(err)
	}
	adapter.dial = func(string, ...goldap.DialOpt) (*goldap.Conn, error) {
		t.Error("canceled context dialed")
		return nil, context.Canceled
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = adapter.connect(ctx, domain.Connection{Metadata: map[string]any{"endpoint": "ldap://127.0.0.1:1"}})
	if err != context.Canceled {
		t.Fatal("cancellation did not propagate", err)
	}
}
