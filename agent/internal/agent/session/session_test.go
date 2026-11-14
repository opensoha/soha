package session

import "testing"

func TestSessionURLUsesAccessURLSchemeAndPath(t *testing.T) {
	tests := []struct {
		accessURL string
		want      string
	}{
		{accessURL: "http://soha.example.com", want: "ws://soha.example.com/api/v1/agent-sessions/connect?clusterId=cluster-a"},
		{accessURL: "https://soha.example.com/console", want: "wss://soha.example.com/console/api/v1/agent-sessions/connect?clusterId=cluster-a"},
		{accessURL: "https://soha.example.com/api/v1", want: "wss://soha.example.com/api/v1/agent-sessions/connect?clusterId=cluster-a"},
	}
	for _, test := range tests {
		t.Run(test.accessURL, func(t *testing.T) {
			got, err := sessionURL(test.accessURL, "cluster-a")
			if err != nil {
				t.Fatalf("sessionURL() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("sessionURL() = %q, want %q", got, test.want)
			}
		})
	}
}
