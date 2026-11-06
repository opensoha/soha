package proxytransport

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestScopeBlocksProtectedIPv4AndIPv6AndRetainsUnprotectedPrivateUpstreams(t *testing.T) {
	scope, err := NewScope([]string{"10.20.0.0/16", "fd20::/64"}, []string{"payroll.example.internal"})
	if err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"10.20.1.1", "fd20::10", "payroll.example.internal", "alias.payroll.example.internal", "127.0.0.1", "::1"} {
		if _, err := scope.Resolve(context.Background(), host); err == nil {
			t.Fatalf("unsafe destination %s accepted", host)
		}
	}
	for _, host := range []string{"10.30.1.1", "fd30::10"} {
		if _, err := scope.Resolve(context.Background(), host); err != nil {
			t.Fatalf("unprotected enterprise upstream %s rejected: %v", host, err)
		}
	}
}
func TestGuardRejectsAmbiguousHTTPAuthorityBeforeDial(t *testing.T) {
	dialed := false
	handler, err := New(map[string]string{"node": "0123456789abcdef0123456789abcdef"}, func(context.Context, string, string) (net.Conn, error) { dialed = true; return nil, nil }, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = handler.Close() }()
	request := httptest.NewRequest(http.MethodGet, "http://public.example/", nil)
	request.Host = "protected.internal"
	request.SetBasicAuth("node", "0123456789abcdef0123456789abcdef")
	request.Header.Set("Proxy-Authorization", request.Header.Get("Authorization"))
	request.Header.Del("Authorization")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || dialed {
		t.Fatalf("ambiguous request status=%d dialed=%t", response.Code, dialed)
	}
}
func TestGuardRejectsExpiredRenewalAndCannotReopenAfterClose(t *testing.T) {
	handler, err := New(map[string]string{"node": "0123456789abcdef0123456789abcdef"}, func(context.Context, string, string) (net.Conn, error) { return nil, nil }, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if handler.Refresh(time.Now().Add(-time.Second)) == nil {
		t.Fatal("expired renewal accepted")
	}
	if err := handler.Close(); err != nil {
		t.Fatal(err)
	}
	if handler.Ready() || handler.Refresh(time.Now().Add(time.Minute)) == nil {
		t.Fatal("closed transport resurrected")
	}
}
