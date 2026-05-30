package sohacli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const userAgent = "soha-cli/0.1"

type APIClient struct {
	ServerURL string
	Token     string
	Client    *http.Client
}

type loginResponse struct {
	Data struct {
		User struct {
			UserID   string   `json:"userId"`
			UserName string   `json:"userName"`
			Roles    []string `json:"roles"`
		} `json:"user"`
		Tokens struct {
			AccessToken  string    `json:"accessToken"`
			RefreshToken string    `json:"refreshToken"`
			ExpiresAt    time.Time `json:"expiresAt"`
		} `json:"tokens"`
	} `json:"data"`
}

type manifestResponse struct {
	Data Manifest `json:"data"`
}

type invokeResponse struct {
	Data ToolInvocationResult `json:"data"`
}

type resourceReadResponse struct {
	Data ResourceReadResult `json:"data"`
}

type promptGetResponse struct {
	Data PromptGetResult `json:"data"`
}

type itemsResponse[T any] struct {
	Items []T `json:"items"`
}

type itemResponse[T any] struct {
	Data T `json:"data"`
}

type Manifest struct {
	Name           string               `json:"name"`
	Version        string               `json:"version"`
	PermissionKeys []string             `json:"permissionKeys"`
	Tools          []ToolCapability     `json:"tools"`
	Resources      []ResourceCapability `json:"resources,omitempty"`
	Prompts        []PromptCapability   `json:"prompts,omitempty"`
	Skills         []SkillCapability    `json:"skills,omitempty"`
	Summary        map[string]any       `json:"summary,omitempty"`
}

type ToolCapability struct {
	Name             string         `json:"name"`
	Title            string         `json:"title"`
	Description      string         `json:"description"`
	Domain           string         `json:"domain"`
	Action           string         `json:"action"`
	RiskLevel        string         `json:"riskLevel"`
	PermissionKeys   []string       `json:"permissionKeys"`
	RequiredScopes   []string       `json:"requiredScopes,omitempty"`
	MCPAdapterID     string         `json:"mcpAdapterId,omitempty"`
	MCPToolName      string         `json:"mcpToolName,omitempty"`
	RequiresApproval bool           `json:"requiresApproval"`
	InputSchema      map[string]any `json:"inputSchema,omitempty"`
	OutputSchema     map[string]any `json:"outputSchema,omitempty"`
}

type ResourceCapability struct {
	Name           string         `json:"name"`
	Description    string         `json:"description"`
	PermissionKeys []string       `json:"permissionKeys,omitempty"`
	RequiredScopes []string       `json:"requiredScopes,omitempty"`
	ContextSchema  map[string]any `json:"contextSchema,omitempty"`
}

type PromptCapability struct {
	Name           string         `json:"name"`
	Description    string         `json:"description"`
	PermissionKeys []string       `json:"permissionKeys,omitempty"`
	RequiredScopes []string       `json:"requiredScopes,omitempty"`
	ArgumentSchema map[string]any `json:"argumentSchema,omitempty"`
	ContextSchema  map[string]any `json:"contextSchema,omitempty"`
}

type SkillCapability struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Category       string   `json:"category"`
	Description    string   `json:"description"`
	CapabilityRefs []string `json:"capabilityRefs,omitempty"`
	PermissionKeys []string `json:"permissionKeys,omitempty"`
	RequiredScopes []string `json:"requiredScopes,omitempty"`
}

type ToolInvocationResult struct {
	ToolName         string         `json:"toolName"`
	RiskLevel        string         `json:"riskLevel"`
	RequiresApproval bool           `json:"requiresApproval"`
	Result           string         `json:"result"`
	Output           any            `json:"output,omitempty"`
	RelatedIDs       map[string]any `json:"relatedIds,omitempty"`
	Audit            map[string]any `json:"audit,omitempty"`
}

type ResourceReadResult struct {
	Name       string         `json:"name"`
	URI        string         `json:"uri"`
	MIMEType   string         `json:"mimeType"`
	Text       string         `json:"text,omitempty"`
	Data       any            `json:"data,omitempty"`
	RelatedIDs map[string]any `json:"relatedIds,omitempty"`
	Audit      map[string]any `json:"audit,omitempty"`
}

type PromptMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type PromptGetResult struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Messages    []PromptMessage `json:"messages"`
	RelatedIDs  map[string]any  `json:"relatedIds,omitempty"`
	Audit       map[string]any  `json:"audit,omitempty"`
}

type PersonalAccessToken struct {
	ID             string         `json:"id"`
	UserID         string         `json:"userId"`
	Name           string         `json:"name"`
	TokenPrefix    string         `json:"tokenPrefix"`
	Scopes         []string       `json:"scopes"`
	PermissionKeys []string       `json:"permissionKeys"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	ExpiresAt      *time.Time     `json:"expiresAt,omitempty"`
	LastUsedAt     *time.Time     `json:"lastUsedAt,omitempty"`
	RevokedAt      *time.Time     `json:"revokedAt,omitempty"`
	CreatedBy      string         `json:"createdBy"`
	CreatedAt      time.Time      `json:"createdAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
}

type CreatedPersonalAccessToken struct {
	Token PersonalAccessToken `json:"token"`
	Value string              `json:"value"`
}

type ServiceAccount struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Description   string         `json:"description,omitempty"`
	Status        string         `json:"status"`
	OwnerUserID   string         `json:"ownerUserId,omitempty"`
	RoleIDs       []string       `json:"roleIds"`
	TeamIDs       []string       `json:"teamIds"`
	ScopeGrantIDs []string       `json:"scopeGrantIds"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	CreatedBy     string         `json:"createdBy"`
	CreatedAt     time.Time      `json:"createdAt"`
	UpdatedAt     time.Time      `json:"updatedAt"`
}

type ServiceAccountToken struct {
	ID               string         `json:"id"`
	ServiceAccountID string         `json:"serviceAccountId"`
	Name             string         `json:"name"`
	TokenPrefix      string         `json:"tokenPrefix"`
	Scopes           []string       `json:"scopes"`
	PermissionKeys   []string       `json:"permissionKeys"`
	Metadata         map[string]any `json:"metadata,omitempty"`
	ExpiresAt        *time.Time     `json:"expiresAt,omitempty"`
	LastUsedAt       *time.Time     `json:"lastUsedAt,omitempty"`
	RevokedAt        *time.Time     `json:"revokedAt,omitempty"`
	CreatedBy        string         `json:"createdBy"`
	CreatedAt        time.Time      `json:"createdAt"`
	UpdatedAt        time.Time      `json:"updatedAt"`
}

type CreatedServiceAccountToken struct {
	Token ServiceAccountToken `json:"token"`
	Value string              `json:"value"`
}

type AuditLog struct {
	ID            string         `json:"id"`
	ActorType     string         `json:"actorType"`
	ActorID       string         `json:"actorId"`
	ActorName     string         `json:"actorName,omitempty"`
	AIClientID    string         `json:"aiClientId,omitempty"`
	AIClientName  string         `json:"aiClientName,omitempty"`
	SkillID       string         `json:"skillId,omitempty"`
	ToolName      string         `json:"toolName,omitempty"`
	RiskLevel     string         `json:"riskLevel,omitempty"`
	ResourceScope map[string]any `json:"resourceScope,omitempty"`
	Action        string         `json:"action"`
	Result        string         `json:"result"`
	Summary       string         `json:"summary"`
	RequestID     string         `json:"requestId,omitempty"`
	SourceIP      string         `json:"sourceIp,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	CreatedAt     time.Time      `json:"createdAt"`
}

type ApprovalTrace struct {
	ApprovalMode           string                  `json:"approvalMode,omitempty"`
	CurrentStageIndex      *int                    `json:"currentStageIndex,omitempty"`
	CurrentStageName       string                  `json:"currentStageName,omitempty"`
	StageCount             int                     `json:"stageCount,omitempty"`
	ApprovedCount          int                     `json:"approvedCount,omitempty"`
	RequiredApprovals      int                     `json:"requiredApprovals,omitempty"`
	PendingRequirements    []string                `json:"pendingRequirements,omitempty"`
	SatisfiedRequirements  []string                `json:"satisfiedRequirements,omitempty"`
	RoleApprovedCounts     map[string]int          `json:"roleApprovedCounts,omitempty"`
	TeamApprovedCounts     map[string]int          `json:"teamApprovedCounts,omitempty"`
	CandidateUserIDs       []string                `json:"candidateUserIds,omitempty"`
	CandidateRoles         []string                `json:"candidateRoles,omitempty"`
	CandidateTeams         []string                `json:"candidateTeams,omitempty"`
	OnCallCandidateUserIDs []string                `json:"onCallCandidateUserIds,omitempty"`
	WorkflowRunID          string                  `json:"workflowRunId,omitempty"`
	ExecutionTaskID        string                  `json:"executionTaskId,omitempty"`
	ReleaseBundleID        string                  `json:"releaseBundleId,omitempty"`
	Decisions              []ApprovalDecisionTrace `json:"decisions,omitempty"`
	StageHistory           []ApprovalStageTrace    `json:"stageHistory,omitempty"`
}

type ApprovalDecisionTrace struct {
	UserID     string     `json:"userId,omitempty"`
	UserName   string     `json:"userName,omitempty"`
	Roles      []string   `json:"roles,omitempty"`
	Teams      []string   `json:"teams,omitempty"`
	Result     string     `json:"result,omitempty"`
	Comment    string     `json:"comment,omitempty"`
	StageIndex *int       `json:"stageIndex,omitempty"`
	StageName  string     `json:"stageName,omitempty"`
	DecidedAt  *time.Time `json:"decidedAt,omitempty"`
}

type ApprovalStageTrace struct {
	StageIndex  *int       `json:"stageIndex,omitempty"`
	StageName   string     `json:"stageName,omitempty"`
	Result      string     `json:"result,omitempty"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
}

type ApprovalTimelineEvent struct {
	ID         string         `json:"id"`
	Kind       string         `json:"kind"`
	Action     string         `json:"action"`
	Result     string         `json:"result"`
	Summary    string         `json:"summary,omitempty"`
	ActorType  string         `json:"actorType,omitempty"`
	ActorID    string         `json:"actorId,omitempty"`
	ActorName  string         `json:"actorName,omitempty"`
	StageIndex *int           `json:"stageIndex,omitempty"`
	StageName  string         `json:"stageName,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	CreatedAt  time.Time      `json:"createdAt"`
}

type ApprovalRequest struct {
	ID                string         `json:"id"`
	Status            string         `json:"status"`
	Strategy          string         `json:"strategy"`
	PolicyID          string         `json:"policyId,omitempty"`
	ApprovalPolicyRef string         `json:"approvalPolicyRef,omitempty"`
	ActorType         string         `json:"actorType"`
	ActorID           string         `json:"actorId"`
	ActorName         string         `json:"actorName,omitempty"`
	ActorRoles        []string       `json:"actorRoles,omitempty"`
	ActorTeams        []string       `json:"actorTeams,omitempty"`
	AIClientID        string         `json:"aiClientId,omitempty"`
	AIClientName      string         `json:"aiClientName,omitempty"`
	SkillID           string         `json:"skillId,omitempty"`
	ToolName          string         `json:"toolName"`
	RiskLevel         string         `json:"riskLevel"`
	RequiresApproval  bool           `json:"requiresApproval"`
	ResourceScope     map[string]any `json:"resourceScope,omitempty"`
	ToolInput         map[string]any `json:"toolInput,omitempty"`
	RelatedIDs        map[string]any `json:"relatedIds,omitempty"`
	ApprovalTrace     *ApprovalTrace `json:"approvalTrace,omitempty"`
	Output            any            `json:"output,omitempty"`
	Summary           string         `json:"summary"`
	RequestID         string         `json:"requestId,omitempty"`
	SourceIP          string         `json:"sourceIp,omitempty"`
	DecidedBy         string         `json:"decidedBy,omitempty"`
	DecidedByName     string         `json:"decidedByName,omitempty"`
	DecidedAt         *time.Time     `json:"decidedAt,omitempty"`
	DecisionComment   string         `json:"decisionComment,omitempty"`
	ExpiresAt         *time.Time     `json:"expiresAt,omitempty"`
	CreatedAt         time.Time      `json:"createdAt"`
	UpdatedAt         time.Time      `json:"updatedAt"`
}

type ApprovalDecisionResult struct {
	Request    ApprovalRequest       `json:"request"`
	Invocation *ToolInvocationResult `json:"invocation,omitempty"`
}

type ApprovalTimeline struct {
	Request ApprovalRequest         `json:"request"`
	Trace   *ApprovalTrace          `json:"trace,omitempty"`
	Events  []ApprovalTimelineEvent `json:"events,omitempty"`
}

type GovernanceStatus struct {
	GeneratedAt           time.Time                        `json:"generatedAt"`
	WindowHours           int                              `json:"windowHours"`
	Health                GovernanceHealth                 `json:"health"`
	Metrics               GovernanceMetrics                `json:"metrics"`
	Tokens                GovernanceTokenSummary           `json:"tokens"`
	Clients               GovernanceClientSummary          `json:"clients"`
	Approvals             GovernanceApprovalSummary        `json:"approvals"`
	PolicyCoverage        GovernancePolicyCoverage         `json:"policyCoverage"`
	Redaction             GovernanceRedactionSummary       `json:"redaction"`
	Anomalies             []GovernanceFinding              `json:"anomalies,omitempty"`
	Recommendations       []string                         `json:"recommendations,omitempty"`
	RecommendationActions []GovernanceRecommendationAction `json:"recommendationActions,omitempty"`
	Metadata              map[string]any                   `json:"metadata,omitempty"`
}

type GovernanceHealth struct {
	Status  string                  `json:"status"`
	Message string                  `json:"message"`
	Checks  []GovernanceHealthCheck `json:"checks,omitempty"`
}

type GovernanceHealthCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
	Count   int    `json:"count,omitempty"`
}

type GovernanceMetrics struct {
	TotalCalls            int                     `json:"totalCalls"`
	SuccessCount          int                     `json:"successCount"`
	DenyCount             int                     `json:"denyCount"`
	FailureCount          int                     `json:"failureCount"`
	PendingApprovalCount  int                     `json:"pendingApprovalCount"`
	DryRunCount           int                     `json:"dryRunCount"`
	RiskCounts            map[string]int          `json:"riskCounts,omitempty"`
	TopTools              []GovernanceMetricCount `json:"topTools,omitempty"`
	TopAIClients          []GovernanceMetricCount `json:"topAiClients,omitempty"`
	TopActors             []GovernanceMetricCount `json:"topActors,omitempty"`
	RecentResultBreakdown map[string]int          `json:"recentResultBreakdown,omitempty"`
	RecentActionBreakdown map[string]int          `json:"recentActionBreakdown,omitempty"`
}

type GovernanceMetricCount struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

type GovernanceRedactionSummary struct {
	TotalMatches            int                     `json:"totalMatches"`
	AuditsWithRedaction     int                     `json:"auditsWithRedaction"`
	InputAudits             int                     `json:"inputAudits"`
	OutputAudits            int                     `json:"outputAudits"`
	FieldMatches            int                     `json:"fieldMatches"`
	SensitiveKeyMatches     int                     `json:"sensitiveKeyMatches"`
	SensitiveTextMatches    int                     `json:"sensitiveTextMatches"`
	ValuePatternMatches     int                     `json:"valuePatternMatches"`
	SecretClassifierMatches int                     `json:"secretClassifierMatches"`
	StructuredSecretMatches int                     `json:"structuredSecretMatches"`
	TopTargets              []GovernanceMetricCount `json:"topTargets,omitempty"`
	TopFieldPaths           []GovernanceMetricCount `json:"topFieldPaths,omitempty"`
	TopMatchTypes           []GovernanceMetricCount `json:"topMatchTypes,omitempty"`
	TopClassifiers          []GovernanceMetricCount `json:"topClassifiers,omitempty"`
	TopPolicies             []GovernanceMetricCount `json:"topPolicies,omitempty"`
	TopTools                []GovernanceMetricCount `json:"topTools,omitempty"`
}

type GovernanceTokenSummary struct {
	PersonalAccessTokens  GovernanceTokenCounts    `json:"personalAccessTokens"`
	ServiceAccountTokens  GovernanceTokenCounts    `json:"serviceAccountTokens"`
	ExpiringSoon          []GovernanceTokenFinding `json:"expiringSoon,omitempty"`
	ExpiredActive         []GovernanceTokenFinding `json:"expiredActive,omitempty"`
	Stale                 []GovernanceTokenFinding `json:"stale,omitempty"`
	NeverUsed             []GovernanceTokenFinding `json:"neverUsed,omitempty"`
	LastUsedTrackingState string                   `json:"lastUsedTrackingState"`
}

type GovernanceTokenCounts struct {
	Total        int `json:"total"`
	Active       int `json:"active"`
	Revoked      int `json:"revoked"`
	Expired      int `json:"expired"`
	ExpiringSoon int `json:"expiringSoon"`
	Stale        int `json:"stale"`
	NeverUsed    int `json:"neverUsed"`
}

type GovernanceTokenFinding struct {
	Kind         string     `json:"kind"`
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	OwnerID      string     `json:"ownerId,omitempty"`
	TokenPrefix  string     `json:"tokenPrefix"`
	Severity     string     `json:"severity"`
	Message      string     `json:"message"`
	ExpiresAt    *time.Time `json:"expiresAt,omitempty"`
	LastUsedAt   *time.Time `json:"lastUsedAt,omitempty"`
	DaysUntilDue int        `json:"daysUntilDue,omitempty"`
	StaleDays    int        `json:"staleDays,omitempty"`
}

type GovernanceClientSummary struct {
	Total                    int      `json:"total"`
	Active                   int      `json:"active"`
	Disabled                 int      `json:"disabled"`
	PendingApproval          int      `json:"pendingApproval"`
	RegistrationApproval     string   `json:"registrationApproval"`
	PendingApprovalClientIDs []string `json:"pendingApprovalClientIds,omitempty"`
}

type GovernanceApprovalSummary struct {
	Pending                int        `json:"pending"`
	DueSoon                int        `json:"dueSoon"`
	StalePending           int        `json:"stalePending"`
	Overdue                int        `json:"overdue"`
	OldestPendingHours     int        `json:"oldestPendingHours,omitempty"`
	OldestPendingRequestID string     `json:"oldestPendingRequestId,omitempty"`
	NextDueAt              *time.Time `json:"nextDueAt,omitempty"`
	NextDueRequestID       string     `json:"nextDueRequestId,omitempty"`
	DueSoonRequestIDs      []string   `json:"dueSoonRequestIds,omitempty"`
	StalePendingRequestIDs []string   `json:"stalePendingRequestIds,omitempty"`
	OverdueRequestIDs      []string   `json:"overdueRequestIds,omitempty"`
}

type GovernancePolicyCoverage struct {
	AccessPolicies               int    `json:"accessPolicies"`
	ToolGrants                   int    `json:"toolGrants"`
	SkillBindings                int    `json:"skillBindings"`
	ActiveAccessPolicies         int    `json:"activeAccessPolicies"`
	ActiveToolGrants             int    `json:"activeToolGrants"`
	ActiveSkillBindings          int    `json:"activeSkillBindings"`
	BudgetPolicies               int    `json:"budgetPolicies"`
	RateLimitPolicies            int    `json:"rateLimitPolicies"`
	RedactionPolicies            int    `json:"redactionPolicies"`
	ResourceScopedAccessPolicies int    `json:"resourceScopedAccessPolicies"`
	ResourceScopedToolGrants     int    `json:"resourceScopedToolGrants"`
	BudgetState                  string `json:"budgetState"`
	RateLimitState               string `json:"rateLimitState"`
	RedactionPolicyState         string `json:"redactionPolicyState"`
	ResourceScopeState           string `json:"resourceScopeState"`
}

type GovernanceFinding struct {
	Type              string `json:"type"`
	Severity          string `json:"severity"`
	Summary           string `json:"summary"`
	Count             int    `json:"count,omitempty"`
	ActorType         string `json:"actorType,omitempty"`
	ActorID           string `json:"actorId,omitempty"`
	SubjectType       string `json:"subjectType,omitempty"`
	SubjectID         string `json:"subjectId,omitempty"`
	AIClientID        string `json:"aiClientId,omitempty"`
	PolicyID          string `json:"policyId,omitempty"`
	ApprovalRequestID string `json:"approvalRequestId,omitempty"`
	GrantID           string `json:"grantId,omitempty"`
	ToolName          string `json:"toolName,omitempty"`
	RiskLevel         string `json:"riskLevel,omitempty"`
}

type GovernanceRecommendationAction struct {
	Type       string         `json:"type"`
	Severity   string         `json:"severity"`
	Summary    string         `json:"summary"`
	Action     string         `json:"action"`
	TargetKind string         `json:"targetKind,omitempty"`
	TargetID   string         `json:"targetId,omitempty"`
	Refs       []string       `json:"refs,omitempty"`
	Count      int            `json:"count,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

func (c APIClient) Login(ctx context.Context, login, password string) (loginResponse, error) {
	var out loginResponse
	err := c.doJSON(ctx, http.MethodPost, "/api/v1/auth/login", "", nil, map[string]string{
		"login":    login,
		"password": password,
	}, &out)
	return out, err
}

func (c APIClient) Capabilities(ctx context.Context, headers map[string]string) (Manifest, error) {
	var out manifestResponse
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/ai-gateway/capabilities", c.Token, headers, nil, &out); err != nil {
		return Manifest{}, err
	}
	return out.Data, nil
}

func (c APIClient) InvokeTool(ctx context.Context, toolName string, input map[string]any, headers map[string]string) (ToolInvocationResult, error) {
	var out invokeResponse
	path := "/api/v1/ai-gateway/tools/" + url.PathEscape(toolName) + "/invoke"
	payload := map[string]any{"input": emptyInput(input)}
	if err := c.doJSON(ctx, http.MethodPost, path, c.Token, headers, payload, &out); err != nil {
		return ToolInvocationResult{}, err
	}
	return out.Data, nil
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
	var out itemResponse[CreatedPersonalAccessToken]
	if err := c.doJSON(ctx, http.MethodPost, "/api/v1/ai-gateway/personal-access-tokens", c.Token, headers, input, &out); err != nil {
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
	var out itemResponse[CreatedServiceAccountToken]
	path := "/api/v1/ai-gateway/service-accounts/" + url.PathEscape(serviceAccountID) + "/tokens"
	if err := c.doJSON(ctx, http.MethodPost, path, c.Token, headers, input, &out); err != nil {
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

func (c APIClient) doJSON(ctx context.Context, method, path, token string, headers map[string]string, body any, out any) error {
	base := strings.TrimRight(strings.TrimSpace(c.ServerURL), "/")
	if base == "" {
		return fmt.Errorf("server URL is required")
	}
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	if body != nil {
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
	client := c.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s failed: %s: %s", method, path, resp.Status, responseErrorMessage(raw))
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func responseErrorMessage(raw []byte) string {
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
	return strings.TrimSpace(string(raw))
}

func emptyInput(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	return input
}
