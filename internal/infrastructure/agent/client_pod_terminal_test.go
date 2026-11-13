package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	contractresource "github.com/opensoha/soha/contracts/resource"
	"github.com/opensoha/soha/contracts/resource/terminalstream"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type nativePodSizeQueue struct{ sent atomic.Bool }

func (queue *nativePodSizeQueue) Next(ctx context.Context) *contractresource.NativeTerminalSize {
	if queue.sent.CompareAndSwap(false, true) {
		return &contractresource.NativeTerminalSize{Width: 120, Height: 40}
	}
	<-ctx.Done()
	return nil
}

func TestNativeAgentPodTerminalPreservesBytesExitAndNeverReconnects(t *testing.T) {
	for _, scenario := range []string{"known", "disconnect", "bad-counter", "old-agent", "forbidden"} {
		t.Run(scenario, func(t *testing.T) { nativeAgentPodTerminalPreservesBytesExitAndNeverReconnectsPhase1(t, &scenario) })
	}
}

func nativeAgentPodTerminalPreservesBytesExitAndNeverReconnectsPhase1(t *testing.T, sharedScenario *string) {

	var attempts atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		serveNativePodTerminalFixture(t, *sharedScenario, &attempts, w, request)
	}))
	defer server.Close()
	client := &Client{baseURL: server.URL, token: "private-agent-token", httpClient: server.Client(), wsDialer: websocket.DefaultDialer}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	var output bytes.Buffer
	result, err := client.StreamNativePodTerminal(ctx, "test", "app", contractresource.PodTerminalInput{ExpectedUID: "reviewed", Container: "main"}, io.NopCloser(bytes.NewReader([]byte{0, 255, '\n'})), &output, &nativePodSizeQueue{})
	if (*sharedScenario) == "known" {
		if err != nil || result.ExitCode == nil || *result.ExitCode != 42 || !bytes.Equal(output.Bytes(), []byte{255, 0, 27}) {
			t.Fatalf("result=%+v output=%v err=%v", result, output.Bytes(), err)
		}
	} else {
		if err == nil || strings.Contains(err.Error(), "PRIVATE_PROVIDER_FAILURE") {
			t.Fatalf("uncertain/private error: %v", err)
		}
		if (*sharedScenario) == "old-agent" && !errors.Is(err, apperrors.ErrUnsupportedOperation) {
			t.Fatalf("old Agent category=%v", err)
		}
		if (*sharedScenario) == "forbidden" && !errors.Is(err, apperrors.ErrAccessDenied) {
			t.Fatalf("forbidden category=%v", err)
		}
	}
	want := int64(1)
	if (*sharedScenario) == "old-agent" {
		want = 0
	}
	if attempts.Load() != want {
		t.Fatalf("attempts=%d want=%d", attempts.Load(), want)
	}
}

func serveNativePodTerminalFixture(t *testing.T, scenario string, attempts *atomic.Int64, w http.ResponseWriter, request *http.Request) {
	if request.Header.Get("Authorization") != "Bearer private-agent-token" || request.URL.Query().Get("access_token") != "" || request.Header.Get("Origin") != "" {
		t.Error("credential/header boundary changed")
		w.WriteHeader(403)
		return
	}
	if strings.HasSuffix(request.URL.Path, "/capabilities") {
		if scenario == "old-agent" {
			w.WriteHeader(404)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": contractresource.AgentPodExecutionCapabilities{ProtocolVersion: "1", ArgvVersion: "1", Ready: true, MaxTimeoutSeconds: 300, MaxOutputBytes: 1048576, TerminalVersion: "1", MaxSessionSeconds: 900, MaxTerminalInputBytes: 1048576, MaxTerminalOutputBytes: 33554432}})
		return
	}
	attempts.Add(1)
	if request.URL.Path != podExecutionPath+"/pods/app/terminal" || request.URL.Query().Get("expectedUid") != "reviewed" || request.URL.Query().Get("container") != "main" || request.URL.Query().Get("namespace") != "test" || request.URL.Query().Get("deadlineAt") == "" {
		t.Error("frozen target or deadline lost")
	}
	if scenario == "forbidden" {
		w.WriteHeader(403)
		_, _ = io.WriteString(w, "PRIVATE_PROVIDER_FAILURE")
		return
	}
	conn, err := (&websocket.Upgrader{}).Upgrade(w, request, nil)
	if err != nil {
		return
	}
	stream := terminalstream.NewServer(request.Context(), conn, contractresource.NativeTerminalSize{Width: 80, Height: 24})
	defer func() { _ = stream.Close() }()
	if scenario == "disconnect" {
		return
	}
	if stream.SendReady() != nil {
		return
	}
	data, err := io.ReadAll(stream)
	if err != nil {
		return
	}
	if !bytes.Equal(data, []byte{0, 255, '\n'}) {
		t.Error("stdin bytes changed")
	}
	output := []byte{255, 0, 27}
	_, _ = stream.Write(output)
	code := int64(42)
	count := int64(len(output))
	if scenario == "bad-counter" {
		count++
	}
	_ = stream.SendExit(contractresource.PodTerminalResult{Status: "failed", ExitCode: &code, InputBytes: int64(len(data)), OutputBytes: count, UIDVerifiedBeforeStart: true, ReasonCode: "process_exited"})
}
