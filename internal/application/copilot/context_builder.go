package copilot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appknowledge "github.com/opensoha/soha/internal/application/knowledge"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/requestctx"
)

type ContextKnowledgeSearcher interface {
	Search(context.Context, domainidentity.Principal, domainknowledge.SearchRequest) (domainknowledge.SearchResult, error)
}

type ContextBuilder struct {
	knowledge   ContextKnowledgeSearcher
	permissions *appaccess.PermissionResolver
	now         func() time.Time
}

func NewContextBuilder(knowledge ContextKnowledgeSearcher, permissions *appaccess.PermissionResolver) *ContextBuilder {
	return &ContextBuilder{knowledge: knowledge, permissions: permissions, now: func() time.Time { return time.Now().UTC() }}
}

func (b *ContextBuilder) Inspect(ctx context.Context, principal domainidentity.Principal, input domaincopilot.ContextBuildInput) (domaincopilot.ContextInspection, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, b.permissions, principal, appknowledge.PermContextInspect); err != nil {
		return domaincopilot.ContextInspection{}, err
	}
	envelope, retrievalMS, truncations, err := b.build(ctx, principal, input)
	if err != nil {
		return domaincopilot.ContextInspection{}, err
	}
	sections := []string{"task", "session", "budgets", "policy"}
	if len(envelope.Evidence) > 0 {
		sections = append(sections, "evidence", "citations")
	}
	if len(envelope.Skills) > 0 {
		sections = append(sections, "skills")
	}
	if len(envelope.Tools) > 0 {
		sections = append(sections, "tools")
	}
	return domaincopilot.ContextInspection{Envelope: envelope, Sections: sections, Truncations: truncations, RetrievalTime: retrievalMS}, nil
}

// BuildForCopilot is called after Copilot chat authorization. Knowledge.Search performs
// its own permission and document ACL checks, so the builder never broadens access.
func (b *ContextBuilder) BuildForCopilot(ctx context.Context, principal domainidentity.Principal, input domaincopilot.ContextBuildInput) (domaincopilot.ContextEnvelope, error) {
	envelope, _, _, err := b.build(ctx, principal, input)
	return envelope, err
}

func (b *ContextBuilder) build(ctx context.Context, principal domainidentity.Principal, input domaincopilot.ContextBuildInput) (domaincopilot.ContextEnvelope, int64, []string, error) {
	input.Task.Goal = strings.TrimSpace(input.Task.Goal)
	if input.Task.Goal == "" {
		return domaincopilot.ContextEnvelope{}, 0, nil, fmt.Errorf("%w: context task goal is required", apperrors.ErrInvalidArgument)
	}
	budgets := normalizeContextBudgets(input.Budgets)
	evidence := make([]domaincopilot.ContextEvidence, 0)
	citations := make([]domainknowledge.Citation, 0)
	truncations := make([]string, 0)
	retrievalMS := int64(0)
	if input.Knowledge.Enabled {
		if b.knowledge == nil {
			return domaincopilot.ContextEnvelope{}, 0, nil, fmt.Errorf("%w: knowledge search is not configured", apperrors.ErrUnsupportedOperation)
		}
		query := strings.TrimSpace(input.Knowledge.Query)
		if query == "" {
			query = input.Task.Goal
		}
		result, err := b.knowledge.Search(ctx, principal, domainknowledge.SearchRequest{KnowledgeBaseIDs: input.Knowledge.KnowledgeBaseIDs, Query: query, TopK: input.Knowledge.TopK})
		if err != nil {
			return domaincopilot.ContextEnvelope{}, 0, nil, err
		}
		retrievalMS = result.TimingMS
		used := 0
		for _, hit := range result.Hits {
			tokens := max(1, (len([]rune(hit.Content))+3)/4)
			if used+tokens > budgets.MaxEvidenceTokens {
				truncations = append(truncations, "evidence:maxEvidenceTokens")
				break
			}
			evidence = append(evidence, domaincopilot.ContextEvidence{CitationID: hit.Citation.ID, Content: hit.Content, TokenCount: tokens})
			citations = append(citations, hit.Citation)
			used += tokens
		}
	}
	meta := requestctx.FromContext(ctx)
	requestID := strings.TrimSpace(input.RequestID)
	if requestID == "" {
		requestID = meta.RequestID
	}
	if requestID == "" {
		requestID = uuid.NewString()
	}
	envelope := domaincopilot.ContextEnvelope{Version: "v1", ID: uuid.NewString(), RequestID: requestID, SessionID: strings.TrimSpace(input.SessionID), AgentRunID: strings.TrimSpace(input.AgentRunID), Principal: domaincopilot.ContextPrincipal{UserID: principal.UserID}, Task: input.Task, Prompt: input.Prompt, Skills: input.Skills, Session: input.Session, Evidence: evidence, Citations: citations, Tools: input.Tools, Environment: input.Environment, Budgets: budgets, BudgetUsage: domaincopilot.ContextBudgetUsage{EvidenceTokens: evidenceTokenCount(evidence), EvidenceItems: len(evidence)}, PolicySnapshot: domaincopilot.ContextPolicySnapshot{ID: "knowledge-acl", Version: "v1"}, CreatedAt: b.now()}
	payload, _ := json.Marshal(envelope)
	hash := sha256.Sum256(payload)
	envelope.ContentHash = hex.EncodeToString(hash[:])
	return envelope, retrievalMS, truncations, nil
}

func normalizeContextBudgets(budgets domaincopilot.ContextBudgets) domaincopilot.ContextBudgets {
	if budgets.MaxInputTokens <= 0 {
		budgets.MaxInputTokens = 16000
	}
	budgets.MaxInputTokens = min(budgets.MaxInputTokens, 128000)
	if budgets.MaxEvidenceTokens <= 0 {
		budgets.MaxEvidenceTokens = min(6000, budgets.MaxInputTokens/2)
	}
	budgets.MaxEvidenceTokens = min(budgets.MaxEvidenceTokens, budgets.MaxInputTokens)
	if budgets.MaxSteps <= 0 {
		budgets.MaxSteps = 8
	}
	budgets.MaxSteps = min(budgets.MaxSteps, 64)
	return budgets
}

func evidenceTokenCount(items []domaincopilot.ContextEvidence) int {
	total := 0
	for _, item := range items {
		total += item.TokenCount
	}
	return total
}

func contextEvidenceSystemMessage(envelope domaincopilot.ContextEnvelope) chatProviderMessage {
	var builder strings.Builder
	builder.WriteString("Use the following authorized knowledge evidence when relevant. Cite evidence using [citation:<id>]. Do not claim the evidence says more than its text.\n")
	for _, item := range envelope.Evidence {
		builder.WriteString("\n[citation:")
		builder.WriteString(item.CitationID)
		builder.WriteString("]\n")
		builder.WriteString(item.Content)
		builder.WriteByte('\n')
	}
	return chatProviderMessage{Role: "system", Content: builder.String()}
}

func contextSnapshot(envelope domaincopilot.ContextEnvelope) map[string]any {
	refs := make([]string, 0, len(envelope.Citations))
	for _, citation := range envelope.Citations {
		refs = append(refs, citation.ID)
	}
	return map[string]any{"id": envelope.ID, "version": envelope.Version, "contentHash": envelope.ContentHash, "citationRefs": refs, "budgetUsage": envelope.BudgetUsage}
}
