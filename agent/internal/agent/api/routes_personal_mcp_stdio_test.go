package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	cfgpkg "github.com/opensoha/soha/agent/internal/agent/config"
	"github.com/opensoha/soha/agent/internal/agent/environment"
	contractenv "github.com/opensoha/soha/contracts/agent/environment"
	"go.uber.org/zap"
)

func TestPersonalMCPStdioStreamsBoundCommandAndRejectsInjectedArgv(t *testing.T) {
	now := time.Now().UTC()
	request := contractenv.PersonalMCPStdioRequest{AssetID: "owned", Version: "1.0", Lease: contractenv.Lease{
		SchemaVersion: contractenv.LeaseVersion, ID: "original", RunID: "run", OwnerID: "owner",
		ScopeHash: "sha256:" + strings.Repeat("a", 64), Mode: "restricted-write", IssuedAt: now, ExpiresAt: now.Add(time.Minute),
		Quota: contractenv.Quota{DiskBytes: 32 << 20, MemoryBytes: 64 << 20, CPUUnits: 250, MaxProcesses: 16},
	}}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	assets := map[cfgpkg.MCPStdioAssetRef][]string{{ID: "owned", Version: "1.0"}: {"/bin/echo", "operator-defined"}}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	group := router.Group("/api/v1/runtime/personal-environments", authRequiredAnyMiddleware("agent-test-token"))
	policy := newActionPolicy(cfgpkg.SecurityConfig{AllowedActions: []string{actionPersonalEnvironmentExecute}}, zap.NewNop(), nil)
	var active atomic.Bool
	registerPersonalMCPStdio(group, policy, newNativePodTerminals(), assets, func(_ context.Context, lease contractenv.Lease, argv []string) (environment.PersonalMCPStdioCommand, error) {
		if lease.ID != request.Lease.ID || !slices.Equal(argv, []string{"/bin/echo", "operator-defined"}) {
			return environment.PersonalMCPStdioCommand{}, errors.New("unexpected lease or command")
		}
		if !active.CompareAndSwap(false, true) {
			return environment.PersonalMCPStdioCommand{}, environment.ErrEnvironmentCommandBusy
		}
		return environment.PersonalMCPStdioCommand{Abort: func() { active.Store(false) }, Run: func(_ context.Context, stdin *os.File, stdout io.Writer) environment.PersonalMCPStdioStatus {
			if _, err := io.Copy(stdout, stdin); err != nil {
				return environment.PersonalMCPStdioUnknown
			}
			return environment.PersonalMCPStdioClosed
		}}, nil
	})
	server := httptest.NewServer(router)
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/v1/runtime/personal-environments/mcp-stdio"
	header := http.Header{"Authorization": {"Bearer agent-test-token"}}
	injected := append(slices.Clone(raw[:len(raw)-1]), []byte(`,"argv":["/bin/sh"]}`)...)
	header.Set(contractenv.PersonalMCPStdioHeader, base64.RawURLEncoding.EncodeToString(injected))
	conn, resp, err := websocket.DefaultDialer.DialContext(ctx, url, header)
	if conn != nil {
		_ = conn.Close()
	}
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err == nil || resp == nil || resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("injected argv was not rejected: %v %v", resp, err)
	}
	header.Set(contractenv.PersonalMCPStdioHeader, base64.RawURLEncoding.EncodeToString(raw))
	conn, _, err = websocket.DefaultDialer.DialContext(ctx, url, header)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	busy, resp, err := websocket.DefaultDialer.DialContext(ctx, url, header)
	if busy != nil {
		_ = busy.Close()
	}
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err == nil || resp == nil || resp.StatusCode != http.StatusConflict {
		t.Fatalf("busy command must fail before upgrade: %v %v", resp, err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	data := []byte{0, 255, '\n'}
	if err := conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"close":true}`)); err != nil {
		t.Fatal(err)
	}
	var got []byte
	for {
		kind, payload, err := conn.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		if kind == websocket.BinaryMessage {
			got = append(got, payload...)
			continue
		}
		if kind != websocket.TextMessage || strings.TrimSpace(string(payload)) != `{"status":"closed"}` || !bytes.Equal(got, data) {
			t.Fatalf("stream output=%v status=%s", got, payload)
		}
		break
	}
}
