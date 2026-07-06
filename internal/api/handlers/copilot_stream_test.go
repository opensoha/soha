package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

func TestStreamMessageAssignsMonotonicSequences(t *testing.T) {
	service := &streamHandlerService{result: domaincopilot.WorkbenchStreamResult{
		Envelope: domaincopilot.SessionMessageEnvelope{},
		Events: []domaincopilot.WorkbenchStreamEvent{{
			Type:         "agent.status",
			ProviderID:   "hermes",
			ProviderKind: "hermes",
			Status:       "queued",
		}, {
			Type:         "message.delta",
			Role:         "assistant",
			ContentDelta: "hello",
		}, {
			Type:    "message.done",
			Role:    "assistant",
			Content: "hello",
		}},
	}}
	events := performStreamMessageRequest(t, service, `{"content":"hi","mode":"general"}`)

	if len(events) != 3 {
		t.Fatalf("expected 3 service events, got %#v", events)
	}
	if events[0].ProviderID != "hermes" || events[0].ProviderKind != "hermes" || events[0].Status != "queued" {
		t.Fatalf("handler should preserve service-owned provider status, got %#v", events[0])
	}
	for index, event := range events {
		wantSeq := index + 1
		if event.Sequence != wantSeq {
			t.Fatalf("event[%d].sequence = %d, want %d: %#v", index, event.Sequence, wantSeq, event)
		}
		if event.ID == "" || event.SessionID != "session-1" || event.CreatedAt.IsZero() {
			t.Fatalf("event[%d] missing server-owned fields: %#v", index, event)
		}
	}
}

func TestStreamMessagePassesRequestModeAndEmitsGeneralVsAnalysisEvents(t *testing.T) {
	service := &streamHandlerService{}
	generalEvents := performStreamMessageRequest(t, service, `{"content":"hi","mode":"general"}`)
	analysisEvents := performStreamMessageRequest(t, service, `{"content":"inspect","mode":"inspection_review","toolset":{"enabledAdapterIds":["platform-native.v1"]},"scopeOverrides":{"namespace":"payments"},"source":"global-assistant","launchContext":{"sourceWorkbench":"platform","sourceRoute":"/platform/pods","entityKind":"kubernetes.pod","entityName":"pod-a"},"selectionContext":{"text":"ERROR failed","kind":"log"},"pinnedContext":{"sourceWorkbench":"platform","entityName":"pod-a"}}`)

	if len(service.inputs) != 2 {
		t.Fatalf("expected two stream inputs, got %#v", service.inputs)
	}
	if service.inputs[0].Mode != "general" || service.inputs[1].Mode != "inspection_review" {
		t.Fatalf("request modes were not passed through: %#v", service.inputs)
	}
	if len(service.inputs[1].Toolset.EnabledAdapterIDs) != 1 || service.inputs[1].Toolset.EnabledAdapterIDs[0] != "platform-native.v1" {
		t.Fatalf("toolset was not decoded: %#v", service.inputs[1].Toolset)
	}
	if service.inputs[1].ScopeOverrides["namespace"] != "payments" {
		t.Fatalf("scope overrides were not passed through: %#v", service.inputs[1].ScopeOverrides)
	}
	if service.inputs[1].Source != "global-assistant" || service.inputs[1].LaunchContext == nil || service.inputs[1].LaunchContext.EntityName != "pod-a" {
		t.Fatalf("launch context was not passed through: %#v", service.inputs[1])
	}
	if service.inputs[1].SelectionContext == nil || service.inputs[1].SelectionContext.Kind != "log" {
		t.Fatalf("selection context was not passed through: %#v", service.inputs[1].SelectionContext)
	}
	if service.inputs[1].PinnedContext["entityName"] != "pod-a" {
		t.Fatalf("pinned context was not passed through: %#v", service.inputs[1].PinnedContext)
	}
	if hasEventType(generalEvents, "thinking.delta") {
		t.Fatalf("general stream should not include analysis thinking events: %#v", generalEvents)
	}
	if !hasEventType(analysisEvents, "thinking.delta") || !hasEventType(analysisEvents, "tool.completed") {
		t.Fatalf("analysis stream missing analysis events: %#v", analysisEvents)
	}
}

func TestStreamMessageLiveSinkFlushesBeforeServiceReturns(t *testing.T) {
	liveStarted := make(chan struct{})
	releaseService := make(chan struct{})
	service := &streamHandlerService{liveStarted: liveStarted, releaseService: releaseService}
	router := gin.New()
	handler := NewCopilotHandler(service)
	router.POST("/sessions/:sessionID/messages/stream", func(c *gin.Context) {
		c.Set("principal", domainidentity.Principal{UserID: "user-1"})
		handler.StreamMessage(c)
	})
	req := httptest.NewRequest(http.MethodPost, "/sessions/session-1/messages/stream", strings.NewReader(`{"content":"inspect","mode":"inspection_review"}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		router.ServeHTTP(recorder, req)
		close(done)
	}()

	<-liveStarted
	events := parseSSEEvents(t, recorder.Body.String())
	if !hasEventType(events, "tool.started") {
		t.Fatalf("expected live tool.started before service returned, got %#v", events)
	}
	close(releaseService)
	<-done
}

type streamHandlerService struct {
	CopilotService
	result         domaincopilot.WorkbenchStreamResult
	inputs         []domaincopilot.WorkbenchSendMessageInput
	liveStarted    chan struct{}
	releaseService chan struct{}
}

func (s *streamHandlerService) StreamMessage(_ context.Context, _ domainidentity.Principal, sessionID string, input domaincopilot.WorkbenchSendMessageInput, _ string) (domaincopilot.WorkbenchStreamResult, error) {
	s.inputs = append(s.inputs, input)
	if s.liveStarted != nil {
		input.EventSink(domaincopilot.WorkbenchStreamEvent{
			Type: "tool.started",
			ToolCall: &domaincopilot.WorkbenchToolCall{
				ID:       "tool-live",
				ToolName: "inspection.review",
				Status:   "running",
			},
		})
		close(s.liveStarted)
		<-s.releaseService
		return domaincopilot.WorkbenchStreamResult{Events: []domaincopilot.WorkbenchStreamEvent{{
			Type:    "message.done",
			Content: "done",
		}, {
			Type:   "agent.status",
			Status: "succeeded",
		}}}, nil
	}
	if len(s.result.Events) > 0 {
		return s.result, nil
	}
	if input.Mode == "general" {
		return domaincopilot.WorkbenchStreamResult{Events: []domaincopilot.WorkbenchStreamEvent{{
			Type:    "message.done",
			Content: "general reply",
		}}}, nil
	}
	return domaincopilot.WorkbenchStreamResult{Events: []domaincopilot.WorkbenchStreamEvent{{
		Type:      "thinking.delta",
		TextDelta: "analysis",
	}, {
		Type: "tool.completed",
		ToolCall: &domaincopilot.WorkbenchToolCall{
			ID:       "tool-1",
			ToolName: "inspection.review",
			Status:   "success",
		},
	}, {
		Type:    "message.done",
		Content: "analysis reply",
	}, {
		Type:   "agent.status",
		Status: "succeeded",
	}}}, nil
}

func performStreamMessageRequest(t *testing.T, service *streamHandlerService, body string) []domaincopilot.WorkbenchStreamEvent {
	t.Helper()
	router := gin.New()
	handler := NewCopilotHandler(service)
	router.POST("/sessions/:sessionID/messages/stream", func(c *gin.Context) {
		c.Set("principal", domainidentity.Principal{UserID: "user-1"})
		handler.StreamMessage(c)
	})
	req := httptest.NewRequest(http.MethodPost, "/sessions/session-1/messages/stream", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("stream status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	return parseSSEEvents(t, recorder.Body.String())
}

func parseSSEEvents(t *testing.T, body string) []domaincopilot.WorkbenchStreamEvent {
	t.Helper()
	events := make([]domaincopilot.WorkbenchStreamEvent, 0)
	for _, block := range strings.Split(body, "\n\n") {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		data := strings.TrimPrefix(block, "data: ")
		var event domaincopilot.WorkbenchStreamEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			t.Fatalf("decode SSE event %q: %v", data, err)
		}
		events = append(events, event)
	}
	return events
}

func hasEventType(events []domaincopilot.WorkbenchStreamEvent, eventType string) bool {
	for _, event := range events {
		if event.Type == eventType {
			return true
		}
	}
	return false
}
