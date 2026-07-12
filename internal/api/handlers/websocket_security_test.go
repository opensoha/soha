package handlers

import (
	"net/http/httptest"
	"testing"
)

func TestAllowWebSocketOrigin(t *testing.T) {
	tests := []struct {
		name    string
		host    string
		origins []string
		want    bool
	}{
		{name: "missing origin", host: "console.example", want: true},
		{name: "same public host and port", host: "console.example:8443", origins: []string{"https://console.example:8443"}, want: true},
		{name: "different public port", host: "console.example:8443", origins: []string{"https://console.example:9443"}},
		{name: "local development cross port", host: "127.0.0.1:8080", origins: []string{"http://localhost:3000"}, want: true},
		{name: "duplicate origin", host: "console.example", origins: []string{"https://console.example", "https://evil.example"}},
		{name: "origin path", host: "console.example", origins: []string{"https://console.example/path"}},
		{name: "origin credentials", host: "console.example", origins: []string{"https://user@console.example"}},
		{name: "unsupported scheme", host: "console.example", origins: []string{"file://console.example"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "http://"+test.host+"/stream", nil)
			for _, origin := range test.origins {
				req.Header.Add("Origin", origin)
			}
			if got := allowWebSocketOrigin(req); got != test.want {
				t.Fatalf("allowWebSocketOrigin() = %v, want %v", got, test.want)
			}
		})
	}
}
