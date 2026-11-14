package sohacli

import (
	"context"
	"net/http"
	"net/url"
	"strings"
)

type KnowledgeConnectorInput struct {
	KnowledgeBaseID string         `json:"knowledgeBaseId"`
	Name            string         `json:"name"`
	Kind            string         `json:"kind"`
	Version         string         `json:"version,omitempty"`
	SecretRef       string         `json:"secretRef"`
	Config          map[string]any `json:"config"`
	SyncPolicy      struct {
		Mode string `json:"mode"`
	} `json:"syncPolicy"`
}

type KnowledgeSyncInput struct {
	SourceID       string `json:"sourceId"`
	TargetRevision string `json:"targetRevision,omitempty"`
}

type KnowledgeRebuildInput struct {
	Reason string `json:"reason,omitempty"`
}

type EvaluationExecuteInput struct {
	ExecutorProfileID string `json:"executorProfileId,omitempty"`
}

type EvaluationReplayInput struct {
	ID                string `json:"id"`
	BaselineRunID     string `json:"baselineRunId"`
	CandidateRunID    string `json:"candidateRunId"`
	ExecutorProfileID string `json:"executorProfileId"`
}

type EvaluationGateInput struct {
	PolicyID       string `json:"policyId"`
	BaselineRunID  string `json:"baselineRunId"`
	CandidateRunID string `json:"candidateRunId"`
}

func (c APIClient) ListKnowledgeConnectors(ctx context.Context, headers map[string]string) ([]map[string]any, error) {
	var out itemsResponse[map[string]any]
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/ai/knowledge/connectors", c.Token, headers, nil, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (c APIClient) CreateKnowledgeConnector(ctx context.Context, input KnowledgeConnectorInput, headers map[string]string) (map[string]any, error) {
	return c.postAIItem(ctx, "/api/v1/ai/knowledge/connectors", input, headers)
}

func (c APIClient) ValidateKnowledgeConnector(ctx context.Context, connectorID string, headers map[string]string) (map[string]any, error) {
	path := "/api/v1/ai/knowledge/connectors/" + url.PathEscape(strings.TrimSpace(connectorID)) + "/validate"
	return c.postAIItem(ctx, path, nil, headers)
}

func (c APIClient) StartKnowledgeSync(ctx context.Context, baseID string, input KnowledgeSyncInput, headers map[string]string) (map[string]any, error) {
	path := "/api/v1/ai/knowledge-bases/" + url.PathEscape(strings.TrimSpace(baseID)) + "/sync-jobs"
	return c.postAIItem(ctx, path, input, headers)
}

func (c APIClient) GetKnowledgeSync(ctx context.Context, jobID string, headers map[string]string) (map[string]any, error) {
	var out itemResponse[map[string]any]
	path := "/api/v1/ai/knowledge/sync-jobs/" + url.PathEscape(strings.TrimSpace(jobID))
	if err := c.doJSON(ctx, http.MethodGet, path, c.Token, headers, nil, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

func (c APIClient) ActOnKnowledgeSync(ctx context.Context, jobID, action string, headers map[string]string) (map[string]any, error) {
	path := "/api/v1/ai/knowledge/sync-jobs/" + url.PathEscape(strings.TrimSpace(jobID)) + "/" + url.PathEscape(action)
	return c.postAIItem(ctx, path, nil, headers)
}

func (c APIClient) RebuildKnowledgeBase(ctx context.Context, baseID string, input KnowledgeRebuildInput, headers map[string]string) (map[string]any, error) {
	path := "/api/v1/ai/knowledge-bases/" + url.PathEscape(strings.TrimSpace(baseID)) + "/rebuild"
	return c.postAIItem(ctx, path, input, headers)
}

func (c APIClient) ExecuteEvaluationRun(ctx context.Context, runID string, input EvaluationExecuteInput, headers map[string]string) (map[string]any, error) {
	path := "/api/v1/ai/evaluations/runs/" + url.PathEscape(strings.TrimSpace(runID)) + "/execute"
	return c.postAIItem(ctx, path, input, headers)
}

func (c APIClient) CreateEvaluationReplay(ctx context.Context, input EvaluationReplayInput, headers map[string]string) (map[string]any, error) {
	return c.postAIItem(ctx, "/api/v1/ai/evaluations/replays", input, headers)
}

func (c APIClient) EvaluateReleaseGate(ctx context.Context, input EvaluationGateInput, headers map[string]string) (map[string]any, error) {
	return c.postAIItem(ctx, "/api/v1/ai/evaluations/gates/evaluate", input, headers)
}

func (c APIClient) InspectMemory(ctx context.Context, query url.Values, headers map[string]string) ([]map[string]any, error) {
	var out itemsResponse[map[string]any]
	path := "/api/v1/ai/memory"
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	if err := c.doJSON(ctx, http.MethodGet, path, c.Token, headers, nil, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (c APIClient) DeleteMemory(ctx context.Context, memoryID string, headers map[string]string) error {
	path := "/api/v1/ai/memory/" + url.PathEscape(strings.TrimSpace(memoryID))
	return c.doJSON(ctx, http.MethodDelete, path, c.Token, headers, nil, nil)
}

func (c APIClient) postAIItem(ctx context.Context, path string, input any, headers map[string]string) (map[string]any, error) {
	var out itemResponse[map[string]any]
	if err := c.doJSON(ctx, http.MethodPost, path, c.Token, headers, input, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}
