package relaywebsocket

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRelayAllowWebSocketOrigin(t *testing.T) {
	tests := []struct {
		name    string
		host    string
		origins []string
		want    bool
	}{
		{name: "missing origin", host: "gateway.example", want: true},
		{name: "same public host and port", host: "gateway.example:8443", origins: []string{"https://gateway.example:8443"}, want: true},
		{name: "different public port", host: "gateway.example:8443", origins: []string{"https://gateway.example:9443"}},
		{name: "local cross port", host: "127.0.0.1:8080", origins: []string{"http://localhost:3000"}, want: true},
		{name: "duplicate origin", host: "gateway.example", origins: []string{"https://gateway.example", "https://evil.example"}},
		{name: "origin path", host: "gateway.example", origins: []string{"https://gateway.example/path"}},
		{name: "unsupported scheme", host: "gateway.example", origins: []string{"file://gateway.example"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "http://"+test.host+"/realtime", nil)
			for _, origin := range test.origins {
				req.Header.Add("Origin", origin)
			}
			if got := relayAllowWebSocketOrigin(req); got != test.want {
				t.Fatalf("relayAllowWebSocketOrigin() = %v, want %v", got, test.want)
			}
		})
	}
}
