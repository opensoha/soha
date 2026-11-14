package sohacli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
)

const (
	userAgent          = "soha/0.1"
	defaultHTTPTimeout = 30 * time.Second
)

type APIClient struct {
	ServerURL      string
	Token          string
	RefreshToken   string
	Client         *http.Client
	Timeout        time.Duration
	authState      *apiClientAuthState
	onRefresh      func(refreshResponse) error
	refreshSession func(context.Context, string) (refreshResponse, error)
}

type apiClientAuthState struct {
	mu           sync.Mutex
	AccessToken  string
	RefreshToken string
}

type httpStatusError struct {
	Method       string
	Path         string
	Status       string
	StatusCode   int
	Message      string
	Code         string
	RetryAfter   int
	AuthRejected bool
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("%s %s failed: %s: %s", e.Method, e.Path, e.Status, e.Message)
}

type loginResponse = sohaapi.AuthResultEnvelope

type refreshResponse = sohaapi.AuthResultEnvelope

type manifestResponse = sohaapi.AIGatewayManifestEnvelope

type invokeResponse = sohaapi.ToolInvocationResultEnvelope

type resourceReadResponse = sohaapi.ResourceReadResultEnvelope

type promptGetResponse = sohaapi.PromptGetResultEnvelope

type itemsResponse[T any] struct {
	Items []T `json:"items"`
}

type itemResponse[T any] struct {
	Data T `json:"data"`
}

type Manifest = sohaapi.AIGatewayManifest

type ToolCapability = sohaapi.ToolCapability

type ResourceCapability = sohaapi.ResourceCapability

type PromptCapability = sohaapi.PromptCapability

type SkillCapability = sohaapi.SkillCapability

type ToolInvocationResult = sohaapi.ToolInvocationResult

type ResourceReadResult = sohaapi.ResourceReadResult

type PromptMessage = sohaapi.PromptMessage

type PromptGetResult = sohaapi.PromptGetResult

type PersonalAccessToken = sohaapi.PersonalAccessToken

type CreatedPersonalAccessToken = sohaapi.CreatedPersonalAccessToken

type ServiceAccount = sohaapi.ServiceAccount

type ServiceAccountToken = sohaapi.ServiceAccountToken

type CreatedServiceAccountToken = sohaapi.CreatedServiceAccountToken

type SecretBinding = sohaapi.SecretBinding

type SecretCreateRequest = sohaapi.SecretCreateRequest

type SecretMetadata = sohaapi.SecretMetadata

type SecretRotateRequest = sohaapi.SecretRotateRequest

type SecretVaultKV2Reference = sohaapi.SecretVaultKV2Reference

type SecretUpdateRequest = sohaapi.SecretUpdateRequest

type SecretVersionMetadata = sohaapi.SecretVersionMetadata

type AuditLog = sohaapi.AuditLog

type ApprovalTrace = sohaapi.ApprovalTrace

type ApprovalDecisionTrace = sohaapi.ApprovalDecisionTrace

type ApprovalStageTrace = sohaapi.ApprovalStageTrace

type ApprovalTimelineEvent = sohaapi.ApprovalTimelineEvent

type ApprovalRequest = sohaapi.ApprovalRequest

type ApprovalDecisionResult = sohaapi.ApprovalDecisionResult

type ApprovalTimeline = sohaapi.ApprovalTimeline

type GovernanceStatus = sohaapi.GovernanceStatus

type GovernanceHealth = sohaapi.GovernanceHealth

type GovernanceHealthCheck = sohaapi.GovernanceHealthCheck

type GovernanceMetrics = sohaapi.GovernanceMetrics

type GovernanceMetricCount = sohaapi.GovernanceMetricCount

type GovernanceRedactionSummary = sohaapi.GovernanceRedactionSummary

type GovernanceTokenSummary = sohaapi.GovernanceTokenSummary

type GovernanceTokenCounts = sohaapi.GovernanceTokenCounts

type GovernanceTokenFinding = sohaapi.GovernanceTokenFinding

type GovernanceClientSummary = sohaapi.GovernanceClientSummary

type GovernanceApprovalSummary = sohaapi.GovernanceApprovalSummary

type GovernancePolicyCoverage = sohaapi.GovernancePolicyCoverage

type GovernanceFinding = sohaapi.GovernanceFinding

type GovernanceRecommendationAction = sohaapi.GovernanceRecommendationAction

type KnowledgeSearchFilters struct {
	DocumentIDs []string `json:"documentIds,omitempty"`
	SourceIDs   []string `json:"sourceIds,omitempty"`
}

type KnowledgeSearchRequest struct {
	Filters          *KnowledgeSearchFilters `json:"filters,omitempty"`
	KnowledgeBaseIDs []string                `json:"knowledgeBaseIds"`
	Query            string                  `json:"query"`
	TopK             int                     `json:"topK,omitempty"`
}

type KnowledgeSourceLocation struct {
	EndByte   int    `json:"endByte,omitempty"`
	StartByte int    `json:"startByte,omitempty"`
	URI       string `json:"uri,omitempty"`
}

type KnowledgeCitation struct {
	ChunkID         string                  `json:"chunkId"`
	ContentHash     string                  `json:"contentHash"`
	DocumentID      string                  `json:"documentId"`
	DocumentTitle   string                  `json:"documentTitle"`
	ID              string                  `json:"id"`
	KnowledgeBaseID string                  `json:"knowledgeBaseId"`
	Location        KnowledgeSourceLocation `json:"location"`
	Score           float32                 `json:"score"`
	URI             string                  `json:"uri,omitempty"`
}

type KnowledgeSearchHit struct {
	ChunkID         string            `json:"chunkId"`
	Citation        KnowledgeCitation `json:"citation"`
	Content         string            `json:"content"`
	DocumentID      string            `json:"documentId"`
	KnowledgeBaseID string            `json:"knowledgeBaseId"`
	LexicalScore    float32           `json:"lexicalScore"`
	Score           float32           `json:"score"`
	Title           string            `json:"title"`
	VectorScore     float32           `json:"vectorScore"`
}

type KnowledgeSearchResult struct {
	CandidateCount int                  `json:"candidateCount"`
	Citations      []KnowledgeCitation  `json:"citations"`
	Hits           []KnowledgeSearchHit `json:"hits"`
	NoAnswer       bool                 `json:"noAnswer"`
	Query          string               `json:"query"`
	TimingMs       int64                `json:"timingMs"`
	TraceID        string               `json:"traceId"`
}

type PluginManifest = sohaapi.PluginManifest

type MarketplacePlugin = sohaapi.MarketplacePlugin

type InstalledPlugin = sohaapi.InstalledPlugin

type PluginInstallRequest = sohaapi.PluginInstallRequest

type PluginConfigRequest = sohaapi.PluginConfigRequest

type ClusterCapabilityStatus string

type ClusterCapabilityRiskLevel string

type ClusterCapabilityModeSupport struct {
	Status ClusterCapabilityStatus `json:"status"`
	Reason string                  `json:"reason,omitempty"`
	Notes  []string                `json:"notes,omitempty"`
}

type ClusterCapabilityMatrixEntry struct {
	Key              string                       `json:"key"`
	Label            string                       `json:"label"`
	Category         string                       `json:"category"`
	RequiredScopes   []string                     `json:"requiredScopes,omitempty"`
	RiskLevel        ClusterCapabilityRiskLevel   `json:"riskLevel"`
	RequiresApproval bool                         `json:"requiresApproval"`
	DocsURL          string                       `json:"docsUrl,omitempty"`
	Direct           ClusterCapabilityModeSupport `json:"direct"`
	Agent            ClusterCapabilityModeSupport `json:"agent"`
}

type LogEntry = sohaapi.LogEntry

type LogPage = sohaapi.LogPage

type LogQuery = sohaapi.LogQuery

type LogSourceSelector = sohaapi.LogSourceSelector

type ComputeTaskDomain = sohaapi.ComputeTaskDomain

type ComputeTaskMutationRequest = sohaapi.ComputeTaskMutationRequest

type ComputeTaskStatus = sohaapi.ComputeTaskStatus

type ComputeTaskView = sohaapi.ComputeTaskView

type CloudFleetCapabilityDiagnosticsEnvelope struct {
	Diagnostics CloudFleetCapabilityDiagnostics `json:"diagnostics"`
}

type CloudFleetCapabilityDiagnostics struct {
	TenantID               string                              `json:"tenantId"`
	FleetID                string                              `json:"fleetId"`
	Mode                   string                              `json:"mode"`
	Status                 string                              `json:"status"`
	ClusterStatusCounts    CloudFleetClusterStatusCounts       `json:"clusterStatusCounts"`
	CapabilityStatusCounts CloudFleetCapabilityStatusCounts    `json:"capabilityStatusCounts"`
	CapabilityGaps         []CloudFleetCapabilityGap           `json:"capabilityGaps"`
	Clusters               []CloudFleetClusterCapabilityStatus `json:"clusters"`
	Message                string                              `json:"message"`
}

type CloudFleetClusterStatusCounts struct {
	Total     int `json:"total"`
	Available int `json:"available"`
	Degraded  int `json:"degraded"`
	Unknown   int `json:"unknown"`
}

type CloudFleetCapabilityStatusCounts struct {
	Available   int `json:"available"`
	Partial     int `json:"partial"`
	Unsupported int `json:"unsupported"`
}

type CloudFleetCapabilityGap struct {
	Key                   string                          `json:"key"`
	PartialClusterIDs     []string                        `json:"partialClusterIds"`
	UnsupportedClusterIDs []string                        `json:"unsupportedClusterIds"`
	MissingClusterIDs     []string                        `json:"missingClusterIds"`
	Reasons               []CloudFleetCapabilityGapReason `json:"reasons"`
}

type CloudFleetCapabilityGapReason struct {
	ClusterID string `json:"clusterId"`
	Status    string `json:"status"`
	Reason    string `json:"reason"`
}

type CloudFleetClusterCapabilityStatus struct {
	ClusterID            string                       `json:"clusterId"`
	ClusterName          string                       `json:"clusterName"`
	DisplayName          string                       `json:"displayName"`
	Status               string                       `json:"status"`
	CapabilitySetVersion string                       `json:"capabilitySetVersion,omitempty"`
	RecordedAt           string                       `json:"recordedAt,omitempty"`
	Counts               CloudFleetCapabilityCounts   `json:"counts"`
	DegradedCapabilities []CloudFleetCapabilityReason `json:"degradedCapabilities"`
	MissingCapabilities  []string                     `json:"missingCapabilities"`
}

type CloudFleetCapabilityCounts struct {
	Available   int `json:"available"`
	Partial     int `json:"partial"`
	Unsupported int `json:"unsupported"`
}

type CloudFleetCapabilityReason struct {
	Key    string `json:"key"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

func (c APIClient) Login(ctx context.Context, login, password string) (loginResponse, error) {
	var out loginResponse
	err := c.doJSON(ctx, http.MethodPost, "/api/v1/auth/login", "", nil, map[string]string{
		"login":    login,
		"password": password,
	}, &out)
	return out, err
}

func (c APIClient) Refresh(ctx context.Context, refreshToken string) (refreshResponse, error) {
	var out refreshResponse
	err := c.doJSON(ctx, http.MethodPost, "/api/v1/auth/refresh", "", nil, map[string]string{
		"refreshToken": strings.TrimSpace(refreshToken),
	}, &out)
	return out, err
}

func (c APIClient) Capabilities(ctx context.Context, headers map[string]string) (Manifest, error) {
	return c.SearchCapabilities(ctx, headers, nil)
}

func (c APIClient) SearchCapabilities(ctx context.Context, headers map[string]string, filters url.Values) (Manifest, error) {
	path := "/api/v1/ai-gateway/capabilities"
	if len(filters) > 0 {
		path += "?" + filters.Encode()
	}
	var out manifestResponse
	if err := c.doJSON(ctx, http.MethodGet, path, c.Token, headers, nil, &out); err != nil {
		return Manifest{}, err
	}
	return out.Data, nil
}

func (c APIClient) ClusterCapabilities(ctx context.Context, headers map[string]string) ([]ClusterCapabilityMatrixEntry, error) {
	var out itemsResponse[ClusterCapabilityMatrixEntry]
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/clusters/capabilities", c.Token, headers, nil, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (c APIClient) QueryClusterLogs(ctx context.Context, clusterID string, query LogQuery) (LogPage, error) {
	return c.queryLogs(ctx, "/api/v1/clusters/"+url.PathEscape(strings.TrimSpace(clusterID))+"/logs/query", query)
}

func (c APIClient) QueryDockerProjectLogs(ctx context.Context, projectID string, query LogQuery) (LogPage, error) {
	return c.queryLogs(ctx, "/api/v1/docker/projects/"+url.PathEscape(strings.TrimSpace(projectID))+"/logs/query", query)
}

func (c APIClient) QueryDeliveryEnvironmentLogs(ctx context.Context, applicationID, environmentID string, query LogQuery) (LogPage, error) {
	path := "/api/v1/delivery/applications/" + url.PathEscape(strings.TrimSpace(applicationID)) +
		"/environments/" + url.PathEscape(strings.TrimSpace(environmentID)) + "/logs/query"
	return c.queryLogs(ctx, path, query)
}

func (c APIClient) queryLogs(ctx context.Context, path string, query LogQuery) (LogPage, error) {
	var out itemResponse[LogPage]
	if err := c.doJSON(ctx, http.MethodPost, path, c.Token, nil, query, &out); err != nil {
		return LogPage{}, err
	}
	return out.Data, nil
}

func (c APIClient) GetComputeTask(ctx context.Context, domain ComputeTaskDomain, taskID string) (ComputeTaskView, error) {
	var out itemResponse[ComputeTaskView]
	path := computeTaskPath(domain, taskID)
	if err := c.doJSON(ctx, http.MethodGet, path, c.Token, nil, nil, &out); err != nil {
		return ComputeTaskView{}, err
	}
	return out.Data, nil
}

func (c APIClient) CancelComputeTask(ctx context.Context, domain ComputeTaskDomain, taskID string, input ComputeTaskMutationRequest) (ComputeTaskView, error) {
	return c.CancelComputeTaskWithKey(ctx, domain, taskID, computeIdempotencyKey(string(domain), taskID, "cancel", input), input)
}

func computeTaskPath(domain ComputeTaskDomain, taskID string) string {
	return "/api/v1/compute/tasks/" + url.PathEscape(string(domain)) + "/" + url.PathEscape(strings.TrimSpace(taskID))
}

func (c APIClient) CloudFleetDiagnostics(ctx context.Context, tenantID, fleetID string) (CloudFleetCapabilityDiagnostics, error) {
	var out CloudFleetCapabilityDiagnosticsEnvelope
	path := "/cloud/tenants/" + url.PathEscape(strings.TrimSpace(tenantID)) + "/agent-fleets/" + url.PathEscape(strings.TrimSpace(fleetID)) + "/diagnostics"
	if err := c.doJSON(ctx, http.MethodGet, path, c.Token, nil, nil, &out); err != nil {
		return CloudFleetCapabilityDiagnostics{}, err
	}
	return out.Diagnostics, nil
}

func (c APIClient) InvokeTool(ctx context.Context, toolName string, input map[string]any, headers map[string]string) (ToolInvocationResult, error) {
	return c.InvokeToolWithRequest(ctx, toolName, input, "", nil, headers)
}

func (c APIClient) InvokeToolWithRequestID(ctx context.Context, toolName string, input map[string]any, requestID string, headers map[string]string) (ToolInvocationResult, error) {
	return c.InvokeToolWithRequest(ctx, toolName, input, requestID, nil, headers)
}

func (c APIClient) InvokeToolWithRequest(ctx context.Context, toolName string, input map[string]any, requestID string, secretRefs map[string]string, headers map[string]string) (ToolInvocationResult, error) {
	return c.InvokeCapability(ctx, ToolCapability{Name: toolName}, input, requestID, secretRefs, headers)
}

func (c APIClient) InvokeCapability(ctx context.Context, tool ToolCapability, input map[string]any, requestID string, secretRefs map[string]string, headers map[string]string) (ToolInvocationResult, error) {
	var out invokeResponse
	path := "/api/v1/ai-gateway/tools/" + url.PathEscape(tool.Name) + "/invoke"
	payload := map[string]any{"input": emptyInput(input)}
	if tool.Version != "" {
		payload["capabilityVersion"] = tool.Version
	}
	if requestID = strings.TrimSpace(requestID); requestID != "" {
		payload["requestId"] = requestID
	}
	if len(secretRefs) > 0 {
		payload["secretRefs"] = secretRefs
	}
	if err := c.doJSON(ctx, http.MethodPost, path, c.Token, headers, payload, &out); err != nil {
		return ToolInvocationResult{}, err
	}
	return out.Data, nil
}

func (c APIClient) ListSecrets(ctx context.Context, scopeType, scopeID string) ([]SecretMetadata, error) {
	query := url.Values{}
	if scopeType = strings.TrimSpace(scopeType); scopeType != "" {
		query.Set("scopeType", scopeType)
	}
	if scopeID = strings.TrimSpace(scopeID); scopeID != "" {
		query.Set("scopeId", scopeID)
	}
	path := "/api/v1/secrets"
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	var out itemsResponse[SecretMetadata]
	if err := c.doJSON(ctx, http.MethodGet, path, c.Token, nil, nil, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (c APIClient) GetSecret(ctx context.Context, secretID string) (SecretMetadata, error) {
	var out itemResponse[SecretMetadata]
	if err := c.doJSON(ctx, http.MethodGet, secretPath(secretID), c.Token, nil, nil, &out); err != nil {
		return SecretMetadata{}, err
	}
	return out.Data, nil
}

func (c APIClient) CreateSecret(ctx context.Context, input SecretCreateRequest) (SecretMetadata, error) {
	var out itemResponse[SecretMetadata]
	if err := c.doJSON(ctx, http.MethodPost, "/api/v1/secrets", c.Token, nil, input, &out); err != nil {
		return SecretMetadata{}, err
	}
	return out.Data, nil
}

func (c APIClient) UpdateSecret(ctx context.Context, secretID string, input SecretUpdateRequest) (SecretMetadata, error) {
	var out itemResponse[SecretMetadata]
	if err := c.doJSON(ctx, http.MethodPatch, secretPath(secretID), c.Token, nil, input, &out); err != nil {
		return SecretMetadata{}, err
	}
	return out.Data, nil
}

func (c APIClient) DisableSecret(ctx context.Context, secretID string) error {
	return c.doJSON(ctx, http.MethodDelete, secretPath(secretID), c.Token, nil, nil, nil)
}

func (c APIClient) ListSecretVersions(ctx context.Context, secretID string) ([]SecretVersionMetadata, error) {
	var out itemsResponse[SecretVersionMetadata]
	if err := c.doJSON(ctx, http.MethodGet, secretPath(secretID)+"/versions", c.Token, nil, nil, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (c APIClient) RotateSecret(ctx context.Context, secretID string, input SecretRotateRequest) (SecretVersionMetadata, error) {
	var out itemResponse[SecretVersionMetadata]
	if err := c.doJSON(ctx, http.MethodPost, secretPath(secretID)+"/versions", c.Token, nil, input, &out); err != nil {
		return SecretVersionMetadata{}, err
	}
	return out.Data, nil
}

func (c APIClient) RevokeSecretVersion(ctx context.Context, secretID string, version int) (SecretVersionMetadata, error) {
	var out itemResponse[SecretVersionMetadata]
	path := secretPath(secretID) + "/versions/" + strconv.Itoa(version) + "/revoke"
	if err := c.doJSON(ctx, http.MethodPost, path, c.Token, nil, nil, &out); err != nil {
		return SecretVersionMetadata{}, err
	}
	return out.Data, nil
}

func secretPath(secretID string) string {
	return "/api/v1/secrets/" + url.PathEscape(strings.TrimSpace(secretID))
}

func (c APIClient) ReadResource(ctx context.Context, uri string, contextValues map[string]any, headers map[string]string) (ResourceReadResult, error) {
	var out resourceReadResponse
	payload := map[string]any{"uri": strings.TrimSpace(uri), "context": emptyInput(contextValues)}
	if err := c.doJSON(ctx, http.MethodPost, "/api/v1/ai-gateway/resources/read", c.Token, headers, payload, &out); err != nil {
		return ResourceReadResult{}, err
	}
	return out.Data, nil
}

func (c APIClient) GetPrompt(ctx context.Context, name string, arguments map[string]any, contextValues map[string]any, headers map[string]string) (PromptGetResult, error) {
	var out promptGetResponse
	payload := map[string]any{"name": strings.TrimSpace(name), "arguments": emptyInput(arguments), "context": emptyInput(contextValues)}
	if err := c.doJSON(ctx, http.MethodPost, "/api/v1/ai-gateway/prompts/get", c.Token, headers, payload, &out); err != nil {
		return PromptGetResult{}, err
	}
	return out.Data, nil
}

func (c APIClient) ListPersonalAccessTokens(ctx context.Context, headers map[string]string) ([]PersonalAccessToken, error) {
	var out itemsResponse[PersonalAccessToken]
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/ai-gateway/personal-access-tokens", c.Token, headers, nil, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (c APIClient) CreatePersonalAccessToken(ctx context.Context, input map[string]any, headers map[string]string) (CreatedPersonalAccessToken, error) {
	metadata, _ := input["metadata"].(map[string]any)
	binding, err := governedBinding(metadata)
	if err != nil {
		return CreatedPersonalAccessToken{}, err
	}
	path, err := c.governedCredentialPath(ctx, "/api/v1/ai-gateway/personal-access-tokens", binding)
	if err != nil {
		return CreatedPersonalAccessToken{}, err
	}
	var out itemResponse[CreatedPersonalAccessToken]
	if err := c.doJSON(ctx, http.MethodPost, path, c.Token, headers, input, &out); err != nil {
		return CreatedPersonalAccessToken{}, err
	}
	if err := c.verifyGovernedCredential(ctx, out.Data.Token.ID, out.Data.Value, out.Data.Token.TokenPrefix, out.Data.Token.Metadata, binding, false); err != nil {
		return CreatedPersonalAccessToken{}, err
	}
	return out.Data, nil
}

func (c APIClient) RevokePersonalAccessToken(ctx context.Context, tokenID string, headers map[string]string) error {
	path := "/api/v1/ai-gateway/personal-access-tokens/" + url.PathEscape(tokenID) + "/revoke"
	return c.doJSON(ctx, http.MethodPost, path, c.Token, headers, nil, nil)
}

func (c APIClient) ListServiceAccounts(ctx context.Context, headers map[string]string) ([]ServiceAccount, error) {
	var out itemsResponse[ServiceAccount]
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/ai-gateway/service-accounts", c.Token, headers, nil, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (c APIClient) ListServiceAccountTokens(ctx context.Context, headers map[string]string) ([]ServiceAccountToken, error) {
	var out itemsResponse[ServiceAccountToken]
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/ai-gateway/service-account-tokens", c.Token, headers, nil, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (c APIClient) CreateServiceAccount(ctx context.Context, input map[string]any, headers map[string]string) (ServiceAccount, error) {
	var out itemResponse[ServiceAccount]
	if err := c.doJSON(ctx, http.MethodPost, "/api/v1/ai-gateway/service-accounts", c.Token, headers, input, &out); err != nil {
		return ServiceAccount{}, err
	}
	return out.Data, nil
}

func (c APIClient) CreateServiceAccountToken(ctx context.Context, serviceAccountID string, input map[string]any, headers map[string]string) (CreatedServiceAccountToken, error) {
	metadata, _ := input["metadata"].(map[string]any)
	binding, err := governedBinding(metadata)
	if err != nil {
		return CreatedServiceAccountToken{}, err
	}
	path, err := c.governedCredentialPath(ctx, "/api/v1/ai-gateway/service-accounts/"+url.PathEscape(serviceAccountID)+"/tokens", binding)
	if err != nil {
		return CreatedServiceAccountToken{}, err
	}
	var out itemResponse[CreatedServiceAccountToken]
	if err := c.doJSON(ctx, http.MethodPost, path, c.Token, headers, input, &out); err != nil {
		return CreatedServiceAccountToken{}, err
	}
	if err := c.verifyGovernedCredential(ctx, out.Data.Token.ID, out.Data.Value, out.Data.Token.TokenPrefix, out.Data.Token.Metadata, binding, true); err != nil {
		return CreatedServiceAccountToken{}, err
	}
	return out.Data, nil
}

func (c APIClient) RevokeServiceAccountToken(ctx context.Context, tokenID string, headers map[string]string) error {
	path := "/api/v1/ai-gateway/service-account-tokens/" + url.PathEscape(tokenID) + "/revoke"
	return c.doJSON(ctx, http.MethodPost, path, c.Token, headers, nil, nil)
}

func (c APIClient) ListAuditLogs(ctx context.Context, query url.Values, headers map[string]string) ([]AuditLog, error) {
	var out itemsResponse[AuditLog]
	path := "/api/v1/ai-gateway/audit-logs"
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	if err := c.doJSON(ctx, http.MethodGet, path, c.Token, headers, nil, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (c APIClient) ListApprovalRequests(ctx context.Context, query url.Values, headers map[string]string) ([]ApprovalRequest, error) {
	var out itemsResponse[ApprovalRequest]
	path := "/api/v1/ai-gateway/approval-requests"
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	if err := c.doJSON(ctx, http.MethodGet, path, c.Token, headers, nil, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (c APIClient) GetApprovalTimeline(ctx context.Context, requestID string, headers map[string]string) (ApprovalTimeline, error) {
	var out itemResponse[ApprovalTimeline]
	path := "/api/v1/ai-gateway/approval-requests/" + url.PathEscape(strings.TrimSpace(requestID)) + "/timeline"
	if err := c.doJSON(ctx, http.MethodGet, path, c.Token, headers, nil, &out); err != nil {
		return ApprovalTimeline{}, err
	}
	return out.Data, nil
}

func (c APIClient) DecideApprovalRequest(ctx context.Context, requestID, action, comment string, headers map[string]string) (ApprovalDecisionResult, error) {
	action = strings.TrimSpace(action)
	switch action {
	case "approve", "reject", "cancel":
	default:
		return ApprovalDecisionResult{}, fmt.Errorf("unsupported approval action %q", action)
	}
	var out itemResponse[ApprovalDecisionResult]
	path := "/api/v1/ai-gateway/approval-requests/" + url.PathEscape(strings.TrimSpace(requestID)) + "/" + action
	payload := map[string]any{}
	if strings.TrimSpace(comment) != "" {
		payload["comment"] = strings.TrimSpace(comment)
	}
	if err := c.doJSON(ctx, http.MethodPost, path, c.Token, headers, payload, &out); err != nil {
		return ApprovalDecisionResult{}, err
	}
	return out.Data, nil
}

func (c APIClient) GovernanceStatus(ctx context.Context, windowHours int, headers map[string]string) (GovernanceStatus, error) {
	var out itemResponse[GovernanceStatus]
	path := "/api/v1/ai-gateway/governance/status"
	if windowHours > 0 {
		query := url.Values{}
		query.Set("windowHours", fmt.Sprint(windowHours))
		path += "?" + query.Encode()
	}
	if err := c.doJSON(ctx, http.MethodGet, path, c.Token, headers, nil, &out); err != nil {
		return GovernanceStatus{}, err
	}
	return out.Data, nil
}

func (c APIClient) SearchKnowledge(ctx context.Context, input KnowledgeSearchRequest, headers map[string]string) (KnowledgeSearchResult, error) {
	var out itemResponse[KnowledgeSearchResult]
	if err := c.doJSON(ctx, http.MethodPost, "/api/v1/ai/knowledge/search", c.Token, headers, input, &out); err != nil {
		return KnowledgeSearchResult{}, err
	}
	return out.Data, nil
}

func (c APIClient) ListMarketplacePlugins(ctx context.Context, query url.Values, headers map[string]string) ([]MarketplacePlugin, error) {
	var out itemsResponse[MarketplacePlugin]
	path := "/api/v1/plugins/marketplace"
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	if err := c.doJSON(ctx, http.MethodGet, path, c.Token, headers, nil, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (c APIClient) GetMarketplacePlugin(ctx context.Context, pluginID string, query url.Values, headers map[string]string) (MarketplacePlugin, error) {
	var out itemResponse[MarketplacePlugin]
	path := "/api/v1/plugins/marketplace/" + url.PathEscape(strings.TrimSpace(pluginID))
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	if err := c.doJSON(ctx, http.MethodGet, path, c.Token, headers, nil, &out); err != nil {
		return MarketplacePlugin{}, err
	}
	return out.Data, nil
}

func (c APIClient) ListInstalledPlugins(ctx context.Context, headers map[string]string) ([]InstalledPlugin, error) {
	var out itemsResponse[InstalledPlugin]
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/plugins/installed", c.Token, headers, nil, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (c APIClient) GetInstalledPlugin(ctx context.Context, pluginID string, headers map[string]string) (InstalledPlugin, error) {
	var out itemResponse[InstalledPlugin]
	path := "/api/v1/plugins/" + url.PathEscape(strings.TrimSpace(pluginID))
	if err := c.doJSON(ctx, http.MethodGet, path, c.Token, headers, nil, &out); err != nil {
		return InstalledPlugin{}, err
	}
	return out.Data, nil
}

func (c APIClient) GetInstalledPluginManifest(ctx context.Context, pluginID string, headers map[string]string) (PluginManifest, error) {
	var out itemResponse[PluginManifest]
	path := "/api/v1/plugins/" + url.PathEscape(strings.TrimSpace(pluginID)) + "/manifest"
	if err := c.doJSON(ctx, http.MethodGet, path, c.Token, headers, nil, &out); err != nil {
		return PluginManifest{}, err
	}
	return out.Data, nil
}

func (c APIClient) InstallPlugin(ctx context.Context, input PluginInstallRequest, headers map[string]string) (InstalledPlugin, error) {
	var out itemResponse[InstalledPlugin]
	if err := c.doJSON(ctx, http.MethodPost, "/api/v1/plugins/install", c.Token, headers, input, &out); err != nil {
		return InstalledPlugin{}, err
	}
	return out.Data, nil
}

func (c APIClient) EnablePlugin(ctx context.Context, pluginID string, headers map[string]string) (InstalledPlugin, error) {
	return c.pluginAction(ctx, pluginID, "enable", headers)
}

func (c APIClient) DisablePlugin(ctx context.Context, pluginID string, headers map[string]string) (InstalledPlugin, error) {
	return c.pluginAction(ctx, pluginID, "disable", headers)
}

func (c APIClient) UpgradePlugin(ctx context.Context, pluginID string, input PluginInstallRequest, headers map[string]string) (InstalledPlugin, error) {
	var out itemResponse[InstalledPlugin]
	path := "/api/v1/plugins/" + url.PathEscape(strings.TrimSpace(pluginID)) + "/upgrade"
	if err := c.doJSON(ctx, http.MethodPost, path, c.Token, headers, input, &out); err != nil {
		return InstalledPlugin{}, err
	}
	return out.Data, nil
}

func (c APIClient) ConfigurePlugin(ctx context.Context, pluginID string, input PluginConfigRequest, headers map[string]string) (InstalledPlugin, error) {
	var out itemResponse[InstalledPlugin]
	path := "/api/v1/plugins/" + url.PathEscape(strings.TrimSpace(pluginID)) + "/config"
	if err := c.doJSON(ctx, http.MethodPut, path, c.Token, headers, input, &out); err != nil {
		return InstalledPlugin{}, err
	}
	return out.Data, nil
}

func (c APIClient) RemovePlugin(ctx context.Context, pluginID string, headers map[string]string) error {
	path := "/api/v1/plugins/" + url.PathEscape(strings.TrimSpace(pluginID))
	return c.doJSON(ctx, http.MethodDelete, path, c.Token, headers, nil, nil)
}

func (c APIClient) pluginAction(ctx context.Context, pluginID, action string, headers map[string]string) (InstalledPlugin, error) {
	var out itemResponse[InstalledPlugin]
	path := "/api/v1/plugins/" + url.PathEscape(strings.TrimSpace(pluginID)) + "/" + action
	if err := c.doJSON(ctx, http.MethodPost, path, c.Token, headers, nil, &out); err != nil {
		return InstalledPlugin{}, err
	}
	return out.Data, nil
}

func (c APIClient) doJSON(ctx context.Context, method, path, token string, headers map[string]string, body any, out any) error {
	base := strings.TrimRight(strings.TrimSpace(c.ServerURL), "/")
	if base == "" {
		return fmt.Errorf("server URL is required")
	}
	if !sameServerURL(base, base) {
		return fmt.Errorf("invalid_server_url: API URL must not contain credentials, query or fragment")
	}
	var rawBody []byte
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rawBody = raw
	}
	token = c.effectiveToken(token)
	raw, err := c.doRawJSON(ctx, method, path, token, headers, rawBody)
	if err != nil {
		var statusErr *httpStatusError
		if errors.As(err, &statusErr) && statusErr.StatusCode == http.StatusUnauthorized && token != "" && c.canRefresh() && (method == http.MethodGet || method == http.MethodHead || statusErr.AuthRejected) {
			refreshedToken, refreshErr := c.refreshAfterUnauthorized(ctx, token)
			if refreshErr != nil {
				return refreshErr
			}
			raw, err = c.doRawJSON(ctx, method, path, refreshedToken, headers, rawBody)
		}
		if err != nil {
			return err
		}
	}
	if out == nil {
		return nil
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return fmt.Errorf("%s %s returned empty response", method, path)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func (c APIClient) doRawJSON(ctx context.Context, method, path, token string, headers map[string]string, rawBody []byte) ([]byte, error) {
	var reader io.Reader
	if rawBody != nil {
		reader = bytes.NewReader(rawBody)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(strings.TrimSpace(c.ServerURL), "/")+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	if rawBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for key, value := range headers {
		value = strings.TrimSpace(value)
		if value != "" {
			req.Header.Set(key, value)
		}
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s request failed: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &httpStatusError{
			Method:       method,
			Path:         path,
			Status:       resp.Status,
			StatusCode:   resp.StatusCode,
			Message:      safeAPIErrorMessage(resp.Status, raw, rawBody, token, c.RefreshToken),
			Code:         responseErrorCode(raw),
			RetryAfter:   retryAfterSeconds(resp.Header.Get("Retry-After")),
			AuthRejected: strings.Contains(resp.Header.Get("WWW-Authenticate"), `error="invalid_token"`),
		}
	}
	return raw, nil
}

func (c APIClient) effectiveToken(token string) string {
	if c.authState != nil && token == c.Token {
		c.authState.mu.Lock()
		defer c.authState.mu.Unlock()
		return strings.TrimSpace(c.authState.AccessToken)
	}
	return strings.TrimSpace(token)
}

func (c APIClient) canRefresh() bool {
	if c.refreshSession != nil {
		return true
	}
	if c.authState != nil {
		c.authState.mu.Lock()
		defer c.authState.mu.Unlock()
		return c.authState.RefreshToken != ""
	}
	return strings.TrimSpace(c.RefreshToken) != ""
}
func (c APIClient) refreshAfterUnauthorized(ctx context.Context, previous ...string) (string, error) {
	sent := c.Token
	if len(previous) > 0 {
		sent = previous[0]
	}
	refreshToken := c.RefreshToken
	if c.authState != nil {
		c.authState.mu.Lock()
		defer c.authState.mu.Unlock()
		if c.authState.AccessToken != "" && c.authState.AccessToken != sent {
			return c.authState.AccessToken, nil
		}
		refreshToken = firstNonEmptyString(c.authState.RefreshToken, refreshToken)
	}
	var result refreshResponse
	var err error
	if c.refreshSession != nil {
		result, err = c.refreshSession(ctx, sent)
	} else {
		if refreshToken == "" {
			return "", fmt.Errorf("refresh token is not configured")
		}
		result, err = (APIClient{ServerURL: c.ServerURL, Client: c.Client, Timeout: c.Timeout}).Refresh(ctx, refreshToken)
	}
	if err != nil {
		return "", refreshFailure(err)
	}
	token := strings.TrimSpace(result.Data.Tokens.AccessToken)
	if token == "" {
		return "", fmt.Errorf("auth response did not include an access token")
	}
	if c.onRefresh != nil {
		if err = c.onRefresh(result); err != nil {
			return "", err
		}
	}
	if c.authState != nil {
		c.authState.AccessToken = token
		c.authState.RefreshToken = firstNonEmptyString(result.Data.Tokens.RefreshToken, refreshToken)
	}
	return token, nil
}
func responseErrorCode(raw []byte) string {
	var value struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
		Code string `json:"code"`
	}
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	return firstNonEmptyString(value.Error.Code, value.Code)
}
func retryAfterSeconds(raw string) int {
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0
	}
	return value
}

func (c APIClient) httpClient() *http.Client {
	client := http.Client{}
	if c.Client != nil {
		client = *c.Client
	}
	if c.Timeout > 0 {
		client.Timeout = c.Timeout
	} else if client.Timeout <= 0 {
		client.Timeout = defaultHTTPTimeout
	}
	base, err := url.Parse(normalizeServerURL(c.ServerURL))
	if err != nil || base.Hostname() == "" {
		return &client
	}
	original := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		fromOrigin := (&url.URL{Scheme: base.Scheme, Host: base.Host}).String()
		toOrigin := (&url.URL{Scheme: req.URL.Scheme, Host: req.URL.Host}).String()
		prefix := strings.TrimRight(base.Path, "/")
		if !sameServerURL(fromOrigin, toOrigin) || prefix != "" && req.URL.Path != prefix && !strings.HasPrefix(req.URL.Path, prefix+"/") {
			return fmt.Errorf("server_mismatch: API redirect left the configured server or base path")
		}
		if original != nil {
			return original(req, via)
		}
		if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		return nil
	}
	return &client
}

func responseErrorMessage(status string, raw []byte) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return status
	}
	var wrapped struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &wrapped) == nil && wrapped.Error.Message != "" {
		if wrapped.Error.Code != "" {
			return wrapped.Error.Code + ": " + wrapped.Error.Message
		}
		return wrapped.Error.Message
	}
	var plain struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &plain) == nil && plain.Message != "" {
		if plain.Code != "" {
			return plain.Code + ": " + plain.Message
		}
		return plain.Message
	}
	return strings.TrimSpace(string(raw))
}

func emptyInput(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	return input
}
