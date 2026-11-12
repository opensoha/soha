package proxytransport

import (
	"context"
	"encoding/base64"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMappedUsernameRoundTrip(t *testing.T) {
	for _, ids := range [][2]string{{"c", "n"}, {"credential.a:b", "node:c.d"}, {"c" + strings.Repeat(":", 127), "n" + strings.Repeat(".", 127)}} {
		username, err := EncodeMappedUsername(ids[0], ids[1])
		if err != nil || strings.Contains(username, ":") || len(username) > MaxUsernameBytes {
			t.Fatalf("encode: length=%d err=%v", len(username), err)
		}
		credentialID, nodeID, err := DecodeMappedUsername(username)
		if err != nil || credentialID != ids[0] || nodeID != ids[1] {
			t.Fatalf("round trip: %q %q %v", credentialID, nodeID, err)
		}
	}
}

func TestMappedUsernameRejectsInvalidEncodings(t *testing.T) {
	encodedJSON := func(raw string) string { return "v1." + base64.RawURLEncoding.EncodeToString([]byte(raw)) }
	canonical, err := EncodeMappedUsername("credential", "node")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, username string }{
		{"unknown version", "v2." + strings.TrimPrefix(canonical, "v1.")},
		{"oversized", strings.Repeat("a", MaxUsernameBytes+1)},
		{"bad base64", "v1.!"},
		{"empty payload", "v1."},
		{"padded base64", canonical + "="},
		{"base64 newline", canonical + "\n"},
		{"not an array", encodedJSON(`{"credential":"node"}`)},
		{"one item", encodedJSON(`["credential"]`)},
		{"three items", encodedJSON(`["credential","node","extra"]`)},
		{"nonstring item", encodedJSON(`["credential",1]`)},
		{"null item", encodedJSON(`[null,"node"]`)},
		{"invalid identifier", encodedJSON(`["credential","node/path"]`)},
		{"trailing data", encodedJSON(`["credential","node"][]`)},
		{"JSON whitespace", encodedJSON(`["credential", "node"]`)},
		{"escaped spelling", encodedJSON(`["\u0063redential","node"]`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if c, n, err := DecodeMappedUsername(tc.username); err == nil || c != "" || n != "" {
				t.Fatalf("accepted noncanonical username: %q %q %v", c, n, err)
			}
		})
	}
	for _, id := range []string{"", ".leading", "contains space", "node/path", "节点", "a" + strings.Repeat("x", 128)} {
		if _, err := EncodeMappedUsername(id, "node"); err == nil {
			t.Fatalf("invalid identifier accepted: %q", id)
		}
		if _, err := EncodeMappedUsername("credential", id); err == nil {
			t.Fatalf("invalid node identifier accepted: %q", id)
		}
	}
}

func TestMappedUsernameBasicAuthenticationAndLimit(t *testing.T) {
	username, err := EncodeMappedUsername("c"+strings.Repeat(":", 127), "n"+strings.Repeat(".", 127))
	if err != nil || len(username) <= 128 {
		t.Fatalf("expected long encoded username: %d %v", len(username), err)
	}
	password := strings.Repeat("p", 32)
	dial := func(context.Context, string, string) (net.Conn, error) { return nil, nil }
	h, err := New(map[string]string{username: password}, dial, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = h.Close() }()
	r := httptest.NewRequest("CONNECT", "http://example.invalid", nil)
	r.SetBasicAuth(username, password)
	r.Header.Set("Proxy-Authorization", r.Header.Get("Authorization"))
	r.Header.Del("Authorization")
	if got, ok := h.authenticate(r); !ok || got != username {
		t.Fatal("mapped IDs containing colons failed Basic authentication")
	}
	r.SetBasicAuth(username, strings.Repeat("q", 32))
	r.Header.Set("Proxy-Authorization", r.Header.Get("Authorization"))
	if _, ok := h.authenticate(r); ok {
		t.Fatal("wrong password accepted")
	}
	for _, size := range []int{MaxUsernameBytes, MaxUsernameBytes + 1} {
		handler, err := New(map[string]string{strings.Repeat("u", size): password}, dial, time.Now().Add(time.Minute))
		if size == MaxUsernameBytes && err != nil || size > MaxUsernameBytes && err == nil {
			t.Fatalf("username size %d: %v", size, err)
		}
		if handler != nil {
			_ = handler.Close()
		}
	}
}
