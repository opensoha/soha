package aigateway

import (
	"context"
	"net/http"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
)

type PersonalAccessTokenRepository interface {
	ListPersonalAccessTokens(context.Context, string) ([]domainaigateway.PersonalAccessToken, error)
	ListAllPersonalAccessTokens(context.Context) ([]domainaigateway.PersonalAccessToken, error)
	CreatePersonalAccessToken(context.Context, domainaigateway.PersonalAccessToken) (domainaigateway.PersonalAccessToken, error)
	RevokePersonalAccessToken(context.Context, string, string) error
}

type ServiceAccountRepository interface {
	ListServiceAccounts(context.Context) ([]domainaigateway.ServiceAccount, error)
	CreateServiceAccount(context.Context, domainaigateway.ServiceAccount) (domainaigateway.ServiceAccount, error)
	GetServiceAccount(context.Context, string) (domainaigateway.ServiceAccount, error)
	ListAllServiceAccountTokens(context.Context) ([]domainaigateway.ServiceAccountToken, error)
	CreateServiceAccountToken(context.Context, domainaigateway.ServiceAccountToken) (domainaigateway.ServiceAccountToken, error)
	RevokeServiceAccountToken(context.Context, string) error
}

type AIClientRepository interface {
	ListAIClients(context.Context) ([]domainaigateway.AIClient, error)
	GetAIClient(context.Context, string) (domainaigateway.AIClient, error)
	CreateAIClient(context.Context, domainaigateway.AIClient) (domainaigateway.AIClient, error)
	UpdateAIClient(context.Context, domainaigateway.AIClient) (domainaigateway.AIClient, error)
}

type ToolGrantRepository interface {
	ListToolGrants(context.Context, domainaigateway.ToolGrantFilter) ([]domainaigateway.ToolGrant, error)
	CreateToolGrant(context.Context, domainaigateway.ToolGrant) (domainaigateway.ToolGrant, error)
	DeleteToolGrant(context.Context, string) error
	ListActiveToolGrants(context.Context, string, string, string, time.Time) ([]domainaigateway.ToolGrant, error)
}

type AccessPolicyRepository interface {
	ListAccessPolicies(context.Context, domainaigateway.AccessPolicyFilter) ([]domainaigateway.AccessPolicy, error)
	CreateAccessPolicy(context.Context, domainaigateway.AccessPolicy) (domainaigateway.AccessPolicy, error)
	UpdateAccessPolicy(context.Context, domainaigateway.AccessPolicy) (domainaigateway.AccessPolicy, error)
	DeleteAccessPolicy(context.Context, string) error
	ListActiveAccessPolicies(context.Context, string, string, string) ([]domainaigateway.AccessPolicy, error)
}

type SkillBindingRepository interface {
	ListSkillBindings(context.Context, domainaigateway.SkillBindingFilter) ([]domainaigateway.SkillBinding, error)
	CreateSkillBinding(context.Context, domainaigateway.SkillBinding) (domainaigateway.SkillBinding, error)
	UpdateSkillBinding(context.Context, domainaigateway.SkillBinding) (domainaigateway.SkillBinding, error)
	DeleteSkillBinding(context.Context, string) error
	ListActiveSkillBindings(context.Context, string, string, string) ([]domainaigateway.SkillBinding, error)
}

type AuditLogRepository interface {
	ListAuditLogs(context.Context, domainaigateway.AuditLogFilter) ([]domainaigateway.AuditLog, error)
	CreateAuditLog(context.Context, domainaigateway.AuditLog) error
}

type RateLimitRepository interface {
	IncrementRateLimitCounter(context.Context, domainaigateway.RateLimitCounter) (domainaigateway.RateLimitCounter, error)
	ApplyRateLimitState(context.Context, domainaigateway.RateLimitState) (domainaigateway.RateLimitState, error)
}

type ApprovalRepository interface {
	CreateApprovalRequest(context.Context, domainaigateway.ApprovalRequest) (domainaigateway.ApprovalRequest, error)
	GetApprovalRequest(context.Context, string) (domainaigateway.ApprovalRequest, error)
	ListApprovalRequests(context.Context, domainaigateway.ApprovalRequestFilter) ([]domainaigateway.ApprovalRequest, error)
	UpdateApprovalRequest(context.Context, string, domainaigateway.ApprovalRequestUpdate) (domainaigateway.ApprovalRequest, error)
	ExpirePendingApprovalRequests(context.Context, time.Time) ([]domainaigateway.ApprovalRequest, error)
}

type LLMRelayRepository interface {
	ListLLMUpstreams(context.Context, domainaigateway.LLMUpstreamFilter) ([]domainaigateway.LLMUpstream, error)
	GetLLMUpstream(context.Context, string) (domainaigateway.LLMUpstream, error)
	CreateLLMUpstream(context.Context, domainaigateway.LLMUpstream) (domainaigateway.LLMUpstream, error)
	UpdateLLMUpstream(context.Context, domainaigateway.LLMUpstream) (domainaigateway.LLMUpstream, error)
	ListLLMModelRoutes(context.Context, domainaigateway.LLMModelRouteFilter) ([]domainaigateway.LLMModelRoute, error)
	GetLLMModelRoute(context.Context, string) (domainaigateway.LLMModelRoute, error)
	CreateLLMModelRoute(context.Context, domainaigateway.LLMModelRoute) (domainaigateway.LLMModelRoute, error)
	UpdateLLMModelRoute(context.Context, domainaigateway.LLMModelRoute) (domainaigateway.LLMModelRoute, error)
	DeleteLLMModelRoute(context.Context, string) error
	ListLLMCallLogs(context.Context, domainaigateway.LLMCallLogFilter) ([]domainaigateway.LLMCallLog, error)
	LLMRelayCallLogMetrics(context.Context, domainaigateway.LLMCallLogFilter) (domainaigateway.LLMRelayCallLogMetrics, error)
	SumLLMCallTokens(context.Context, domainaigateway.LLMCallLogFilter) (int, error)
	LLMRelayCacheLogStats(context.Context, domainaigateway.LLMCallLogFilter) (domainaigateway.LLMRelayCacheLogStats, error)
	CreateLLMCallLog(context.Context, domainaigateway.LLMCallLog) error
	GetLLMCacheEntryByKey(context.Context, string) (domainaigateway.LLMCacheEntry, error)
	CountLLMCacheEntries(context.Context, domainaigateway.LLMCacheEntryFilter) (int, error)
	CreateLLMCacheEntry(context.Context, domainaigateway.LLMCacheEntry) (domainaigateway.LLMCacheEntry, error)
	UpdateLLMCacheEntry(context.Context, domainaigateway.LLMCacheEntry) (domainaigateway.LLMCacheEntry, error)
	DeleteLLMCacheEntries(context.Context, domainaigateway.LLMCacheEntryFilter) (int, error)
	CreateLLMHealthEvent(context.Context, domainaigateway.LLMHealthEvent) error
}

type LLMRelayConfig struct {
	Enabled                     bool
	DefaultTimeout              time.Duration
	StreamTimeout               time.Duration
	HealthCheckEnabled          bool
	HealthCheckInterval         time.Duration
	MaxRequestBodyBytes         int64
	AllowInsecureUpstreamHTTP   bool
	AllowPrivateUpstreamHosts   bool
	IncludeUsageForOpenAIStream bool
	CredentialEncryptionKey     string
}

type ServiceDeps struct {
	Permissions *appaccess.PermissionResolver
	Audit       AuditRecorder

	PersonalTokens  PersonalAccessTokenRepository
	ServiceAccounts ServiceAccountRepository
	Clients         AIClientRepository
	ToolGrants      ToolGrantRepository
	AccessPolicies  AccessPolicyRepository
	SkillBindings   SkillBindingRepository
	AuditLogs       AuditLogRepository
	RateLimits      RateLimitRepository
	Approvals       ApprovalRepository
	LLMRelay        LLMRelayRepository

	RateLimitBackend RateLimitBackend
	RelayConfig      LLMRelayConfig
	HTTPClient       *http.Client
}
