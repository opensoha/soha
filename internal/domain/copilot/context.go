package copilot

import (
	"time"

	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
)

type KnowledgeContextConfig struct {
	Enabled          bool     `json:"enabled"`
	KnowledgeBaseIDs []string `json:"knowledgeBaseIds,omitempty"`
	TopK             int      `json:"topK,omitempty"`
}

type ContextTask struct {
	Mode string `json:"mode,omitempty"`
	Goal string `json:"goal"`
}

type ContextVersionRef struct {
	ID      string `json:"id,omitempty"`
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
}

type ContextKnowledgeInput struct {
	Enabled          bool     `json:"enabled"`
	KnowledgeBaseIDs []string `json:"knowledgeBaseIds"`
	Query            string   `json:"query,omitempty"`
	TopK             int      `json:"topK,omitempty"`
}

type ContextToolRef struct {
	Name          string `json:"name"`
	SchemaVersion string `json:"schemaVersion,omitempty"`
}

type ContextEnvironment struct {
	Mode            string   `json:"mode,omitempty"`
	ObservationRefs []string `json:"observationRefs,omitempty"`
}

type ContextBudgets struct {
	MaxInputTokens    int `json:"maxInputTokens,omitempty"`
	MaxEvidenceTokens int `json:"maxEvidenceTokens,omitempty"`
	MaxSteps          int `json:"maxSteps,omitempty"`
}

type ContextBudgetUsage struct {
	EvidenceTokens int `json:"evidenceTokens"`
	EvidenceItems  int `json:"evidenceItems"`
}

type ContextBuildInput struct {
	RequestID   string                `json:"requestId,omitempty"`
	SessionID   string                `json:"sessionId,omitempty"`
	AgentRunID  string                `json:"agentRunId,omitempty"`
	Task        ContextTask           `json:"task"`
	Prompt      ContextVersionRef     `json:"prompt,omitempty"`
	Skills      []ContextVersionRef   `json:"skills,omitempty"`
	Session     ContextSession        `json:"session,omitempty"`
	Knowledge   ContextKnowledgeInput `json:"knowledge"`
	Tools       []ContextToolRef      `json:"tools,omitempty"`
	Environment ContextEnvironment    `json:"environment,omitempty"`
	Budgets     ContextBudgets        `json:"budgets,omitempty"`
}

type ContextSession struct {
	Summary           string   `json:"summary,omitempty"`
	RecentMessageRefs []string `json:"recentMessageRefs,omitempty"`
}

type ContextEvidence struct {
	CitationID string `json:"citationId"`
	Content    string `json:"content"`
	TokenCount int    `json:"tokenCount"`
}

type ContextPrincipal struct {
	UserID string `json:"userId"`
}

type ContextPolicySnapshot struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

type ContextEnvelope struct {
	Version        string                     `json:"version"`
	ID             string                     `json:"id"`
	RequestID      string                     `json:"requestId"`
	SessionID      string                     `json:"sessionId,omitempty"`
	AgentRunID     string                     `json:"agentRunId,omitempty"`
	Principal      ContextPrincipal           `json:"principal"`
	Task           ContextTask                `json:"task"`
	Prompt         ContextVersionRef          `json:"prompt,omitempty"`
	Skills         []ContextVersionRef        `json:"skills,omitempty"`
	Session        ContextSession             `json:"session,omitempty"`
	Evidence       []ContextEvidence          `json:"evidence,omitempty"`
	Citations      []domainknowledge.Citation `json:"citations,omitempty"`
	Tools          []ContextToolRef           `json:"tools,omitempty"`
	Environment    ContextEnvironment         `json:"environment,omitempty"`
	Budgets        ContextBudgets             `json:"budgets"`
	BudgetUsage    ContextBudgetUsage         `json:"budgetUsage"`
	PolicySnapshot ContextPolicySnapshot      `json:"policySnapshot"`
	ContentHash    string                     `json:"contentHash"`
	CreatedAt      time.Time                  `json:"createdAt"`
}

type ContextInspection struct {
	Envelope      ContextEnvelope `json:"envelope"`
	Sections      []string        `json:"sections"`
	Truncations   []string        `json:"truncations,omitempty"`
	RetrievalTime int64           `json:"retrievalTimeMs,omitempty"`
}
