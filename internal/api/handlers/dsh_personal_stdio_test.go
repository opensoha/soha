package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	copilot "github.com/opensoha/soha/internal/application/copilot"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type dshStdioFixture struct {
	dshCallbackFixture
	endpoint         string
	containerStopped func(context.Context) error
}

func (s *dshStdioFixture) PrepareDSHPersonalStdio(_ context.Context, grant string, id domain.DSHIdentity, asset, version string) (copilot.DSHPersonalStdioBinding, error) {
	if grant != "run-grant" || id.RunID != "owner-run" || id.Attempt != 1 || id.Fence != 2 || asset != "owner-mcp" || version != "1" {
		return copilot.DSHPersonalStdioBinding{}, apperrors.ErrAccessDenied
	}
	return copilot.DSHPersonalStdioBinding{
		Deadline:         time.Now().Add(5 * time.Second),
		Check:            func(context.Context, []byte) error { return nil },
		ContainerStopped: s.containerStopped,
		Open: func(ctx context.Context) (copilot.DSHPersonalStdioPeer, error) {
			dialer := websocket.Dialer{}
			conn, _, err := dialer.DialContext(ctx, s.endpoint, nil)
			return conn, err
		},
	}, nil
}

func TestDSHPersonalStdioRelay(t *testing.T) {
	const pingReply = "{\"jsonrpc\":\"2.0\",\"id\":\"server-ping\",\"result\":{}}\n"
	var stopped atomic.Bool
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		for _, fragment := range []string{`{"jsonrpc":"2.0","id":"server-ping",`, "\"method\":\"ping\"}\n"} {
			if err := conn.WriteMessage(websocket.BinaryMessage, []byte(fragment)); err != nil {
				t.Error(err)
				return
			}
		}
		pingAnswered := false
		for {
			kind, body, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if kind == websocket.BinaryMessage && string(body) == pingReply {
				pingAnswered = true
				continue
			}
			if kind == websocket.TextMessage {
				if string(body) != `{"close":true}` {
					t.Error("unexpected cleanup request")
				}
				if !pingAnswered {
					t.Error("Agent did not receive the local ping response")
				}
				_ = conn.WriteMessage(kind, []byte(`{"status":"stopped"}`))
				return
			}
			if err := conn.WriteMessage(kind, body); err != nil {
				return
			}
		}
	}))
	defer agent.Close()
	h, err := NewDSHRuntimeHandler(&dshStdioFixture{
		endpoint: "ws" + strings.TrimPrefix(agent.URL, "http"),
		containerStopped: func(context.Context) error {
			stopped.Store(true)
			return nil
		},
	}, callbackTestToken)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.GET("/personal-stdio", h.PersonalStdio)
	core := httptest.NewServer(router)
	defer core.Close()
	dialer := websocket.Dialer{HandshakeTimeout: time.Second}
	endpoint := "ws" + strings.TrimPrefix(core.URL, "http") + "/personal-stdio?runId=owner-run&attempt=1&fence=2&assetId=owner-mcp&version=1"
	headers := http.Header{"Authorization": {"Bearer " + callbackTestToken}, "X-Soha-Tool-Grant": {"wrong-grant"}}
	if conn, resp, err := dialer.Dial(endpoint, headers); err == nil {
		_ = conn.Close()
		t.Fatal("accepted unauthorized Run grant")
	} else if resp != nil {
		_ = resp.Body.Close()
	}
	headers.Set("X-Soha-Tool-Grant", "run-grant")
	conn, _, err := dialer.Dial(endpoint, headers)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for _, frame := range []struct {
		kind       int
		send, want string
	}{
		{websocket.BinaryMessage, "{\"jsonrpc\":\"2.0\",\"method\":\"tools/list\"}\n", "{\"jsonrpc\":\"2.0\",\"method\":\"tools/list\"}\n"},
		{websocket.TextMessage, `{"close":true}`, `{"status":"closed"}`},
	} {
		if err := conn.WriteMessage(frame.kind, []byte(frame.send)); err != nil {
			t.Fatal(err)
		}
		kind, got, err := conn.ReadMessage()
		if err != nil || kind != frame.kind || string(got) != frame.want {
			t.Fatalf("frame = %d %q %v", kind, got, err)
		}
	}
	if !stopped.Load() {
		t.Fatal("forwarded the terminal receipt before recording the stopped container")
	}
}

func TestDSHStdioPingResponse(t *testing.T) {
	for _, tc := range []struct{ name, request, want string }{
		{"integer ID", `{"jsonrpc":"2.0","id":-12,"method":"ping"}`, `{"jsonrpc":"2.0","id":-12,"result":{}}`},
		{"exact integer with exponent", `{"jsonrpc":"2.0","id":1.20e1,"method":"ping"}`, `{"jsonrpc":"2.0","id":1.20e1,"result":{}}`},
		{"fractional ID", `{"jsonrpc":"2.0","id":1.2,"method":"ping"}`, ""},
		{"no ID", `{"jsonrpc":"2.0","method":"ping"}`, ""},
		{"null ID", `{"jsonrpc":"2.0","id":null,"method":"ping"}`, ""},
		{"duplicate ID", `{"jsonrpc":"2.0","id":1,"id":2,"method":"ping"}`, ""},
		{"mixed response", `{"jsonrpc":"2.0","id":1,"method":"ping","result":{}}`, ""},
		{"different request", `{"jsonrpc":"2.0","id":1,"method":"sampling/createMessage"}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := copilot.DSHPersonalStdioPingResponse([]byte(tc.request + "\n"))
			if tc.want == "" && got != nil || tc.want != "" && string(got) != tc.want+"\n" {
				t.Fatalf("reply = %q", got)
			}
		})
	}
}
