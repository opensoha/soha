package copilot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/redaction"
)

type chatDelegateRepository interface {
	CreateDelegatedAgentRun(context.Context, string, domaincopilot.AgentRun) (domaincopilot.AgentRun, error)
}

func (s *Service) delegateChatSpecialist(ctx context.Context, parent domaincopilot.AgentRun, input map[string]any) (map[string]any, error) {
	query := strings.TrimSpace(stringValue(input["query"]))
	if parent.ParentRunID != "" || query == "" || len([]rune(query)) > 2048 {
		return nil, fmt.Errorf("%w: one specialist requires a bounded question; recursive delegation is disabled", apperrors.ErrInvalidArgument)
	}
	if remainingChatToolEvidenceTokens(parent) < 256 {
		return nil, fmt.Errorf("%w: parent evidence allowance exhausted", apperrors.ErrInvalidArgument)
	}
	repository, ok := s.agentRuns.(chatDelegateRepository)
	if !ok {
		return nil, fmt.Errorf("%w: specialist delegation is unavailable", apperrors.ErrUnsupportedOperation)
	}
	principal, err := agentToolPrincipal(parent)
	if err != nil {
		return nil, err
	}
	bindings := []domaincopilot.AgentToolBinding{}
	for _, binding := range parent.ToolBindings {
		switch binding.ToolName {
		case "knowledge.search", "k8s.workloads.overview", "k8s.nodes.detail", "k8s.services.backends":
			bindings = append(bindings, binding)
		}
	}
	if len(bindings) == 0 {
		return nil, fmt.Errorf("%w: no authorized specialist tools", apperrors.ErrAccessDenied)
	}
	remaining := int(time.Until(parent.QueuedAt.Add(time.Duration(parent.TimeoutSeconds) * time.Second)).Seconds())
	if remaining <= 0 {
		return nil, fmt.Errorf("%w: parent deadline exceeded", apperrors.ErrInvalidArgument)
	}
	chat := sohaapi.AgentChatInput{Question: query, Locale: stringValue(parent.Input["locale"]), Context: "You are the read-only evidence specialist for one parent task. Use the available authorized knowledge or Kubernetes tools to investigate the question. Return concise findings with exact citation IDs and explicitly state missing evidence. Do not make changes, request approvals, delegate, or assume access to parent conversation history. Limit your response to 2000 characters."}
	child, err := s.buildAgentRun(ctx, principal, domaincopilot.AgentRunInput{
		ParentRunID: parent.ID, ProviderID: parent.ProviderID, CapabilityID: "general", SessionID: parent.SessionID, CreatedBy: parent.CreatedBy,
		Scope: parent.Scope, Toolset: parent.Toolset, ToolBindings: bindings, TimeoutSeconds: remaining,
		Input: map[string]any{"question": query, "mode": "general", "locale": chat.Locale, "chat": chat, "_sohaKnowledgeBaseIds": parent.Input["_sohaKnowledgeBaseIds"]},
	})
	if err != nil {
		return nil, err
	}
	child.ID = parent.ID + ":specialist"
	child.SkillBindings = nil
	child, err = repository.CreateDelegatedAgentRun(ctx, parent.ID, child)
	if err != nil {
		return nil, err
	}
	if stringValue(child.Input["question"]) != query {
		return nil, fmt.Errorf("%w: this run already delegated a different question", apperrors.ErrConflict)
	}
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for !agentRunStatusTerminal(child.Status) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
			return chatSpecialistResult(child, remainingChatToolEvidenceTokens(parent)), nil
		case <-ticker.C:
			child, err = s.agentRuns.GetAgentRun(ctx, parent.CreatedBy, child.ID)
			if err != nil {
				return nil, err
			}
		}
	}
	return chatSpecialistResult(child, remainingChatToolEvidenceTokens(parent)), nil
}

func chatSpecialistResult(child domaincopilot.AgentRun, remaining int) map[string]any {
	content := []rune(redaction.Text(firstNonEmpty(stringValue(child.Output["answer"]), stringValue(child.Output["summary"]), child.ErrorMessage)))
	if len(content) > 2000 {
		content = content[:2000]
	}
	sources := chatRunSources(child)
	result := map[string]any{"status": child.Status, "content": string(content), "sources": sources, "relatedIds": map[string]any{"childRunId": child.ID, "parentRunId": child.ParentRunID}, "instruction": "If pending, repeat agent.delegate with the same query. Do not claim completion until terminal. Queued work requires runner concurrency >= 2."}
	for {
		data, _ := json.Marshal(result)
		if len(data) <= remaining*4 {
			result["estimatedTokens"] = (len(data) + 3) / 4
			return result
		}
		if len(sources) > 0 {
			sources = sources[:len(sources)-1]
			result["sources"] = sources
			continue
		}
		if len(content) > 0 {
			content = content[:len(content)/2]
			result["content"] = string(content)
			continue
		}
		result["estimatedTokens"] = (len(data) + 3) / 4
		return result
	}
}

func (s *Service) cancelChatSpecialist(ctx context.Context, parent domaincopilot.AgentRun) error {
	if parent.ParentRunID != "" || parent.CapabilityID != "general" {
		return nil
	}
	child, err := s.agentRuns.GetAgentRun(ctx, parent.CreatedBy, parent.ID+":specialist")
	if errors.Is(err, apperrors.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if child.ID != parent.ID+":specialist" || child.ParentRunID != parent.ID || agentRunStatusTerminal(child.Status) {
		return nil
	}
	_, err = s.agentRuns.CancelAgentRun(ctx, domaincopilot.AgentRunCancelInput{RunID: child.ID, RequestedBy: parent.CreatedBy, Reason: "parent run ended"})
	return err
}
