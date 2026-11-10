package cluster

import (
	"errors"
	"testing"
)

func TestExecutionConnectionFenceBindsOnlyRuntimeConfiguration(t *testing.T) {
	original := Connection{Summary: Summary{ID: "owned"}, Metadata: map[string]any{"kubeconfig_data": "private"}}
	fingerprint, err := RuntimeConnectionFingerprint(original)
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithExpectedExecutionConnection(t.Context(), "owned", fingerprint)
	for _, scenario := range []string{"same", "health", "credentials", "mode", "identity", "source", "invalid"} {
		t.Run(scenario, func(t *testing.T) {
			current := original
			switch scenario {
			case "same":
				current.Summary.ConnectionMode = ConnectionModeDirectKubeconfig
			case "health":
				current.Summary.Health.Status = "healthy"
			case "credentials":
				current.Metadata = map[string]any{"kubeconfig_data": "other"}
			case "mode":
				current.Summary.ConnectionMode = ConnectionModeAgent
			case "identity":
				current.Summary.ID = "other"
			case "source":
				current.SourceRef = "other"
			case "invalid":
				current.Metadata = map[string]any{"invalid": make(chan int)}
			}
			err := VerifyExecutionConnection(ctx, current)
			allowed := scenario == "same" || scenario == "health"
			if (err == nil) != allowed || !allowed && !errors.Is(err, ErrExecutionConnectionChanged) {
				t.Fatalf("allowed=%v error=%v", allowed, err)
			}
		})
	}
	if VerifyExecutionConnection(t.Context(), Connection{}) != nil || HasExpectedExecutionConnection(t.Context()) || !HasExpectedExecutionConnection(ctx) {
		t.Fatal("ordinary callers changed or expected fence lost")
	}
}
