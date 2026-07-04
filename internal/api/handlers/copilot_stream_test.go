package handlers

import (
	"testing"
	"time"

	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
)

func TestWorkbenchStreamEventsFromEnvelopeMapsAnalysisEnvelope(t *testing.T) {
	now := time.Date(2026, 7, 3, 10, 0, 0, 0, time.UTC)
	completedAt := now.Add(3 * time.Second)
	tool := domaincopilot.ToolExecution{
		ID:          "tool-1",
		AdapterID:   "metrics.v1",
		ToolName:    "metrics.anomaly_summary",
		Status:      "completed",
		Summary:     "error rate spiked",
		Input:       map[string]any{"namespace": "payments"},
		Output:      map[string]any{"errorRate": "12%"},
		StartedAt:   now,
		CompletedAt: &completedAt,
	}
	events := workbenchStreamEventsFromEnvelope("session-1", domaincopilot.SessionMessageEnvelope{
		Messages: []domaincopilot.Message{{
			ID:        "msg-assistant",
			SessionID: "session-1",
			Role:      "assistant",
			Content:   "错误率突增，建议先回看最近发布。",
			Metadata:  map[string]any{"providerId": "internal", "providerKind": "internal"},
			CreatedAt: now,
		}},
		AnalysisArtifacts: []domaincopilot.AnalysisArtifact{{
			Kind:           "performance",
			RunID:          "run-1",
			Summary:        "发现错误率突增。",
			ToolExecutions: []domaincopilot.ToolExecution{tool},
			Evidence: []domaincopilot.RootCauseEvidence{{
				ID:      "metric-1",
				Kind:    "metrics.signal",
				Title:   "Error Rate",
				Summary: "latest=12%",
			}},
		}},
	}, now)

	wantTypes := []string{
		"thinking.delta",
		"tool.started",
		"tool.completed",
		"source.updated",
		"artifact.updated",
		"thinking.done",
		"message.delta",
		"message.done",
		"agent.status",
	}
	if len(events) != len(wantTypes) {
		t.Fatalf("event count = %d, want %d: %#v", len(events), len(wantTypes), events)
	}
	for index, want := range wantTypes {
		if events[index].Type != want {
			t.Fatalf("event[%d].type = %q, want %q", index, events[index].Type, want)
		}
	}
	if events[1].ToolCall == nil || events[1].ToolCall.Status != "running" {
		t.Fatalf("started tool event not running: %#v", events[1].ToolCall)
	}
	if events[2].ToolCall == nil || events[2].ToolCall.Status != "success" || events[2].ToolCall.DurationMs != 3000 {
		t.Fatalf("completed tool event mismatch: %#v", events[2].ToolCall)
	}
	if events[3].Source == nil || events[3].Source.Kind != "metric" {
		t.Fatalf("source event mismatch: %#v", events[3].Source)
	}
	if events[7].Content != "错误率突增，建议先回看最近发布。" {
		t.Fatalf("message.done content mismatch: %#v", events[7])
	}
	if events[8].Status != "succeeded" {
		t.Fatalf("final status mismatch: %#v", events[8])
	}
}
