package virtualization

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/opensoha/soha/internal/application/virtualization/consoleport"
)

func TestDialConsoleStopsWhenHandshakeTimesOut(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	defer close(release)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		stream, err := DialConsole(ctx, ConsoleURLResult{Type: "vnc", BackendURL: server.URL})
		if stream != nil {
			_ = stream.Close()
		}
		done <- err
	}()

	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("upstream did not receive the handshake request")
	}
	select {
	case err := <-done:
		if !errors.Is(err, consoleport.ErrKubeVirtConnection) {
			t.Fatalf("DialConsole() error = %v, want redacted connection error", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("upstream handshake exceeded the request deadline")
	}
}
