package radiusruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func httpFixture(t *testing.T) (*HTTP, *Tracker) {
	t.Helper()
	tracker, _ := fixture(t)
	handler, err := NewHTTP(tracker, 101, func(net.Conn) (uint32, error) { return 101, nil }, 8)
	if err != nil {
		t.Fatal(err)
	}
	return handler, tracker
}
func call(handler *HTTP, path string, value any) *httptest.ResponseRecorder {
	data, _ := json.Marshal(value)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), peerKey{}, true))
	result := httptest.NewRecorder()
	handler.ServeHTTP(result, req)
	return result
}
func TestHTTPRoundTripAndGeneratedOctetsAttributeMap(t *testing.T) {
	handler, tracker := httpFixture(t)
	result := call(handler, "/admit", request(""))
	if result.Code != 200 {
		t.Fatal(result.Code, result.Body.String())
	}
	var attributes map[string]struct {
		Value []string `json:"value"`
		Op    string   `json:"op"`
		Xlat  bool     `json:"do_xlat"`
	}
	if err := json.Unmarshal(result.Body.Bytes(), &attributes); err != nil {
		t.Fatal(err)
	}
	attr := attributes["control:Tmp-String-8"]
	if len(attr.Value) != 1 || !validNonce(attr.Value[0]) || attr.Op != ":=" || attr.Xlat {
		t.Fatal(attributes)
	}
	// nextState is required as a field, but its empty value requests generation.
	result = call(handler, "/challenge", map[string]string{"epoch": "child-1", "nasId": "nas-1", "stationId": "02:00:00:00:00:01", "state": "", "nonce": attr.Value[0], "nextState": ""})
	if result.Code != 200 {
		t.Fatal(result.Code, result.Body.String())
	}
	if err := json.Unmarshal(result.Body.Bytes(), &attributes); err != nil {
		t.Fatal(err)
	}
	state := attributes["reply:State"]
	if len(state.Value) != 1 || len(state.Value[0]) != 66 || state.Op != ":=" || state.Xlat {
		t.Fatal(attributes)
	}
	result = call(handler, "/admit", request(state.Value[0]))
	if result.Code != 200 {
		t.Fatal(result.Code)
	}
	if err := json.Unmarshal(result.Body.Bytes(), &attributes); err != nil {
		t.Fatal(err)
	}
	result = call(handler, "/finish", Completion{Request: request(state.Value[0]), Nonce: attributes["control:Tmp-String-8"].Value[0], Outcome: "reject"})
	if result.Code != 204 {
		t.Fatal(result.Code, result.Body.String())
	}
	if snap := tracker.Snapshot(); !snap.Known || snap.Active == nil || *snap.Active != 0 {
		t.Fatal(snap)
	}
}
func TestHTTPExistingChallengeUses204(t *testing.T) {
	handler, tracker := httpFixture(t)
	nonce, err := tracker.Admit(request(""))
	if err != nil {
		t.Fatal(err)
	}
	result := call(handler, "/challenge", Completion{Request: request(""), Nonce: nonce, NextState: "0x0102"})
	if result.Code != 204 || result.Body.Len() != 0 {
		t.Fatal(result.Code, result.Body.String())
	}
}
func TestHTTPRejectsUntrustedAndTCPPeers(t *testing.T) {
	handler, _ := httpFixture(t)
	req := httptest.NewRequest("POST", "/admit", strings.NewReader(`{}`))
	req.Header.Set("X-Peer-UID", "101")
	result := httptest.NewRecorder()
	handler.ServeHTTP(result, req)
	if result.Code != 403 {
		t.Fatal(result.Code)
	}
	if _, ok := handler.connContext(context.Background(), &net.TCPConn{}).Value(peerKey{}).(bool); ok {
		t.Fatal("TCP peer trusted")
	}
	if trusted, _ := handler.connContext(context.Background(), &net.UnixConn{}).Value(peerKey{}).(bool); !trusted {
		t.Fatal("injected Unix UID lost")
	}
	handler.peer = func(net.Conn) (uint32, error) { return 999, nil }
	if trusted, _ := handler.connContext(context.Background(), &net.UnixConn{}).Value(peerKey{}).(bool); trusted {
		t.Fatal("foreign UID trusted")
	}
	handler.peer = func(net.Conn) (uint32, error) { return 101, errors.New("no credentials") }
	if trusted, _ := handler.connContext(context.Background(), &net.UnixConn{}).Value(peerKey{}).(bool); trusted {
		t.Fatal("credential failure trusted")
	}
}
func TestHTTPStrictBodiesAndMalformedCompletionLatch(t *testing.T) {
	for _, body := range []string{`{"epoch":"child-1","epoch":"other"}`, `[]`, `{} {}`, `{"unknown":true}`, strings.Repeat("x", 4097)} {
		handler, tracker := httpFixture(t)
		req := httptest.NewRequest("POST", "/challenge", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(context.WithValue(req.Context(), peerKey{}, true))
		result := httptest.NewRecorder()
		handler.ServeHTTP(result, req)
		if result.Code != 400 || tracker.Snapshot().Known {
			t.Fatalf("accepted malformed completion %d", result.Code)
		}
	}
}
func TestHTTPBoundedServerAndConcurrency(t *testing.T) {
	handler, _ := httpFixture(t)
	server := handler.Server()
	if server.ReadTimeout != 2*time.Second || server.WriteTimeout != 2*time.Second || server.MaxHeaderBytes != 8192 || server.ConnContext == nil {
		t.Fatal("unbounded server")
	}
	for i := 0; i < cap(handler.slots); i++ {
		handler.slots <- struct{}{}
	}
	result := call(handler, "/admit", request(""))
	if result.Code != 503 {
		t.Fatal(result.Code)
	}
}

type brokenWriter struct{ header http.Header }

func (w brokenWriter) Header() http.Header     { return w.header }
func (brokenWriter) WriteHeader(int)           {}
func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("lost socket") }
func TestFailedHookResponseWriteLatchesUnknown(t *testing.T) {
	handler, tracker := httpFixture(t)
	handler.reply(brokenWriter{header: make(http.Header)}, "control:Tmp-String-8", "nonce")
	if tracker.Snapshot().Known {
		t.Fatal("failed write retained certainty")
	}
}
