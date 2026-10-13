package copilot

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	appmemory "github.com/opensoha/soha/internal/application/memory"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func messageKnowledgeCitations(message domaincopilot.Message) ([]domainknowledge.Citation, error) {
	var metadata struct {
		ContextSnapshot struct {
			Citations []domainknowledge.Citation `json:"citations"`
		} `json:"contextSnapshot"`
		ContextEnvelope struct {
			Citations []domainknowledge.Citation `json:"citations"`
		} `json:"contextEnvelope"`
		ToolExecutions []struct {
			ToolName string         `json:"toolName"`
			Status   string         `json:"status"`
			Output   map[string]any `json:"output"`
		} `json:"toolExecutions"`
	}
	raw, err := json.Marshal(message.Metadata)
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(raw, &metadata); err != nil {
		return nil, err
	}
	citations := append(metadata.ContextSnapshot.Citations, metadata.ContextEnvelope.Citations...)
	for _, tool := range metadata.ToolExecutions {
		if tool.ToolName != "knowledge.search" || tool.Status != "success" {
			continue
		}
		raw, err = json.Marshal(tool.Output["knowledgeCitations"])
		if err != nil {
			return nil, err
		}
		var additional []domainknowledge.Citation
		if err = json.Unmarshal(raw, &additional); err != nil {
			return nil, err
		}
		for _, citation := range additional {
			if citation.KnowledgeBaseID == "" || citation.DocumentID == "" || citation.ChunkID == "" || citation.ContentHash == "" {
				return nil, fmt.Errorf("%w: historical knowledge evidence lacks source lineage", apperrors.ErrInvalidArgument)
			}
		}
		citations = append(citations, additional...)
	}
	seen := map[string]bool{}
	knowledge := make([]domainknowledge.Citation, 0, len(citations))
	for _, citation := range citations {
		if citation.KnowledgeBaseID == "" && !strings.HasPrefix(citation.URI, "memory:") {
			continue
		}
		key := knowledgeCitationKey(citation)
		if !seen[key] {
			knowledge = append(knowledge, citation)
			seen[key] = true
		}
	}
	if len(knowledge) > 200 {
		return nil, fmt.Errorf("%w: historical citation budget exceeded", apperrors.ErrInvalidArgument)
	}
	return knowledge, nil
}

func knowledgeCitationKey(citation domainknowledge.Citation) string {
	return citation.KnowledgeBaseID + ":" + citation.DocumentID + ":" + citation.ChunkID + ":" + citation.ContentHash + ":" + citation.DocumentVersionID + ":" + citation.URI
}

func (s *Service) validateMessageKnowledge(ctx context.Context, principal domainidentity.Principal, message domaincopilot.Message) ([]domainknowledge.Citation, error) {
	if err := s.validateMessageTaskEvidence(ctx, principal, message); err != nil {
		return nil, err
	}
	if mode := stringValue(message.Metadata["mode"]); message.Role == "assistant" && isRunnableSessionAnalysisMode(mode) {
		if err := s.authorizeProfessionalCapability(ctx, principal, mode); err != nil {
			return nil, err
		}
	}
	citations, err := messageKnowledgeCitations(message)
	if err != nil || len(citations) == 0 {
		return citations, err
	}
	if err := s.validateMessageMemory(ctx, principal, citations); err != nil {
		return nil, err
	}
	knowledge := slices.DeleteFunc(slices.Clone(citations), func(c domainknowledge.Citation) bool { return c.KnowledgeBaseID == "" })
	if len(knowledge) == 0 {
		return citations, nil
	}
	reader, ok := s.contextBuilder.(interface {
		ValidateKnowledgeCitations(context.Context, domainidentity.Principal, []domainknowledge.Citation) error
	})
	if !ok {
		return nil, fmt.Errorf("%w: current citation validation unavailable", apperrors.ErrAccessDenied)
	}
	return citations, reader.ValidateKnowledgeCitations(ctx, principal, knowledge)
}

func (s *Service) validateMessageMemory(ctx context.Context, principal domainidentity.Principal, citations []domainknowledge.Citation) error {
	ctx = boundedMemoryLineage(ctx)
	memories := slices.DeleteFunc(slices.Clone(citations), func(c domainknowledge.Citation) bool { return !strings.HasPrefix(c.URI, "memory:") })
	if len(memories) == 0 {
		return nil
	}
	if s.chatMemory == nil {
		return apperrors.ErrAccessDenied
	}
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermAIMemoryView); err != nil {
		return err
	}
	records, err := s.chatMemory.ListRecords(ctx, "user", principal.UserID)
	if err != nil {
		return err
	}
	for _, citation := range memories {
		id := strings.TrimPrefix(citation.URI, "memory:")
		index := slices.IndexFunc(records, func(record appmemory.Record) bool {
			return record.ID == id && chatMemoryAvailable(record, principal.UserID, time.Now())
		})
		if index < 0 || citation.ContentHash != chatMemoryContentHash(records[index]) {
			return apperrors.ErrAccessDenied
		}
		if _, err := s.authorizeChatMemoryRecord(ctx, principal, records[index]); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) groundedHistory(ctx context.Context, principal domainidentity.Principal, history []domaincopilot.Message) ([]domaincopilot.Message, bool) {
	visible, excluded, _ := s.groundedHistorySelections(ctx, principal, history)
	return visible, len(excluded) > 0
}

func (s *Service) groundedHistorySelections(ctx context.Context, principal domainidentity.Principal, history []domaincopilot.Message) ([]domaincopilot.Message, []chatContextExclusion, map[string][]domainknowledge.Citation) {
	visible := make([]domaincopilot.Message, 0, len(history))
	var excluded []chatContextExclusion
	sources := make(map[string][]domainknowledge.Citation)
	for _, message := range history {
		citations, err := s.validateMessageKnowledge(ctx, principal, message)
		if err != nil {
			excluded = append(excluded, chatContextExclusion{MessageID: message.ID, Reason: "source_unavailable"})
			continue
		}
		visible = append(visible, message)
		sources[message.ID] = citations
	}
	return visible, excluded, sources
}

func (s *Service) visibleKnowledgeMessage(ctx context.Context, principal domainidentity.Principal, message domaincopilot.Message) domaincopilot.Message {
	if _, err := s.validateMessageKnowledge(ctx, principal, message); err == nil {
		return message
	}
	message.Content = "此回答引用的知识已失效或暂不可访问，请重新获取依据。"
	metadata := map[string]any{"knowledgeUnavailable": true}
	for _, key := range []string{"mode", "source", "agentRunId", "requestMessageId"} {
		if value, ok := message.Metadata[key]; ok {
			metadata[key] = value
		}
	}
	status := map[string]any{}
	for _, key := range []string{"providerId", "providerKind", "status", "runId", "agentRunId", "externalRunId", "executionOwner", "phase", "stopReason", "eventSequence", "usageKnown"} {
		if value, ok := mapValue(message.Metadata["agentStatus"])[key]; ok {
			status[key] = value
		}
	}
	if len(status) > 0 {
		metadata["agentStatus"] = status
	}
	message.Metadata = metadata
	return message
}

func agentRunKnowledgeMessage(run domaincopilot.AgentRun) domaincopilot.Message {
	return domaincopilot.Message{Role: "assistant", Metadata: map[string]any{"mode": run.CapabilityID, "contextSnapshot": run.Output["contextSnapshot"], "contextEnvelope": run.Input["contextSnapshot"], "toolExecutions": run.ToolExecutions, "capabilityTaskIds": run.Output["capabilityTaskIds"]}}
}

func (s *Service) visibleKnowledgeAgentRun(ctx context.Context, principal domainidentity.Principal, run domaincopilot.AgentRun) domaincopilot.AgentRun {
	if _, err := s.validateMessageKnowledge(ctx, principal, agentRunKnowledgeMessage(run)); err == nil {
		return run
	}
	// Keep durable identity and lifecycle visible while withholding every
	// projection that may contain an answer derived from a revoked source.
	output := map[string]any{"knowledgeUnavailable": true, "answer": "任务引用的知识已失效或暂不可访问，请重新获取依据。"}
	for _, key := range []string{"phase", "stopReason", "eventSequence", "usageKnown", "usage"} {
		if value, ok := run.Output[key]; ok {
			output[key] = value
		}
	}
	run.Input, run.Output = map[string]any{}, output
	run.AnalysisArtifacts, run.ToolExecutions = nil, nil
	run.ErrorMessage = ""
	return run
}

func (s *Service) visibleKnowledgeEvents(ctx context.Context, principal domainidentity.Principal, run domaincopilot.AgentRun, page domaincopilot.NativeEventPage) domaincopilot.NativeEventPage {
	if _, err := s.validateMessageKnowledge(ctx, principal, agentRunKnowledgeMessage(run)); err == nil || len(page.Items) == 0 {
		return page
	}
	last := page.Items[len(page.Items)-1]
	page.Items = []domaincopilot.WorkbenchStreamEvent{{ID: last.ID, Sequence: last.Sequence, RunID: last.RunID, SessionID: last.SessionID, CreatedAt: last.CreatedAt, Type: "error", Code: "knowledge_unavailable", Message: "任务引用的知识已失效或暂不可访问，请重新获取依据。"}}
	return page
}
