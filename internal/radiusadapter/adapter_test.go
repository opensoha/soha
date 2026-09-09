package radiusadapter

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/opensoha/soha/internal/networkprotocol"
)

func TestRadclientRequestUsesSecretFileAndEscapesValues(t *testing.T) {
	command := networkprotocol.NASSessionCommand{
		CommandID: "command-1", SessionID: "session-1", NASID: "nas-hq", SubjectID: "user-1", DeviceID: "aa:bb:cc:dd:ee:ff",
		Action: "coa", TargetAccessProfile: "restricted", PolicyVersion: 7, EffectiveAt: time.Now().UTC(), ReasonCode: "risk_changed",
		RadiusAttributes: &networkprotocol.RadiusAttributes{VLANID: 30, FilterID: "restricted\nFilter-Id = injected", SessionTimeoutSeconds: 900},
	}
	args, input := radclientRequest(Config{
		RadclientPath: "/usr/bin/radclient", NASTarget: "192.0.2.10:3799", SecretFile: "/run/secrets/radius", CommandTimeout: 3 * time.Second,
	}, command)
	joined := strings.Join(args, " ")
	if joined != "-b -r 1 -t 3.000 -S /run/secrets/radius 192.0.2.10:3799 coa" {
		t.Fatalf("unexpected radclient args: %q", joined)
	}
	if strings.Contains(joined, "shared-secret") || !strings.Contains(joined, "-S /run/secrets/radius") {
		t.Fatalf("radclient args expose or omit secret file: %q", joined)
	}
	if strings.Contains(input, "restricted\nFilter-Id = injected") || !strings.Contains(input, `Filter-Id = "restricted\nFilter-Id = injected"`) {
		t.Fatalf("radclient input is not safely quoted: %q", input)
	}
	for _, expected := range []string{`NAS-Identifier = "nas-hq"`, `User-Name = "user-1"`, `Calling-Station-Id = "aa:bb:cc:dd:ee:ff"`, `Class = "session-1"`, "Tunnel-Private-Group-Id = 30", "Session-Timeout = 900"} {
		if !strings.Contains(input, expected) {
			t.Fatalf("radclient input %q does not contain %q", input, expected)
		}
	}
}

func TestAdapterReplaysJournalWithoutExecutingCommandTwice(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	commandMessage := runtimeCommand(t, now, "runtime-1", "nas-hq")
	var mu sync.Mutex
	posts := make([]string, 0, 2)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(commandMessage)
		case http.MethodPost:
			body, _ := io.ReadAll(request.Body)
			mu.Lock()
			posts = append(posts, string(body))
			attempt := len(posts)
			mu.Unlock()
			if attempt == 1 {
				http.Error(w, "try again", http.StatusServiceUnavailable)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	defer server.Close()

	schemas, err := networkprotocol.CompileSchemas()
	if err != nil {
		t.Fatal(err)
	}
	journal, err := OpenJournal(filepath.Join(t.TempDir(), "results.json"))
	if err != nil {
		t.Fatal(err)
	}
	executions := 0
	adapter, err := New(Config{RuntimeID: "runtime-1", NASID: "nas-hq", ControlURL: server.URL, CommandTimeout: 5 * time.Second, MaxClockSkew: 2 * time.Minute}, server.Client(), schemas, journal, func(context.Context, networkprotocol.NASSessionCommand) ExecutionResult {
		executions++
		return ExecutionResult{Status: "applied", ReasonCode: "coa_applied"}
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	adapter.now = func() time.Time { return now.Add(time.Second) }
	firstErr := adapter.processOnce(context.Background())
	if firstErr == nil {
		t.Fatal("first processOnce() error = nil, want failed result delivery")
	}
	if executions != 1 || journal.Len() != 1 {
		t.Fatalf("after failed delivery executions=%d journal=%d error=%v, want 1/1", executions, journal.Len(), firstErr)
	}
	if err := adapter.processOnce(context.Background()); err != nil {
		t.Fatalf("second processOnce() error = %v", err)
	}
	if executions != 1 || journal.Len() != 0 {
		t.Fatalf("after replay executions=%d journal=%d, want 1/0", executions, journal.Len())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(posts) != 2 || posts[0] != posts[1] {
		t.Fatalf("result replay changed payload: %#v", posts)
	}
}

func TestAdapterRejectsCommandForAnotherNAS(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(runtimeCommand(t, now, "runtime-1", "nas-other"))
	}))
	defer server.Close()
	schemas, _ := networkprotocol.CompileSchemas()
	journal, _ := OpenJournal(filepath.Join(t.TempDir(), "results.json"))
	executions := 0
	adapter, err := New(Config{RuntimeID: "runtime-1", NASID: "nas-hq", ControlURL: server.URL, MaxClockSkew: 2 * time.Minute}, server.Client(), schemas, journal, func(context.Context, networkprotocol.NASSessionCommand) ExecutionResult {
		executions++
		return ExecutionResult{}
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	adapter.now = func() time.Time { return now }
	if err := adapter.processOnce(context.Background()); err == nil || !strings.Contains(err.Error(), "NAS identity") {
		t.Fatalf("processOnce() error = %v, want NAS identity mismatch", err)
	}
	if executions != 0 {
		t.Fatalf("executor called %d times", executions)
	}
}

func runtimeCommand(t *testing.T, now time.Time, runtimeID, nasID string) []byte {
	t.Helper()
	payload, err := json.Marshal(networkprotocol.NASSessionCommand{
		CommandID: "command-1", SessionID: "session-1", NASID: nasID, SubjectID: "user-1", DeviceID: "device-1",
		Action: "coa", TargetAccessProfile: "restricted", PolicyVersion: 7, EffectiveAt: now, ReasonCode: "risk_changed",
		RadiusAttributes: &networkprotocol.RadiusAttributes{VLANID: 30, FilterID: "restricted", SessionTimeoutSeconds: 900},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(networkprotocol.RuntimeMessage{
		SchemaVersion: networkprotocol.RuntimeSchemaVersion, MessageID: "message-1", MessageType: networkprotocol.MessageNASSessionCommand,
		ProducerID: "network-control", RuntimeID: runtimeID, RuntimeKind: "nas", OccurredAt: now, ExpiresAt: now.Add(time.Minute), Payload: payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
