package aigateway

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

const (
	PersonalAccessTokenPrefix = "soha_pat_"
	ServiceAccountTokenPrefix = "soha_sat_"
)

type RiskLevel string

const (
	RiskLevelRead    RiskLevel = "read"
	RiskLevelAnalyze RiskLevel = "analyze"
	RiskLevelMutate  RiskLevel = "mutate"
	RiskLevelExecute RiskLevel = "execute"
	RiskLevelHigh    RiskLevel = "high"
)

type ToolCapability struct {
	Name             string         `json:"name"`
	Title            string         `json:"title"`
	Description      string         `json:"description"`
	Domain           string         `json:"domain"`
	Action           string         `json:"action"`
	RiskLevel        RiskLevel      `json:"riskLevel"`
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
	PermissionKeys []string       `json:"permissionKeys"`
	RequiredScopes []string       `json:"requiredScopes,omitempty"`
	ContextSchema  map[string]any `json:"contextSchema,omitempty"`
}

type PromptCapability struct {
	Name           string         `json:"name"`
	Description    string         `json:"description"`
	PermissionKeys []string       `json:"permissionKeys"`
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

type ManifestSummary struct {
	ToolCount     int `json:"toolCount"`
	ResourceCount int `json:"resourceCount"`
	PromptCount   int `json:"promptCount"`
	SkillCount    int `json:"skillCount"`
	DeniedCount   int `json:"deniedCount"`
}

type CallerContext struct {
	IdentityMode string `json:"identityMode"`
	AIClientID   string `json:"aiClientId,omitempty"`
	AIClientName string `json:"aiClientName,omitempty"`
	SkillID      string `json:"skillId,omitempty"`
	TokenID      string `json:"tokenId,omitempty"`
	SessionID    string `json:"sessionId,omitempty"`
	SubjectType  string `json:"subjectType,omitempty"`
	SubjectID    string `json:"subjectId,omitempty"`
	Source       string `json:"source,omitempty"`
}

type Manifest struct {
	Name           string                   `json:"name"`
	Version        string                   `json:"version"`
	GeneratedAt    time.Time                `json:"generatedAt"`
	Principal      domainidentity.Principal `json:"principal"`
	Caller         CallerContext            `json:"caller"`
	PermissionKeys []string                 `json:"permissionKeys"`
	Tools          []ToolCapability         `json:"tools"`
	Resources      []ResourceCapability     `json:"resources,omitempty"`
	Prompts        []PromptCapability       `json:"prompts,omitempty"`
	Skills         []SkillCapability        `json:"skills,omitempty"`
	Summary        ManifestSummary          `json:"summary"`
}

type ManifestRequest struct {
	AIClientID   string
	AIClientName string
	SkillID      string
	TokenID      string
	TokenKind    string
	SessionID    string
	SubjectType  string
	SubjectID    string
	Source       string
}

type ToolInvocationRequest struct {
	ToolName     string         `json:"toolName"`
	Input        map[string]any `json:"input,omitempty"`
	AIClientID   string         `json:"aiClientId,omitempty"`
	AIClientName string         `json:"aiClientName,omitempty"`
	SkillID      string         `json:"skillId,omitempty"`
	RequestID    string         `json:"requestId,omitempty"`
}

type ToolInvocationResult struct {
	ToolName         string         `json:"toolName"`
	RiskLevel        RiskLevel      `json:"riskLevel"`
	RequiresApproval bool           `json:"requiresApproval"`
	Result           string         `json:"result"`
	Output           any            `json:"output,omitempty"`
	RelatedIDs       map[string]any `json:"relatedIds,omitempty"`
	Audit            map[string]any `json:"audit,omitempty"`
}

type ResourceReadRequest struct {
	Name         string         `json:"name,omitempty"`
	URI          string         `json:"uri,omitempty"`
	Context      map[string]any `json:"context,omitempty"`
	AIClientID   string         `json:"aiClientId,omitempty"`
	AIClientName string         `json:"aiClientName,omitempty"`
	SkillID      string         `json:"skillId,omitempty"`
	RequestID    string         `json:"requestId,omitempty"`
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

type PromptGetRequest struct {
	Name         string         `json:"name"`
	Arguments    map[string]any `json:"arguments,omitempty"`
	Context      map[string]any `json:"context,omitempty"`
	AIClientID   string         `json:"aiClientId,omitempty"`
	AIClientName string         `json:"aiClientName,omitempty"`
	SkillID      string         `json:"skillId,omitempty"`
	RequestID    string         `json:"requestId,omitempty"`
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

type AIClient struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Kind           string         `json:"kind"`
	Status         string         `json:"status"`
	RedirectURIs   []string       `json:"redirectUris"`
	AllowedOrigins []string       `json:"allowedOrigins"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	CreatedBy      string         `json:"createdBy"`
	CreatedAt      time.Time      `json:"createdAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
}

type AIClientInput struct {
	ID             string         `json:"id,omitempty"`
	Name           string         `json:"name"`
	Kind           string         `json:"kind"`
	Status         string         `json:"status"`
	RedirectURIs   []string       `json:"redirectUris,omitempty"`
	AllowedOrigins []string       `json:"allowedOrigins,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

type ToolGrant struct {
	ID               string         `json:"id"`
	SubjectType      string         `json:"subjectType"`
	SubjectID        string         `json:"subjectId"`
	AIClientID       string         `json:"aiClientId,omitempty"`
	ToolName         string         `json:"toolName"`
	Effect           string         `json:"effect"`
	RiskLevel        RiskLevel      `json:"riskLevel"`
	PermissionKeys   []string       `json:"permissionKeys,omitempty"`
	ResourceScopes   map[string]any `json:"resourceScopes,omitempty"`
	RequiresApproval bool           `json:"requiresApproval"`
	ExpiresAt        *time.Time     `json:"expiresAt,omitempty"`
	CreatedBy        string         `json:"createdBy"`
	CreatedAt        time.Time      `json:"createdAt"`
	UpdatedAt        time.Time      `json:"updatedAt"`
}

type ToolGrantFilter struct {
	SubjectType    string
	SubjectID      string
	AIClientID     string
	ToolName       string
	IncludeExpired bool
}

type ToolGrantInput struct {
	SubjectType      string         `json:"subjectType"`
	SubjectID        string         `json:"subjectId"`
	AIClientID       string         `json:"aiClientId,omitempty"`
	ToolName         string         `json:"toolName"`
	Effect           string         `json:"effect"`
	RiskLevel        RiskLevel      `json:"riskLevel,omitempty"`
	PermissionKeys   []string       `json:"permissionKeys,omitempty"`
	ResourceScopes   map[string]any `json:"resourceScopes,omitempty"`
	RequiresApproval bool           `json:"requiresApproval"`
	ExpiresAt        *time.Time     `json:"expiresAt,omitempty"`
}

type AccessPolicy struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Description    string         `json:"description,omitempty"`
	Enabled        bool           `json:"enabled"`
	SubjectType    string         `json:"subjectType"`
	SubjectID      string         `json:"subjectId"`
	AIClientID     string         `json:"aiClientId,omitempty"`
	Effect         string         `json:"effect"`
	ToolPatterns   []string       `json:"toolPatterns,omitempty"`
	SkillIDs       []string       `json:"skillIds,omitempty"`
	ResourceScopes map[string]any `json:"resourceScopes,omitempty"`
	RiskLevels     []RiskLevel    `json:"riskLevels,omitempty"`
	ApprovalPolicy map[string]any `json:"approvalPolicy,omitempty"`
	Conditions     map[string]any `json:"conditions,omitempty"`
	CreatedBy      string         `json:"createdBy"`
	CreatedAt      time.Time      `json:"createdAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
}

type AccessPolicyFilter struct {
	SubjectType     string
	SubjectID       string
	AIClientID      string
	Effect          string
	IncludeDisabled bool
}

type AuditLogFilter struct {
	ActorType         string
	ActorID           string
	AIClientID        string
	SkillID           string
	ToolName          string
	ApprovalRequestID string
	RiskLevel         RiskLevel
	Result            string
	Action            string
	From              *time.Time
	To                *time.Time
	Limit             int
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
	RiskLevel         RiskLevel      `json:"riskLevel"`
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

type ApprovalRequestFilter struct {
	ID            string
	Status        string
	ActorType     string
	ActorID       string
	AIClientID    string
	SkillID       string
	ToolName      string
	RiskLevel     RiskLevel
	Strategy      string
	From          *time.Time
	To            *time.Time
	ExpiresBefore *time.Time
	Limit         int
}

type ApprovalRequestUpdate struct {
	ExpectedStatus  string
	Status          string
	Summary         string
	RelatedIDs      map[string]any
	Output          any
	DecidedBy       string
	DecidedByName   string
	DecidedAt       *time.Time
	DecisionComment string
	UpdatedAt       time.Time
}

type ApprovalDecisionInput struct {
	Comment string `json:"comment,omitempty"`
}

type ApprovalDecisionResult struct {
	Request    ApprovalRequest       `json:"request"`
	Invocation *ToolInvocationResult `json:"invocation,omitempty"`
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

type ApprovalTimeline struct {
	Request ApprovalRequest         `json:"request"`
	Trace   *ApprovalTrace          `json:"trace,omitempty"`
	Events  []ApprovalTimelineEvent `json:"events,omitempty"`
}

type GovernanceStatusRequest struct {
	WindowHours int `json:"windowHours,omitempty"`
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
	RiskCounts            map[RiskLevel]int       `json:"riskCounts,omitempty"`
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
	Type              string    `json:"type"`
	Severity          string    `json:"severity"`
	Summary           string    `json:"summary"`
	Count             int       `json:"count,omitempty"`
	ActorType         string    `json:"actorType,omitempty"`
	ActorID           string    `json:"actorId,omitempty"`
	SubjectType       string    `json:"subjectType,omitempty"`
	SubjectID         string    `json:"subjectId,omitempty"`
	AIClientID        string    `json:"aiClientId,omitempty"`
	PolicyID          string    `json:"policyId,omitempty"`
	ApprovalRequestID string    `json:"approvalRequestId,omitempty"`
	GrantID           string    `json:"grantId,omitempty"`
	ToolName          string    `json:"toolName,omitempty"`
	RiskLevel         RiskLevel `json:"riskLevel,omitempty"`
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

type AccessPolicyInput struct {
	Name           string         `json:"name"`
	Description    string         `json:"description,omitempty"`
	Enabled        *bool          `json:"enabled,omitempty"`
	SubjectType    string         `json:"subjectType"`
	SubjectID      string         `json:"subjectId"`
	AIClientID     string         `json:"aiClientId,omitempty"`
	Effect         string         `json:"effect"`
	ToolPatterns   []string       `json:"toolPatterns,omitempty"`
	SkillIDs       []string       `json:"skillIds,omitempty"`
	ResourceScopes map[string]any `json:"resourceScopes,omitempty"`
	RiskLevels     []RiskLevel    `json:"riskLevels,omitempty"`
	ApprovalPolicy map[string]any `json:"approvalPolicy,omitempty"`
	Conditions     map[string]any `json:"conditions,omitempty"`
}

type SkillBinding struct {
	ID             string         `json:"id"`
	SubjectType    string         `json:"subjectType"`
	SubjectID      string         `json:"subjectId"`
	AIClientID     string         `json:"aiClientId,omitempty"`
	SkillID        string         `json:"skillId"`
	CapabilityRefs []string       `json:"capabilityRefs,omitempty"`
	Enabled        bool           `json:"enabled"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	CreatedBy      string         `json:"createdBy"`
	CreatedAt      time.Time      `json:"createdAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
}

type SkillBindingFilter struct {
	SubjectType     string
	SubjectID       string
	AIClientID      string
	SkillID         string
	IncludeDisabled bool
}

type SkillBindingInput struct {
	SubjectType    string         `json:"subjectType"`
	SubjectID      string         `json:"subjectId"`
	AIClientID     string         `json:"aiClientId,omitempty"`
	SkillID        string         `json:"skillId"`
	CapabilityRefs []string       `json:"capabilityRefs,omitempty"`
	Enabled        *bool          `json:"enabled,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

type PersonalAccessToken struct {
	ID             string         `json:"id"`
	UserID         string         `json:"userId"`
	Name           string         `json:"name"`
	TokenHash      string         `json:"-"`
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

type PersonalAccessTokenListRequest struct {
	Scope  string
	UserID string
}

type PersonalAccessTokenInput struct {
	Name           string         `json:"name"`
	Scopes         []string       `json:"scopes"`
	PermissionKeys []string       `json:"permissionKeys"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	ExpiresAt      *time.Time     `json:"expiresAt,omitempty"`
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

type ServiceAccountInput struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Description   string         `json:"description,omitempty"`
	Status        string         `json:"status"`
	OwnerUserID   string         `json:"ownerUserId,omitempty"`
	RoleIDs       []string       `json:"roleIds"`
	TeamIDs       []string       `json:"teamIds"`
	ScopeGrantIDs []string       `json:"scopeGrantIds"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

type ServiceAccountToken struct {
	ID               string         `json:"id"`
	ServiceAccountID string         `json:"serviceAccountId"`
	Name             string         `json:"name"`
	TokenHash        string         `json:"-"`
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

type ServiceAccountTokenInput struct {
	Name           string         `json:"name"`
	Scopes         []string       `json:"scopes"`
	PermissionKeys []string       `json:"permissionKeys"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	ExpiresAt      *time.Time     `json:"expiresAt,omitempty"`
}

type CreatedServiceAccountToken struct {
	Token ServiceAccountToken `json:"token"`
	Value string              `json:"value"`
}

type TokenRotationInput struct {
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

type LLMTokenMetadata struct {
	Purpose                string   `json:"purpose,omitempty"`
	AllowedModels          []string `json:"allowedModels,omitempty"`
	AllowedProviderKinds   []string `json:"allowedProviderKinds,omitempty"`
	AllowedUpstreamIDs     []string `json:"allowedUpstreamIds,omitempty"`
	AllowedIPCIDRs         []string `json:"allowedIPCIDRs,omitempty"`
	AllowedTeams           []string `json:"allowedTeams,omitempty"`
	DeniedTeams            []string `json:"deniedTeams,omitempty"`
	RateLimitProfileID     string   `json:"rateLimitProfileId,omitempty"`
	AllowRouteTrace        bool     `json:"allowRouteTrace,omitempty"`
	AllowUpstreamSelection bool     `json:"allowUpstreamSelection,omitempty"`
}

type LLMUpstream struct {
	ID                   string         `json:"id"`
	Name                 string         `json:"name"`
	ProviderKind         string         `json:"providerKind"`
	BaseURL              string         `json:"baseUrl"`
	APIKeyCiphertext     string         `json:"-"`
	APIKeyPrefix         string         `json:"apiKeyPrefix,omitempty"`
	Status               string         `json:"status"`
	Priority             int            `json:"priority"`
	Weight               int            `json:"weight"`
	TimeoutSeconds       int            `json:"timeoutSeconds"`
	StreamTimeoutSeconds int            `json:"streamTimeoutSeconds"`
	MaxConcurrency       int            `json:"maxConcurrency"`
	SupportedModels      []string       `json:"supportedModels"`
	DefaultHeaders       map[string]any `json:"defaultHeaders,omitempty"`
	ProxyURL             string         `json:"proxyUrl,omitempty"`
	Health               map[string]any `json:"health,omitempty"`
	Metadata             map[string]any `json:"metadata,omitempty"`
	CreatedBy            string         `json:"createdBy"`
	CreatedAt            time.Time      `json:"createdAt"`
	UpdatedAt            time.Time      `json:"updatedAt"`
}

type LLMUpstreamInput struct {
	ID                   string         `json:"id,omitempty"`
	Name                 string         `json:"name"`
	ProviderKind         string         `json:"providerKind"`
	BaseURL              string         `json:"baseUrl"`
	APIKey               string         `json:"apiKey,omitempty"`
	Status               string         `json:"status,omitempty"`
	Priority             int            `json:"priority,omitempty"`
	Weight               int            `json:"weight,omitempty"`
	TimeoutSeconds       int            `json:"timeoutSeconds,omitempty"`
	StreamTimeoutSeconds int            `json:"streamTimeoutSeconds,omitempty"`
	MaxConcurrency       int            `json:"maxConcurrency,omitempty"`
	SupportedModels      []string       `json:"supportedModels,omitempty"`
	DefaultHeaders       map[string]any `json:"defaultHeaders,omitempty"`
	ProxyURL             string         `json:"proxyUrl,omitempty"`
	Health               map[string]any `json:"health,omitempty"`
	Metadata             map[string]any `json:"metadata,omitempty"`
}

type LLMUpstreamFilter struct {
	ProviderKind string
	Status       string
	IncludeAll   bool
}

type LLMModelRoute struct {
	ID                 string         `json:"id"`
	PublicModel        string         `json:"publicModel"`
	ProviderKind       string         `json:"providerKind,omitempty"`
	UpstreamID         string         `json:"upstreamId,omitempty"`
	UpstreamModel      string         `json:"upstreamModel"`
	RouteGroup         string         `json:"routeGroup,omitempty"`
	Priority           int            `json:"priority"`
	Weight             int            `json:"weight"`
	Enabled            bool           `json:"enabled"`
	TransformPolicy    map[string]any `json:"transformPolicy,omitempty"`
	FallbackPolicy     map[string]any `json:"fallbackPolicy,omitempty"`
	CachePolicy        map[string]any `json:"cachePolicy,omitempty"`
	RateLimitProfileID string         `json:"rateLimitProfileId,omitempty"`
	Metadata           map[string]any `json:"metadata,omitempty"`
	CreatedAt          time.Time      `json:"createdAt"`
	UpdatedAt          time.Time      `json:"updatedAt"`
}

type LLMModelRouteInput struct {
	ID                 string         `json:"id,omitempty"`
	PublicModel        string         `json:"publicModel"`
	ProviderKind       string         `json:"providerKind,omitempty"`
	UpstreamID         string         `json:"upstreamId,omitempty"`
	UpstreamModel      string         `json:"upstreamModel"`
	RouteGroup         string         `json:"routeGroup,omitempty"`
	Priority           int            `json:"priority,omitempty"`
	Weight             int            `json:"weight,omitempty"`
	Enabled            *bool          `json:"enabled,omitempty"`
	TransformPolicy    map[string]any `json:"transformPolicy,omitempty"`
	FallbackPolicy     map[string]any `json:"fallbackPolicy,omitempty"`
	CachePolicy        map[string]any `json:"cachePolicy,omitempty"`
	RateLimitProfileID string         `json:"rateLimitProfileId,omitempty"`
	Metadata           map[string]any `json:"metadata,omitempty"`
}

type LLMModelRouteFilter struct {
	PublicModel     string
	ProviderKind    string
	UpstreamID      string
	RouteGroup      string
	IncludeDisabled bool
}

type LLMCallLog struct {
	ID                   string         `json:"id"`
	RequestID            string         `json:"requestId,omitempty"`
	ActorType            string         `json:"actorType,omitempty"`
	ActorID              string         `json:"actorId,omitempty"`
	ActorName            string         `json:"actorName,omitempty"`
	TokenID              string         `json:"tokenId,omitempty"`
	TokenPrefix          string         `json:"tokenPrefix,omitempty"`
	TokenKind            string         `json:"tokenKind,omitempty"`
	AIClientID           string         `json:"aiClientId,omitempty"`
	PublicModel          string         `json:"publicModel,omitempty"`
	UpstreamID           string         `json:"upstreamId,omitempty"`
	UpstreamName         string         `json:"upstreamName,omitempty"`
	ProviderKind         string         `json:"providerKind,omitempty"`
	UpstreamModel        string         `json:"upstreamModel,omitempty"`
	Endpoint             string         `json:"endpoint,omitempty"`
	Stream               bool           `json:"stream"`
	Status               string         `json:"status"`
	HTTPStatus           int            `json:"httpStatus,omitempty"`
	UpstreamStatus       int            `json:"upstreamStatus,omitempty"`
	ErrorCode            string         `json:"errorCode,omitempty"`
	ErrorMessage         string         `json:"errorMessage,omitempty"`
	PromptTokens         int            `json:"promptTokens,omitempty"`
	CompletionTokens     int            `json:"completionTokens,omitempty"`
	TotalTokens          int            `json:"totalTokens,omitempty"`
	ReasoningTokens      int            `json:"reasoningTokens,omitempty"`
	CachedReadTokens     int            `json:"cachedReadTokens,omitempty"`
	CachedWriteTokens    int            `json:"cachedWriteTokens,omitempty"`
	EstimatedTokens      bool           `json:"estimatedTokens"`
	TTFBMilliseconds     int64          `json:"ttfbMs,omitempty"`
	TTFTMilliseconds     int64          `json:"ttftMs,omitempty"`
	DurationMilliseconds int64          `json:"durationMs,omitempty"`
	InputBytes           int64          `json:"inputBytes,omitempty"`
	OutputBytes          int64          `json:"outputBytes,omitempty"`
	CacheStatus          string         `json:"cacheStatus,omitempty"`
	RouteTrace           map[string]any `json:"routeTrace,omitempty"`
	SourceIP             string         `json:"sourceIp,omitempty"`
	UserAgent            string         `json:"userAgent,omitempty"`
	Metadata             map[string]any `json:"metadata,omitempty"`
	CreatedAt            time.Time      `json:"createdAt"`
}

type LLMCallLogFilter struct {
	ActorType    string
	ActorID      string
	TokenID      string
	TokenPrefix  string
	TokenKind    string
	AIClientID   string
	PublicModel  string
	UpstreamID   string
	ProviderKind string
	Status       string
	Endpoint     string
	CacheStatus  string
	From         *time.Time
	To           *time.Time
	Limit        int
}

type LLMRelayMetrics struct {
	RequestsToday     int                     `json:"requestsToday"`
	TotalCalls        int                     `json:"totalCalls"`
	SuccessRate       float64                 `json:"successRate"`
	SuccessCount      int                     `json:"successCount"`
	FailureCount      int                     `json:"failureCount"`
	AverageTTFBMs     float64                 `json:"averageTTFBMs"`
	AverageTTFTMs     float64                 `json:"averageTTFTMs"`
	AverageDurationMs float64                 `json:"averageDurationMs"`
	TokensPerSecond   float64                 `json:"tokensPerSecond"`
	CacheHitCount     int                     `json:"cacheHitCount"`
	CacheReadTokens   int                     `json:"cacheReadTokens"`
	CacheWriteTokens  int                     `json:"cacheWriteTokens"`
	ModelRanking      []GovernanceMetricCount `json:"modelRanking,omitempty"`
	TopModels         []GovernanceMetricCount `json:"topModels,omitempty"`
	UpstreamHealth    []GovernanceMetricCount `json:"upstreamHealth,omitempty"`
	RecentErrors      []LLMCallLog            `json:"recentErrors,omitempty"`
	GeneratedAt       time.Time               `json:"generatedAt"`
}

type LLMRelayCallLogMetrics struct {
	TotalCalls        int
	SuccessCount      int
	FailureCount      int
	AverageTTFBMs     float64
	AverageTTFTMs     float64
	AverageDurationMs float64
	TokensPerSecond   float64
	CacheHitCount     int
	CacheReadTokens   int
	CacheWriteTokens  int
	ModelRanking      []GovernanceMetricCount
	RecentErrors      []LLMCallLog
}

type LLMRelayCacheStatsRequest struct {
	WindowHours int
	PublicModel string
	UpstreamID  string
}

type LLMRelayCacheLogStats struct {
	ResponseCacheHits         int
	ResponseCacheMisses       int
	ResponseCacheWrites       int
	ResponseCacheBypasses     int
	ProviderCachedReadTokens  int
	ProviderCachedWriteTokens int
	ByModel                   []map[string]any
	ByUpstream                []map[string]any
}

type LLMRelayCacheStats struct {
	GeneratedAt               time.Time        `json:"generatedAt"`
	WindowHours               int              `json:"windowHours"`
	ResponseCacheEnabled      bool             `json:"responseCacheEnabled"`
	ResponseCacheHits         int              `json:"responseCacheHits,omitempty"`
	ResponseCacheMisses       int              `json:"responseCacheMisses,omitempty"`
	ResponseCacheWrites       int              `json:"responseCacheWrites,omitempty"`
	ResponseCacheBypasses     int              `json:"responseCacheBypasses,omitempty"`
	ProviderCachedReadTokens  int              `json:"providerCachedReadTokens,omitempty"`
	ProviderCachedWriteTokens int              `json:"providerCachedWriteTokens,omitempty"`
	ByModel                   []map[string]any `json:"byModel,omitempty"`
	ByUpstream                []map[string]any `json:"byUpstream,omitempty"`
}

type LLMRelayCachePurgeRequest struct {
	PublicModel string     `json:"publicModel,omitempty"`
	UpstreamID  string     `json:"upstreamId,omitempty"`
	RouteGroup  string     `json:"routeGroup,omitempty"`
	OlderThan   *time.Time `json:"olderThan,omitempty"`
	DryRun      bool       `json:"dryRun,omitempty"`
}

type LLMRelayCachePurgeResult struct {
	Status      string `json:"status"`
	PurgedCount int    `json:"purgedCount"`
	DryRun      bool   `json:"dryRun"`
}

type LLMUpstreamTestResult struct {
	UpstreamID   string    `json:"upstreamId"`
	ProviderKind string    `json:"providerKind"`
	Status       string    `json:"status"`
	HTTPStatus   int       `json:"httpStatus,omitempty"`
	DurationMs   int64     `json:"durationMs,omitempty"`
	CheckedAt    time.Time `json:"checkedAt"`
}

type LLMRelayHealthCheckRun struct {
	CheckedAt time.Time               `json:"checkedAt"`
	Total     int                     `json:"total"`
	Checked   int                     `json:"checked"`
	Skipped   int                     `json:"skipped"`
	Healthy   int                     `json:"healthy"`
	Degraded  int                     `json:"degraded"`
	Recovered int                     `json:"recovered"`
	Failed    int                     `json:"failed"`
	Results   []LLMUpstreamTestResult `json:"results,omitempty"`
}

type LLMCacheEntry struct {
	ID                     string         `json:"id"`
	CacheKey               string         `json:"cacheKey"`
	ScopeKey               string         `json:"scopeKey"`
	PublicModel            string         `json:"publicModel"`
	UpstreamID             string         `json:"upstreamId,omitempty"`
	UpstreamModel          string         `json:"upstreamModel,omitempty"`
	RequestHash            string         `json:"requestHash"`
	ResponseBodyCiphertext string         `json:"-"`
	ResponseHeaders        map[string]any `json:"responseHeaders,omitempty"`
	Status                 string         `json:"status"`
	HitCount               int            `json:"hitCount"`
	ExpiresAt              *time.Time     `json:"expiresAt,omitempty"`
	LastHitAt              *time.Time     `json:"lastHitAt,omitempty"`
	Metadata               map[string]any `json:"metadata,omitempty"`
	CreatedAt              time.Time      `json:"createdAt"`
	UpdatedAt              time.Time      `json:"updatedAt"`
}

type LLMCacheEntryFilter struct {
	CacheKey      string
	ScopeKey      string
	PublicModel   string
	UpstreamID    string
	Status        string
	ExpiresAfter  *time.Time
	ExpiresBefore *time.Time
	UpdatedBefore *time.Time
	Limit         int
}

type LLMHealthEvent struct {
	ID                  string         `json:"id"`
	UpstreamID          string         `json:"upstreamId,omitempty"`
	UpstreamName        string         `json:"upstreamName,omitempty"`
	ProviderKind        string         `json:"providerKind,omitempty"`
	EventType           string         `json:"eventType"`
	Status              string         `json:"status"`
	HTTPStatus          int            `json:"httpStatus,omitempty"`
	LatencyMilliseconds int64          `json:"latencyMs,omitempty"`
	ErrorCode           string         `json:"errorCode,omitempty"`
	ErrorMessage        string         `json:"errorMessage,omitempty"`
	Message             string         `json:"message,omitempty"`
	Metadata            map[string]any `json:"metadata,omitempty"`
	CreatedAt           time.Time      `json:"createdAt"`
}

type LLMHealthEventFilter struct {
	UpstreamID   string
	ProviderKind string
	EventType    string
	Status       string
	From         *time.Time
	To           *time.Time
	Limit        int
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
	RiskLevel     RiskLevel      `json:"riskLevel,omitempty"`
	ResourceScope map[string]any `json:"resourceScope,omitempty"`
	Action        string         `json:"action"`
	Result        string         `json:"result"`
	Summary       string         `json:"summary"`
	RequestID     string         `json:"requestId,omitempty"`
	SourceIP      string         `json:"sourceIp,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	CreatedAt     time.Time      `json:"createdAt"`
}

type RateLimitCounter struct {
	Key         string         `json:"key"`
	PolicyID    string         `json:"policyId"`
	Scope       string         `json:"scope"`
	ActorType   string         `json:"actorType,omitempty"`
	ActorID     string         `json:"actorId,omitempty"`
	AIClientID  string         `json:"aiClientId,omitempty"`
	ToolName    string         `json:"toolName,omitempty"`
	WindowStart time.Time      `json:"windowStart"`
	WindowEnd   time.Time      `json:"windowEnd"`
	Limit       int            `json:"limit"`
	Count       int            `json:"count"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	CreatedAt   time.Time      `json:"createdAt"`
	UpdatedAt   time.Time      `json:"updatedAt"`
}

type RateLimitState struct {
	Key             string         `json:"key"`
	PolicyID        string         `json:"policyId"`
	Scope           string         `json:"scope"`
	ActorType       string         `json:"actorType,omitempty"`
	ActorID         string         `json:"actorId,omitempty"`
	AIClientID      string         `json:"aiClientId,omitempty"`
	ToolName        string         `json:"toolName,omitempty"`
	Limit           int            `json:"limit"`
	Burst           int            `json:"burst"`
	IntervalSeconds float64        `json:"intervalSeconds"`
	TAT             time.Time      `json:"tat"`
	Allowed         bool           `json:"allowed"`
	RetryAfter      time.Duration  `json:"retryAfter,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
	CreatedAt       time.Time      `json:"createdAt"`
	UpdatedAt       time.Time      `json:"updatedAt"`
}

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
