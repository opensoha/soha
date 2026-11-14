package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestHasAnyBearerToken(t *testing.T) {
	tests := []struct {
		name          string
		authorization string
		allowed       []string
		want          bool
	}{
		{name: "matching token", authorization: "Bearer agent-token", allowed: []string{"agent-token"}, want: true},
		{
			name:          "matching second token",
			authorization: "Bearer runner-token",
			allowed:       []string{"agent-token", "runner-token"},
			want:          true,
		},
		{name: "case insensitive scheme", authorization: "bearer agent-token", allowed: []string{"agent-token"}, want: true},
		{name: "wrong token", authorization: "Bearer wrong-token", allowed: []string{"agent-token"}},
		{name: "missing token", allowed: []string{"agent-token"}},
		{name: "wrong scheme", authorization: "Basic agent-token", allowed: []string{"agent-token"}},
		{name: "embedded whitespace", authorization: "Bearer agent token", allowed: []string{"agent token"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.authorization != "" {
				req.Header.Set("Authorization", tt.authorization)
			}
			if got := requestHasAnyBearerToken(req, tt.allowed); got != tt.want {
				t.Fatalf("requestHasAnyBearerToken() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSecureTokenEqual(t *testing.T) {
	tests := []struct {
		name      string
		presented string
		expected  string
		want      bool
	}{
		{name: "equal", presented: "secret-token", expected: "secret-token", want: true},
		{name: "different content", presented: "secret-tokee", expected: "secret-token"},
		{name: "different length", presented: "secret-token-long", expected: "secret-token"},
		{name: "empty", presented: "", expected: "", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := secureTokenEqual(tt.presented, tt.expected); got != tt.want {
				t.Fatalf("secureTokenEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRequestHasAnyBearerTokenRejectsMultipleHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Add("Authorization", "Bearer agent-token")
	req.Header.Add("Authorization", "Bearer runner-token")
	if requestHasAnyBearerToken(req, []string{"agent-token", "runner-token"}) {
		t.Fatal("requestHasAnyBearerToken() accepted multiple Authorization headers")
	}
}
