package agent

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashicorp/yamux"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
)

func TestReverseClientsReuseStreamsWithCurrentCredentials(t *testing.T) {
	registry := NewRegistry(time.Second)
	t.Cleanup(func() { _ = registry.Close() })
	coreConn, agentConn := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = registry.Attach(ctx, "cluster-a", coreConn) }()
	config := yamux.DefaultConfig()
	config.LogOutput = io.Discard
	session, err := yamux.Client(agentConn, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	var opened atomic.Int64
	server := &http.Server{
		ReadHeaderTimeout: time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": domaincluster.Summary{Name: r.Header.Get("Authorization")}})
		}),
		ConnState: func(_ net.Conn, state http.ConnState) {
			if state == http.StateNew {
				opened.Add(1)
			}
		},
	}
	t.Cleanup(func() { _ = server.Close() })
	go func() { _ = server.Serve(yamuxListener{session: session}) }()
	deadline := time.Now().Add(time.Second)
	for !registry.Connected("cluster-a") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	for index := range 100 {
		token := "test-token-" + strconv.Itoa(index)
		client, err := registry.ClientFor(domaincluster.Connection{Summary: domaincluster.Summary{ID: "cluster-a"}, Metadata: map[string]any{"transport": "reverse_session", "token": token}})
		if err != nil {
			t.Fatal(err)
		}
		summary, err := client.GetSummary(ctx)
		if err != nil || summary.Name != "Bearer "+token {
			t.Fatalf("read %d: summary=%+v err=%v", index, summary, err)
		}
	}
	if count := opened.Load(); count != 1 {
		t.Fatalf("100 sequential reads opened %d streams, want 1", count)
	}
	registry.SetDefaultTimeout(2 * time.Second)
	client, err := registry.ClientFor(domaincluster.Connection{Summary: domaincluster.Summary{ID: "cluster-a"}, Metadata: map[string]any{"transport": "reverse_session"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetSummary(ctx); err != nil {
		t.Fatal(err)
	}
	if count := opened.Load(); count != 2 {
		t.Fatalf("timeout replacement opened %d streams, want 2", count)
	}
}

func TestRegistryCloseRejectsConcurrentAttach(t *testing.T) {
	for range 50 {
		registry := NewRegistry(time.Second)
		coreConn, agentConn := net.Pipe()
		done := make(chan error, 1)
		go func() { done <- registry.Attach(context.Background(), "cluster-a", coreConn) }()
		_ = registry.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("attach remained open after registry close")
		}
		_ = agentConn.Close()
		if registry.Connected("cluster-a") || len(registry.sessions) != 0 {
			t.Fatal("closed registry retained a session")
		}
	}
}
