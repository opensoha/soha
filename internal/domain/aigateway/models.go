package aigateway

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	domainidentity "github.com/soha/soha/internal/domain/identity"
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
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	PermissionKeys []string `json:"permissionKeys"`
	RequiredScopes []string `json:"requiredScopes,omitempty"`
}

type PromptCapability struct {
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	PermissionKeys []string `json:"permissionKeys"`
	RequiredScopes []string `json:"requiredScopes,omitempty"`
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

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
