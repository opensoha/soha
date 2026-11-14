package sohacli

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestNetworkSessionExecuteRetainsOriginalHashWithoutReplay(t *testing.T) {
	hash := "sha256:" + strings.Repeat("a", 64)
	for _, mode := range []string{"accepted", "lost", "unbound", "old-server", "denied"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != http.MethodPost || r.URL.RequestURI() != "/api/v1/network-access/sessions/session:1/actions/execute" {
					t.Errorf("unexpected %s %s", r.Method, r.URL.RequestURI())
				}
				if mode == "lost" {
					hijacker, ok := w.(http.Hijacker)
					if !ok {
						t.Error("fixture writer does not support hijacking")
						return
					}
					conn, _, err := hijacker.Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					_ = conn.Close()
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if mode == "old-server" {
					w.WriteHeader(404)
					_, _ = fmt.Fprint(w, `{"error":{"code":"not_found","message":"unsupported"}}`)
					return
				}
				if mode == "denied" {
					w.WriteHeader(403)
					_, _ = fmt.Fprint(w, `{"error":{"code":"forbidden","message":"denied"}}`)
					return
				}
				session := "session:1"
				if mode == "unbound" {
					session = "other-session"
				}
				w.WriteHeader(202)
				_, _ = fmt.Fprintf(w, `{"data":{"id":"original-command","sessionId":%q,"runtimeId":"owned-runtime","nasId":"owned-nas","action":"disconnect","status":"pending"}}`, session)
			}))
			defer server.Close()
			var out, stderr bytes.Buffer
			code := Run(t.Context(), []string{"network-access", "sessions", "execute", "session:1", "--input-json", `{"action":"disconnect","targetAccessProfile":"deny","reasonCode":"manual_revoke","planHash":"` + hash + `"}`, "--yes"}, Runtime{In: strings.NewReader(""), Out: &out, Err: &stderr, ConfigPath: writeTestConfig(t, server.URL)})
			if calls.Load() != 1 || !strings.Contains(stderr.String(), hash) {
				t.Fatalf("calls=%d code=%d stderr=%s", calls.Load(), code, stderr.String())
			}
			if mode == "accepted" {
				if code != 0 || !strings.Contains(out.String(), `"status": "pending"`) {
					t.Fatalf("accepted code=%d out=%s err=%s", code, out.String(), stderr.String())
				}
				return
			}
			if code == 0 || out.Len() != 0 || !strings.Contains(stderr.String(), "sessions request "+hash+" --session-id session:1") || !strings.Contains(stderr.String(), "do not resubmit") {
				t.Fatalf("unconfirmed code=%d out=%s err=%s", code, out.String(), stderr.String())
			}
		})
	}
}

func TestNetworkOriginalCommandReadUsesOnlyGET(t *testing.T) {
	hash := "sha256:" + strings.Repeat("a", 64)
	for _, status := range []int{200, 401, 403, 404} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != http.MethodGet || r.URL.Path != "/api/v1/network-access/sessions/session:1/command-request" || r.URL.Query().Get("planHash") != hash {
					t.Errorf("unexpected request %s %s", r.Method, r.URL)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				if status == 200 {
					_, _ = fmt.Fprint(w, `{"data":{"id":"original","sessionId":"session:1","status":"applied"}}`)
				} else {
					_, _ = fmt.Fprint(w, `{"error":{"code":"denied","message":"denied"}}`)
				}
			}))
			defer server.Close()
			var out, stderr bytes.Buffer
			code := Run(t.Context(), []string{"network-access", "sessions", "request", hash, "--session-id", "session:1"}, Runtime{In: strings.NewReader(""), Out: &out, Err: &stderr, ConfigPath: writeTestConfig(t, server.URL)})
			if calls.Load() != 1 || status == 200 && code != 0 || status != 200 && (code == 0 || out.Len() != 0) {
				t.Fatalf("status=%d code=%d calls=%d err=%s", status, code, calls.Load(), stderr.String())
			}
		})
	}
}

func TestNetworkCommandInvalidArgumentsDoNotCallServer(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	for _, args := range [][]string{{"sessions", "command", "id"}, {"sessions", "request", "sha256:short", "--session-id", "session"}, {"sessions", "command", "id", "--session-id", "../session"}, {"sessions", "command", "id", "--session-id", "session", "--yes"}, {"sessions", "execute", "session", "--input-json", `{"action":"disconnect","planHash":"invalid"}`, "--yes"}} {
		var out, stderr bytes.Buffer
		code := Run(t.Context(), append([]string{"network-access"}, args...), Runtime{In: strings.NewReader(""), Out: &out, Err: &stderr, ConfigPath: writeTestConfig(t, server.URL)})
		if code == 0 || calls.Load() != 0 {
			t.Fatalf("invalid arguments called server: %v code=%d calls=%d", args, code, calls.Load())
		}
	}
}
