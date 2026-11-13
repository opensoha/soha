package sohacli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	protocol "github.com/opensoha/soha/contracts/docker/runtime"
	contractresource "github.com/opensoha/soha/contracts/resource"
	"github.com/opensoha/soha/contracts/resource/terminalstream"
)

func fixtureDockerTerminalLease() protocol.TerminalLease {
	now := time.Now().UTC()
	binding, _ := protocol.NewProjectBinding("11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222")
	return protocol.TerminalLease{Protocol: protocol.TerminalProtocol, ID: "11511151-1151-4151-8151-115111511151", Status: "pending", ProjectID: "project-a", Target: protocol.ContainerBinding{ProjectBinding: binding, ServiceName: "api", ContainerID: strings.Repeat("a", 64)}, Shell: "/bin/sh", CreatedAt: now, TicketExpiresAt: now.Add(time.Minute), DeadlineAt: now.Add(5 * time.Minute), MaxInputBytes: 1048576, MaxOutputBytes: 33554432}
}

func TestNativeDockerTerminalRunRejectsNonTTYAndIgnoredFlagsBeforeHTTP(t *testing.T) {
	for _, extra := range [][]string{nil, {"--output", "json"}, {"--idempotency-key", "key"}, {"--stdin-file", "-"}, {"--session-timeout-seconds", "901"}, {"--shell", "sh -c id"}, {"--session-id", "11511151-1151-4151-8151-115111511151"}} {
		t.Run(strings.Join(extra, "-"), func(t *testing.T) {
			var calls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
			defer server.Close()
			args := []string{"docker", "projects", "terminal", "project-a", "--service", "api", "--expected-container-id", strings.Repeat("a", 64), "--profile", "dev", "--yes"}
			args = append(args, extra...)
			var out, errOut bytes.Buffer
			code := Run(t.Context(), args, Runtime{In: strings.NewReader(""), Out: &out, Err: &errOut, ConfigPath: writeTestConfig(t, server.URL)})
			if code != 2 || calls.Load() != 0 || out.Len() != 0 {
				t.Fatalf("code=%d calls=%d out=%q err=%q", code, calls.Load(), out.String(), errOut.String())
			}
		})
	}
}

func TestNativeDockerTerminalRunMetadataQueriesAndClosesWithoutStream(t *testing.T) {
	for _, action := range []string{"terminal-session", "terminal-close"} {
		t.Run(action, func(t *testing.T) {
			var calls atomic.Int64
			item := fixtureDockerTerminalLease()
			if action == "terminal-close" {
				closedAt := item.CreatedAt.Add(time.Millisecond)
				item.Status, item.ClosedAt = "closed", &closedAt
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				want := http.MethodGet
				if action == "terminal-close" {
					want = http.MethodDelete
				}
				if r.Method != want || strings.HasSuffix(r.URL.Path, "/stream") || r.URL.RawQuery != "" || r.URL.Path != "/api/v1/docker/projects/project-a/terminal-sessions/"+item.ID {
					t.Error("metadata command started or reconnected a terminal")
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": item})
			}))
			defer server.Close()
			var out, errOut bytes.Buffer
			args := []string{"docker", "projects", action, "project-a", "--session-id", item.ID, "--profile", "dev"}
			if action == "terminal-close" {
				args = append(args, "--yes")
			}
			code := Run(t.Context(), args, Runtime{In: strings.NewReader(""), Out: &out, Err: &errOut, ConfigPath: writeTestConfig(t, server.URL)})
			if code != 0 || calls.Load() != 1 || !strings.Contains(out.String(), item.ID) {
				t.Fatalf("code=%d calls=%d err=%q", code, calls.Load(), errOut.String())
			}
		})
	}
}

func TestNativeDockerTerminalRejectsMismatchedAuthorizationReceipt(t *testing.T) {
	for _, scenario := range []string{"target", "ticket", "lifetime", "shell", "active-result", "bounds"} {
		t.Run(scenario, func(t *testing.T) {
			item := fixtureDockerTerminalLease()
			ticket := strings.Repeat("A", 43)
			switch scenario {
			case "target":
				item.Target.ContainerID = strings.Repeat("b", 64)
			case "ticket":
				ticket = "PRIVATE_TICKET_MARKER"
			case "lifetime":
				item.DeadlineAt = item.CreatedAt.Add(6 * time.Minute)
			case "shell":
				item.Shell = "/bin/bash"
			case "active-result":
				item.Status = "active"
				item.Result = &contractresource.PodTerminalResult{Status: "unknown"}
			case "bounds":
				item.MaxInputBytes++
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{"data": protocol.TerminalCreated{Session: item, Ticket: ticket}})
			}))
			defer server.Close()
			client := APIClient{ServerURL: server.URL, Token: "private-token"}
			_, err := client.createDockerTerminal(t.Context(), dockerTerminalOptions{Project: "project-a"}, protocol.TerminalInput{ExpectedContainerID: strings.Repeat("a", 64), ServiceName: "api"})
			if err == nil || strings.Contains(err.Error(), "PRIVATE_TICKET_MARKER") {
				t.Fatalf("invalid authorization accepted/leaked: %v", err)
			}
		})
	}
}

func TestNativeDockerTerminalWebSocketUsesHeadersAndNeverRetries(t *testing.T) {
	for _, scenario := range []string{"known", "lost"} {
		t.Run(scenario, func(t *testing.T) {
			var attempts atomic.Int64
			item := fixtureDockerTerminalLease()
			ticket := strings.Repeat("A", 43)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempts.Add(1)
				if r.Header.Get("Authorization") != "Bearer private-token" || r.Header.Get(contractresource.NativePodTerminalTicketHeader) != ticket || strings.Contains(r.URL.String(), ticket) || strings.Contains(r.URL.String(), "private-token") || r.Header.Get("Origin") != "" || r.URL.RawQuery != "" {
					t.Error("credential transport boundary changed")
				}
				if scenario == "lost" {
					w.WriteHeader(503)
					return
				}
				conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
				if err != nil {
					return
				}
				stream := terminalstream.NewServer(r.Context(), conn, contractresource.NativeTerminalSize{Width: 80, Height: 24})
				defer func() { _ = stream.Close() }()
				_ = stream.SendReady()
				_, _ = stream.Write([]byte{0, 255})
				zero := int64(0)
				_ = stream.SendExit(contractresource.PodTerminalResult{Status: "succeeded", ExitCode: &zero, OutputBytes: 2, UIDVerifiedBeforeStart: true})
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			client := APIClient{ServerURL: server.URL, Token: "private-token"}
			conn, err := client.dialDockerTerminal(ctx, dockerTerminalOptions{Project: "project-a"}, protocol.TerminalCreated{Session: item, Ticket: ticket})
			if scenario == "lost" {
				if err == nil || strings.Contains(err.Error(), ticket) || strings.Contains(err.Error(), "private-token") {
					t.Fatalf("connection error leaked credentials: %v", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				stream := terminalstream.NewClient(ctx, conn)
				defer func() { _ = stream.Close() }()
				output, _ := io.ReadAll(stream)
				result, known := stream.Result()
				if !bytes.Equal(output, []byte{0, 255}) || !known || result.ExitCode == nil || *result.ExitCode != 0 {
					t.Fatalf("output=%v result=%+v", output, result)
				}
			}
			if attempts.Load() != 1 {
				t.Fatalf("reconnected: %d", attempts.Load())
			}
		})
	}
}
