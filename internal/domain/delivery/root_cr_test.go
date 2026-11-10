package delivery

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

func TestRootCRTaskPublicProjectionDoesNotMutatePrivatePayload(t *testing.T) {
	private := ExecutionTask{Payload: map[string]any{"rootResource": map[string]any{"uid": "root"}, "action": "apply", "documents": []any{map[string]any{"content": "synthetic-secret"}}}}
	public := WithOperationState(private, time.Now())
	if public.Payload["documents"] != nil || public.Payload["rootResource"] == nil || private.Payload["documents"] == nil {
		t.Fatal("root CR private configuration escaped or durable payload changed")
	}
}

func TestRootCRTaskSummaryOmitsConfigurationDigestsAndDriftValues(t *testing.T) {
	task := ExecutionTask{Payload: map[string]any{"rootResource": map[string]any{"uid": "root"}, "renderedDigest": "configuration-hash"}, Result: map[string]any{"renderedDigest": "configuration-hash", "error": "synthetic-secret", "diagnostics": []any{map[string]any{"message": "synthetic-secret", "field": "spec.synthetic-secret"}}, "preflight": map[string]any{"ready": true, "renderedDigest": "configuration-hash", "diagnostics": []any{map[string]any{"message": "synthetic-secret"}}}, "drift": map[string]any{"password": "synthetic-secret"}}, Artifacts: []ExecutionArtifact{{Metadata: map[string]any{"content": "synthetic-secret"}}}}
	summary := WithOperationState(task, time.Now())
	encoded, _ := json.Marshal(summary)
	if bytes.Contains(encoded, []byte("configuration-hash")) || bytes.Contains(encoded, []byte("synthetic-secret")) {
		t.Fatalf("configuration material leaked: %s", encoded)
	}
	if task.Result["renderedDigest"] != "configuration-hash" {
		t.Fatal("private task result mutated")
	}
}
