package handlers

import "testing"

func TestStreamExitMessageMapsKinds(t *testing.T) {
	tests := []struct {
		kind streamExitKind
		want string
	}{
		{streamExitKindPodLogs, "log stream closed"},
		{streamExitKindPodTerminal, "terminal session closed"},
		{streamExitKindDockerLogs, "docker log stream closed"},
		{streamExitKindDockerTerminal, "docker terminal session closed"},
		{streamExitKindTaskUpdates, "task stream closed"},
		{"unknown", "stream closed"},
	}

	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			if got := streamExitMessage(tt.kind); got != tt.want {
				t.Fatalf("streamExitMessage(%q) = %q, want %q", tt.kind, got, tt.want)
			}
		})
	}
}
