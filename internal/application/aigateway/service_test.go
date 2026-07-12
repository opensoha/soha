package aigateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainalert "github.com/opensoha/soha/internal/domain/alert"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainoperation "github.com/opensoha/soha/internal/domain/operation"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type stubRolePermissionReader struct {
	matrix map[string][]string
}

func (r stubRolePermissionReader) ListRolePermissions(context.Context) (map[string][]string, error) {
	return r.matrix, nil
}

type captureAuditRecorder struct {
	entries []domainaudit.Entry
}

func (r *captureAuditRecorder) Record(_ context.Context, entry domainaudit.Entry) error {
	r.entries = append(r.entries, entry)
	return nil
}

type captureOperationRecorder struct {
	entries []domainoperation.Entry
}

func (r *captureOperationRecorder) Record(_ context.Context, entry domainoperation.Entry) error {
	r.entries = append(r.entries, entry)
	return nil
}

type memoryGatewayRepository struct {
	personalTokens       []domainaigateway.PersonalAccessToken
	serviceAccounts      map[string]domainaigateway.ServiceAccount
	serviceAccountTokens []domainaigateway.ServiceAccountToken
	aiClients            map[string]domainaigateway.AIClient
	toolGrants           []domainaigateway.ToolGrant
	accessPolicies       []domainaigateway.AccessPolicy
	skillBindings        []domainaigateway.SkillBinding
	auditLogs            []domainaigateway.AuditLog
	approvalRequests     []domainaigateway.ApprovalRequest
	rateLimitCounters    map[string]domainaigateway.RateLimitCounter
	rateLimitCounterErr  error
	rateLimitStates      map[string]domainaigateway.RateLimitState
	rateLimitStateErr    error
}

func newTestService(permissions *appaccess.PermissionResolver, audit AuditRecorder, repos ...*memoryGatewayRepository) *Service {
	var repo *memoryGatewayRepository
	if len(repos) > 0 {
		repo = repos[0]
	}
	return newTestServiceWithRelay(permissions, audit, repo, nil)
}

func newTestServiceWithRelay(permissions *appaccess.PermissionResolver, audit AuditRecorder, repo *memoryGatewayRepository, relayRepo LLMRelayRepository) *Service {
	deps := ServiceDeps{
		Permissions: permissions,
		Audit:       audit,
		LLMRelay:    relayRepo,
	}
	if repo != nil {
		deps.PersonalTokens = repo
		deps.ServiceAccounts = repo
		deps.Clients = repo
		deps.ToolGrants = repo
		deps.AccessPolicies = repo
		deps.SkillBindings = repo
		deps.AuditLogs = repo
		deps.RateLimits = repo
		deps.Approvals = repo
	}
	return NewWithDeps(deps)
}

type fakeRateLimitBackend struct {
	counterCalls int
	stateCalls   int
	counterErr   error
	stateErr     error
	counters     map[string]domainaigateway.RateLimitCounter
	states       map[string]domainaigateway.RateLimitState
}

type testCapabilityProvider struct {
	tools        []domainaigateway.ToolCapability
	resources    []domainaigateway.ResourceCapability
	prompts      []domainaigateway.PromptCapability
	skills       []domainaigateway.SkillCapability
	resourceRefs []ResourceCapabilityRefs
	invoke       func(context.Context, domainidentity.Principal, domainaigateway.ToolCapability, map[string]any) (any, map[string]any, error)
}

func customTestCapabilityProvider() testCapabilityProvider {
	return testCapabilityProvider{
		tools: []domainaigateway.ToolCapability{
			{Name: "custom.echo", RiskLevel: domainaigateway.RiskLevelRead, PermissionKeys: []string{appaccess.PermAIGatewayInvoke}},
			{Name: "custom.blocked", RiskLevel: domainaigateway.RiskLevelRead, PermissionKeys: []string{appaccess.PermAIGatewayInvoke}},
		},
		resources: []domainaigateway.ResourceCapability{{Name: "soha://custom/context", PermissionKeys: []string{appaccess.PermAIGatewayInvoke}}},
		prompts: []domainaigateway.PromptCapability{
			{Name: "custom.prompt", PermissionKeys: []string{appaccess.PermAIGatewayInvoke}},
			{Name: "custom.blocked_prompt", PermissionKeys: []string{appaccess.PermAIGatewayInvoke}},
		},
		skills:       []domainaigateway.SkillCapability{{ID: "custom-skill", Name: "Custom Skill", CapabilityRefs: []string{"custom.echo"}, PermissionKeys: []string{appaccess.PermAIGatewayInvoke}}},
		resourceRefs: []ResourceCapabilityRefs{{Resource: "soha://custom/context", Tools: []string{"custom.echo"}, Prompts: []string{"custom.prompt"}, Skills: []string{"custom-skill"}}},
	}
}

func newSecretClassifierTestService(policyID string, secretTypes []any) (*fakeApplicationService, *memoryGatewayRepository, *Service) {
	apps := &fakeApplicationService{}
	repo := &memoryGatewayRepository{accessPolicies: []domainaigateway.AccessPolicy{{
		ID: policyID, Enabled: true, SubjectType: "role", SubjectID: "developer", Effect: "allow",
		ToolPatterns: []string{"delivery.applications.list"},
		Conditions:   map[string]any{"redactionPolicy": map[string]any{"mode": "strict", "secretTypes": secretTypes}},
	}}}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{matrix: map[string][]string{
		"developer": {appaccess.PermAIGatewayInvoke, appaccess.PermDeliveryApplicationsView},
	}}), nil, repo)
	service.SetDeliveryServices(apps, &fakeDeliveryService{})
	return apps, repo, service
}

func (p testCapabilityProvider) Tools() []domainaigateway.ToolCapability {
	return append([]domainaigateway.ToolCapability(nil), p.tools...)
}

func (p testCapabilityProvider) Resources() []domainaigateway.ResourceCapability {
	return append([]domainaigateway.ResourceCapability(nil), p.resources...)
}

func (p testCapabilityProvider) Prompts() []domainaigateway.PromptCapability {
	return append([]domainaigateway.PromptCapability(nil), p.prompts...)
}

func (p testCapabilityProvider) Skills() []domainaigateway.SkillCapability {
	return append([]domainaigateway.SkillCapability(nil), p.skills...)
}

func (p testCapabilityProvider) ResourceCapabilityRefs() []ResourceCapabilityRefs {
	return append([]ResourceCapabilityRefs(nil), p.resourceRefs...)
}

func (p testCapabilityProvider) InvokeTool(ctx context.Context, principal domainidentity.Principal, tool domainaigateway.ToolCapability, input map[string]any) (any, map[string]any, error) {
	if p.invoke == nil {
		return nil, nil, fmt.Errorf("%w: tool %s is not implemented yet", apperrors.ErrInvalidArgument, tool.Name)
	}
	return p.invoke(ctx, principal, tool, input)
}

func TestCapabilityRegistryMergesResourceRefs(t *testing.T) {
	registry := newCapabilityRegistry(
		testCapabilityProvider{
			resourceRefs: []ResourceCapabilityRefs{
				{
					Resource: "soha://custom/context",
					Tools:    []string{"custom.echo", "custom.echo"},
					Prompts:  []string{"custom.prompt"},
				},
			},
		},
		testCapabilityProvider{
			resourceRefs: []ResourceCapabilityRefs{
				{
					Resource: "soha://resource/soha://custom/context",
					Tools:    []string{"custom.inspect"},
					Skills:   []string{"custom-skill"},
				},
			},
		},
	)

	refs := registry.ResourceRefs("resource/soha://custom/context")
	if refs.Resource != "soha://custom/context" || !slices.Equal(refs.Tools, []string{"custom.echo", "custom.inspect"}) || !slices.Equal(refs.Prompts, []string{"custom.prompt"}) || !slices.Equal(refs.Skills, []string{"custom-skill"}) {
		t.Fatalf("expected merged normalized resource refs, got %#v", refs)
	}
	if len(registry.ResourceCapabilityRefs()) != 1 {
		t.Fatalf("expected duplicate resource refs to merge, got %#v", registry.ResourceCapabilityRefs())
	}
}

func (b *fakeRateLimitBackend) IncrementRateLimitCounter(_ context.Context, item domainaigateway.RateLimitCounter) (domainaigateway.RateLimitCounter, error) {
	b.counterCalls++
	if b.counterErr != nil {
		return domainaigateway.RateLimitCounter{}, b.counterErr
	}
	if b.counters == nil {
		b.counters = map[string]domainaigateway.RateLimitCounter{}
	}
	existing := b.counters[item.Key]
	if existing.Key == "" {
		item.Count = 1
		b.counters[item.Key] = item
		return item, nil
	}
	existing.Count++
	existing.Limit = item.Limit
	existing.WindowEnd = item.WindowEnd
	existing.Metadata = item.Metadata
	b.counters[item.Key] = existing
	return existing, nil
}

func (b *fakeRateLimitBackend) ApplyRateLimitState(_ context.Context, item domainaigateway.RateLimitState) (domainaigateway.RateLimitState, error) {
	b.stateCalls++
	if b.stateErr != nil {
		return domainaigateway.RateLimitState{}, b.stateErr
	}
	if b.states == nil {
		b.states = map[string]domainaigateway.RateLimitState{}
	}
	existing := b.states[item.Key]
	if existing.Key == "" {
		item.Allowed = true
		item.TAT = time.Now().UTC().Add(time.Duration(item.IntervalSeconds * float64(time.Second)))
		b.states[item.Key] = item
		return item, nil
	}
	existing.Allowed = false
	existing.RetryAfter = time.Duration(existing.IntervalSeconds * float64(time.Second))
	existing.PolicyID = item.PolicyID
	existing.Burst = item.Burst
	existing.Limit = item.Limit
	b.states[item.Key] = existing
	return existing, nil
}

func (r *memoryGatewayRepository) ListPersonalAccessTokens(_ context.Context, userID string) ([]domainaigateway.PersonalAccessToken, error) {
	items := make([]domainaigateway.PersonalAccessToken, 0)
	for _, item := range r.personalTokens {
		if item.UserID == userID {
			items = append(items, item)
		}
	}
	return items, nil
}

func (r *memoryGatewayRepository) ListAllPersonalAccessTokens(context.Context) ([]domainaigateway.PersonalAccessToken, error) {
	return append([]domainaigateway.PersonalAccessToken(nil), r.personalTokens...), nil
}

func (r *memoryGatewayRepository) CreatePersonalAccessToken(_ context.Context, item domainaigateway.PersonalAccessToken) (domainaigateway.PersonalAccessToken, error) {
	r.personalTokens = append(r.personalTokens, item)
	return item, nil
}

func (r *memoryGatewayRepository) RevokePersonalAccessToken(_ context.Context, userID, tokenID string) error {
	for index := range r.personalTokens {
		if r.personalTokens[index].UserID == userID && r.personalTokens[index].ID == tokenID {
			now := time.Now().UTC()
			r.personalTokens[index].RevokedAt = &now
			r.personalTokens[index].UpdatedAt = now
			return nil
		}
	}
	return apperrors.ErrNotFound
}

func (r *memoryGatewayRepository) ListServiceAccounts(context.Context) ([]domainaigateway.ServiceAccount, error) {
	items := make([]domainaigateway.ServiceAccount, 0, len(r.serviceAccounts))
	for _, item := range r.serviceAccounts {
		items = append(items, item)
	}
	return items, nil
}

func (r *memoryGatewayRepository) CreateServiceAccount(_ context.Context, item domainaigateway.ServiceAccount) (domainaigateway.ServiceAccount, error) {
	if r.serviceAccounts == nil {
		r.serviceAccounts = map[string]domainaigateway.ServiceAccount{}
	}
	r.serviceAccounts[item.ID] = item
	return item, nil
}

func (r *memoryGatewayRepository) GetServiceAccount(_ context.Context, serviceAccountID string) (domainaigateway.ServiceAccount, error) {
	return r.serviceAccounts[serviceAccountID], nil
}

func (r *memoryGatewayRepository) CreateServiceAccountToken(_ context.Context, item domainaigateway.ServiceAccountToken) (domainaigateway.ServiceAccountToken, error) {
	r.serviceAccountTokens = append(r.serviceAccountTokens, item)
	return item, nil
}

func (r *memoryGatewayRepository) ListAllServiceAccountTokens(context.Context) ([]domainaigateway.ServiceAccountToken, error) {
	return append([]domainaigateway.ServiceAccountToken(nil), r.serviceAccountTokens...), nil
}

func (r *memoryGatewayRepository) RevokeServiceAccountToken(_ context.Context, tokenID string) error {
	for index := range r.serviceAccountTokens {
		if r.serviceAccountTokens[index].ID == tokenID {
			now := time.Now().UTC()
			r.serviceAccountTokens[index].RevokedAt = &now
			r.serviceAccountTokens[index].UpdatedAt = now
			return nil
		}
	}
	return nil
}

func (r *memoryGatewayRepository) ListAIClients(context.Context) ([]domainaigateway.AIClient, error) {
	items := make([]domainaigateway.AIClient, 0, len(r.aiClients))
	for _, item := range r.aiClients {
		items = append(items, item)
	}
	return items, nil
}

func (r *memoryGatewayRepository) GetAIClient(_ context.Context, clientID string) (domainaigateway.AIClient, error) {
	if r.aiClients == nil {
		return domainaigateway.AIClient{}, nil
	}
	return r.aiClients[clientID], nil
}

func (r *memoryGatewayRepository) CreateAIClient(_ context.Context, item domainaigateway.AIClient) (domainaigateway.AIClient, error) {
	if r.aiClients == nil {
		r.aiClients = map[string]domainaigateway.AIClient{}
	}
	r.aiClients[item.ID] = item
	return item, nil
}

func (r *memoryGatewayRepository) UpdateAIClient(_ context.Context, item domainaigateway.AIClient) (domainaigateway.AIClient, error) {
	if r.aiClients == nil {
		r.aiClients = map[string]domainaigateway.AIClient{}
	}
	r.aiClients[item.ID] = item
	return item, nil
}

func (r *memoryGatewayRepository) ListToolGrants(_ context.Context, filter domainaigateway.ToolGrantFilter) ([]domainaigateway.ToolGrant, error) {
	items := make([]domainaigateway.ToolGrant, 0)
	for _, item := range r.toolGrants {
		if filter.SubjectType != "" && item.SubjectType != filter.SubjectType {
			continue
		}
		if filter.SubjectID != "" && item.SubjectID != filter.SubjectID {
			continue
		}
		if filter.AIClientID != "" && item.AIClientID != filter.AIClientID {
			continue
		}
		if filter.ToolName != "" && item.ToolName != filter.ToolName {
			continue
		}
		items = append(items, item)
	}
	return items, nil
}

func (r *memoryGatewayRepository) CreateToolGrant(_ context.Context, item domainaigateway.ToolGrant) (domainaigateway.ToolGrant, error) {
	r.toolGrants = append(r.toolGrants, item)
	return item, nil
}

func (r *memoryGatewayRepository) DeleteToolGrant(_ context.Context, grantID string) error {
	for index := range r.toolGrants {
		if r.toolGrants[index].ID == grantID {
			r.toolGrants = append(r.toolGrants[:index], r.toolGrants[index+1:]...)
			return nil
		}
	}
	return nil
}

func (r *memoryGatewayRepository) ListActiveToolGrants(_ context.Context, subjectType, subjectID, aiClientID string, at time.Time) ([]domainaigateway.ToolGrant, error) {
	items := make([]domainaigateway.ToolGrant, 0)
	for _, item := range r.toolGrants {
		if item.SubjectType != subjectType || item.SubjectID != subjectID {
			continue
		}
		if item.AIClientID != "" && item.AIClientID != aiClientID {
			continue
		}
		if item.ExpiresAt != nil && !item.ExpiresAt.After(at) {
			continue
		}
		items = append(items, item)
	}
	return items, nil
}

func (r *memoryGatewayRepository) CreateAuditLog(_ context.Context, item domainaigateway.AuditLog) error {
	r.auditLogs = append(r.auditLogs, item)
	return nil
}

func (r *memoryGatewayRepository) ListAuditLogs(_ context.Context, filter domainaigateway.AuditLogFilter) ([]domainaigateway.AuditLog, error) {
	items := make([]domainaigateway.AuditLog, 0)
	for _, item := range r.auditLogs {
		if !auditLogMatchesFilter(item, filter) {
			continue
		}
		items = append(items, item)
		if filter.Limit > 0 && len(items) >= filter.Limit {
			break
		}
	}
	return items, nil
}

func auditLogMatchesFilter(item domainaigateway.AuditLog, filter domainaigateway.AuditLogFilter) bool {
	if filter.ApprovalRequestID != "" && auditApprovalRequestID(item) != filter.ApprovalRequestID {
		return false
	}
	return (filter.ActorType == "" || item.ActorType == filter.ActorType) &&
		(filter.ActorID == "" || item.ActorID == filter.ActorID) &&
		(filter.AIClientID == "" || item.AIClientID == filter.AIClientID) &&
		(filter.SkillID == "" || item.SkillID == filter.SkillID) &&
		(filter.ToolName == "" || item.ToolName == filter.ToolName) &&
		(filter.RiskLevel == "" || item.RiskLevel == filter.RiskLevel) &&
		(filter.Result == "" || item.Result == filter.Result) &&
		(filter.From == nil || !item.CreatedAt.Before(*filter.From)) &&
		(filter.To == nil || !item.CreatedAt.After(*filter.To))
}

func auditApprovalRequestID(item domainaigateway.AuditLog) string {
	if id := firstMapString(item.Metadata, "approvalRequestId"); id != "" {
		return id
	}
	return firstMapString(mapValue(item.Metadata["relatedIds"]), "approvalRequestId")
}

func (r *memoryGatewayRepository) IncrementRateLimitCounter(_ context.Context, item domainaigateway.RateLimitCounter) (domainaigateway.RateLimitCounter, error) {
	if r.rateLimitCounterErr != nil {
		return domainaigateway.RateLimitCounter{}, r.rateLimitCounterErr
	}
	if r.rateLimitCounters == nil {
		r.rateLimitCounters = map[string]domainaigateway.RateLimitCounter{}
	}
	existing := r.rateLimitCounters[item.Key]
	if existing.Key == "" {
		item.Count = 1
		if item.CreatedAt.IsZero() {
			item.CreatedAt = time.Now().UTC()
		}
		item.UpdatedAt = item.CreatedAt
		r.rateLimitCounters[item.Key] = item
		return item, nil
	}
	existing.Count++
	existing.Limit = item.Limit
	existing.WindowEnd = item.WindowEnd
	existing.Metadata = item.Metadata
	existing.UpdatedAt = time.Now().UTC()
	r.rateLimitCounters[item.Key] = existing
	return existing, nil
}

func (r *memoryGatewayRepository) ApplyRateLimitState(_ context.Context, item domainaigateway.RateLimitState) (domainaigateway.RateLimitState, error) {
	if r.rateLimitStateErr != nil {
		return domainaigateway.RateLimitState{}, r.rateLimitStateErr
	}
	if r.rateLimitStates == nil {
		r.rateLimitStates = map[string]domainaigateway.RateLimitState{}
	}
	now := time.Now().UTC()
	existing := r.rateLimitStates[item.Key]
	if existing.Key == "" {
		existing = item
		if existing.CreatedAt.IsZero() {
			existing.CreatedAt = now
		}
	}
	base := existing.TAT
	if base.Before(now) {
		base = now
	}
	interval := time.Duration(item.IntervalSeconds * float64(time.Second))
	if interval <= 0 {
		interval = time.Second
	}
	burst := item.Burst
	if burst <= 0 {
		burst = 1
	}
	tolerance := time.Duration(burst-1) * interval
	allowedAt := existing.TAT.Add(-tolerance)
	if existing.TAT.IsZero() || !allowedAt.After(now) {
		existing.TAT = base.Add(interval)
		existing.Allowed = true
		existing.RetryAfter = 0
	} else {
		existing.Allowed = false
		existing.RetryAfter = allowedAt.Sub(now)
	}
	existing.PolicyID = item.PolicyID
	existing.Scope = item.Scope
	existing.ActorType = item.ActorType
	existing.ActorID = item.ActorID
	existing.AIClientID = item.AIClientID
	existing.ToolName = item.ToolName
	existing.Limit = item.Limit
	existing.Burst = item.Burst
	existing.IntervalSeconds = item.IntervalSeconds
	existing.Metadata = item.Metadata
	existing.UpdatedAt = now
	r.rateLimitStates[item.Key] = existing
	return existing, nil
}

func (r *memoryGatewayRepository) CreateApprovalRequest(_ context.Context, item domainaigateway.ApprovalRequest) (domainaigateway.ApprovalRequest, error) {
	r.approvalRequests = append(r.approvalRequests, item)
	return item, nil
}

func (r *memoryGatewayRepository) GetApprovalRequest(_ context.Context, requestID string) (domainaigateway.ApprovalRequest, error) {
	for _, item := range r.approvalRequests {
		if item.ID == requestID {
			return item, nil
		}
	}
	return domainaigateway.ApprovalRequest{}, nil
}

func (r *memoryGatewayRepository) ListApprovalRequests(_ context.Context, filter domainaigateway.ApprovalRequestFilter) ([]domainaigateway.ApprovalRequest, error) {
	items := make([]domainaigateway.ApprovalRequest, 0)
	for _, item := range r.approvalRequests {
		if !approvalRequestMatchesFilter(item, filter) {
			continue
		}
		items = append(items, item)
		if filter.Limit > 0 && len(items) >= filter.Limit {
			break
		}
	}
	return items, nil
}

func approvalRequestMatchesFilter(item domainaigateway.ApprovalRequest, filter domainaigateway.ApprovalRequestFilter) bool {
	expiresBeforeMatches := filter.ExpiresBefore == nil || (item.ExpiresAt != nil && !item.ExpiresAt.After(*filter.ExpiresBefore))
	return approvalRequestIdentityMatches(item, filter) && approvalRequestTimeMatches(item, filter, expiresBeforeMatches)
}

func approvalRequestIdentityMatches(item domainaigateway.ApprovalRequest, filter domainaigateway.ApprovalRequestFilter) bool {
	return (filter.ID == "" || item.ID == filter.ID) &&
		(filter.Status == "" || item.Status == filter.Status) &&
		(filter.ActorType == "" || item.ActorType == filter.ActorType) &&
		(filter.ActorID == "" || item.ActorID == filter.ActorID) &&
		(filter.AIClientID == "" || item.AIClientID == filter.AIClientID) &&
		(filter.SkillID == "" || item.SkillID == filter.SkillID) &&
		(filter.ToolName == "" || item.ToolName == filter.ToolName) &&
		(filter.RiskLevel == "" || item.RiskLevel == filter.RiskLevel) &&
		(filter.Strategy == "" || item.Strategy == filter.Strategy)
}

func approvalRequestTimeMatches(item domainaigateway.ApprovalRequest, filter domainaigateway.ApprovalRequestFilter, expiresBeforeMatches bool) bool {
	return (filter.From == nil || !item.CreatedAt.Before(*filter.From)) &&
		(filter.To == nil || !item.CreatedAt.After(*filter.To)) && expiresBeforeMatches
}

func (r *memoryGatewayRepository) UpdateApprovalRequest(_ context.Context, requestID string, update domainaigateway.ApprovalRequestUpdate) (domainaigateway.ApprovalRequest, error) {
	expectedStatus := update.ExpectedStatus
	if expectedStatus == "" {
		expectedStatus = "pending"
	}
	for index := range r.approvalRequests {
		if r.approvalRequests[index].ID != requestID {
			continue
		}
		if r.approvalRequests[index].Status != expectedStatus {
			return domainaigateway.ApprovalRequest{}, nil
		}
		item := r.approvalRequests[index]
		item.Status = update.Status
		item.Summary = update.Summary
		item.RelatedIDs = update.RelatedIDs
		item.Output = update.Output
		item.DecidedBy = update.DecidedBy
		item.DecidedByName = update.DecidedByName
		item.DecidedAt = update.DecidedAt
		item.DecisionComment = update.DecisionComment
		item.UpdatedAt = update.UpdatedAt
		r.approvalRequests[index] = item
		return item, nil
	}
	return domainaigateway.ApprovalRequest{}, nil
}

func (r *memoryGatewayRepository) ExpirePendingApprovalRequests(_ context.Context, at time.Time) ([]domainaigateway.ApprovalRequest, error) {
	expired := make([]domainaigateway.ApprovalRequest, 0)
	for index := range r.approvalRequests {
		item := r.approvalRequests[index]
		if item.Status != "pending" || item.ExpiresAt == nil || item.ExpiresAt.After(at) {
			continue
		}
		item.Status = "timeout"
		item.Summary = "AI Gateway approval request timed out"
		item.UpdatedAt = at
		r.approvalRequests[index] = item
		expired = append(expired, item)
	}
	return expired, nil
}

func (r *memoryGatewayRepository) ListAccessPolicies(_ context.Context, filter domainaigateway.AccessPolicyFilter) ([]domainaigateway.AccessPolicy, error) {
	items := make([]domainaigateway.AccessPolicy, 0)
	for _, item := range r.accessPolicies {
		if filter.SubjectType != "" && item.SubjectType != filter.SubjectType {
			continue
		}
		if filter.SubjectID != "" && item.SubjectID != filter.SubjectID {
			continue
		}
		if filter.AIClientID != "" && item.AIClientID != filter.AIClientID {
			continue
		}
		if filter.Effect != "" && item.Effect != filter.Effect {
			continue
		}
		if !filter.IncludeDisabled && !item.Enabled {
			continue
		}
		items = append(items, item)
	}
	return items, nil
}

func (r *memoryGatewayRepository) CreateAccessPolicy(_ context.Context, item domainaigateway.AccessPolicy) (domainaigateway.AccessPolicy, error) {
	r.accessPolicies = append(r.accessPolicies, item)
	return item, nil
}

func (r *memoryGatewayRepository) UpdateAccessPolicy(_ context.Context, item domainaigateway.AccessPolicy) (domainaigateway.AccessPolicy, error) {
	for index := range r.accessPolicies {
		if r.accessPolicies[index].ID == item.ID {
			r.accessPolicies[index] = item
			return item, nil
		}
	}
	r.accessPolicies = append(r.accessPolicies, item)
	return item, nil
}

func (r *memoryGatewayRepository) DeleteAccessPolicy(_ context.Context, policyID string) error {
	for index := range r.accessPolicies {
		if r.accessPolicies[index].ID == policyID {
			r.accessPolicies = append(r.accessPolicies[:index], r.accessPolicies[index+1:]...)
			return nil
		}
	}
	return nil
}

func (r *memoryGatewayRepository) ListActiveAccessPolicies(_ context.Context, subjectType, subjectID, aiClientID string) ([]domainaigateway.AccessPolicy, error) {
	items := make([]domainaigateway.AccessPolicy, 0)
	for _, item := range r.accessPolicies {
		if !item.Enabled || item.SubjectType != subjectType || item.SubjectID != subjectID {
			continue
		}
		if item.AIClientID != "" && item.AIClientID != aiClientID {
			continue
		}
		items = append(items, item)
	}
	return items, nil
}

func (r *memoryGatewayRepository) ListSkillBindings(_ context.Context, filter domainaigateway.SkillBindingFilter) ([]domainaigateway.SkillBinding, error) {
	items := make([]domainaigateway.SkillBinding, 0)
	for _, item := range r.skillBindings {
		if filter.SubjectType != "" && item.SubjectType != filter.SubjectType {
			continue
		}
		if filter.SubjectID != "" && item.SubjectID != filter.SubjectID {
			continue
		}
		if filter.AIClientID != "" && item.AIClientID != filter.AIClientID {
			continue
		}
		if filter.SkillID != "" && item.SkillID != filter.SkillID {
			continue
		}
		if !filter.IncludeDisabled && !item.Enabled {
			continue
		}
		items = append(items, item)
	}
	return items, nil
}

func (r *memoryGatewayRepository) CreateSkillBinding(_ context.Context, item domainaigateway.SkillBinding) (domainaigateway.SkillBinding, error) {
	r.skillBindings = append(r.skillBindings, item)
	return item, nil
}

func (r *memoryGatewayRepository) UpdateSkillBinding(_ context.Context, item domainaigateway.SkillBinding) (domainaigateway.SkillBinding, error) {
	for index := range r.skillBindings {
		if r.skillBindings[index].ID == item.ID {
			r.skillBindings[index] = item
			return item, nil
		}
	}
	r.skillBindings = append(r.skillBindings, item)
	return item, nil
}

func (r *memoryGatewayRepository) DeleteSkillBinding(_ context.Context, bindingID string) error {
	for index := range r.skillBindings {
		if r.skillBindings[index].ID == bindingID {
			r.skillBindings = append(r.skillBindings[:index], r.skillBindings[index+1:]...)
			return nil
		}
	}
	return nil
}

func (r *memoryGatewayRepository) ListActiveSkillBindings(_ context.Context, subjectType, subjectID, aiClientID string) ([]domainaigateway.SkillBinding, error) {
	items := make([]domainaigateway.SkillBinding, 0)
	for _, item := range r.skillBindings {
		if !item.Enabled || item.SubjectType != subjectType || item.SubjectID != subjectID {
			continue
		}
		if item.AIClientID != "" && item.AIClientID != aiClientID {
			continue
		}
		items = append(items, item)
	}
	return items, nil
}

type fakeApplicationService struct {
	listed         bool
	created        bool
	servicesListed bool
	lastFilter     domainapp.Filter
	lastCreate     domainapp.UpsertInput
}

func (s *fakeApplicationService) List(_ context.Context, _ domainidentity.Principal, filter domainapp.Filter) ([]domainapp.App, error) {
	s.listed = true
	s.lastFilter = filter
	return []domainapp.App{{ID: "app-1", Name: "Billing", Key: "billing", BusinessLineID: "core"}}, nil
}

func (s *fakeApplicationService) Create(_ context.Context, _ domainidentity.Principal, input domainapp.UpsertInput) (domainapp.App, error) {
	s.created = true
	s.lastCreate = input
	return domainapp.App{ID: firstNonEmpty(input.ID, "app-created"), Name: input.Name, Key: input.Key, Metadata: input.Metadata}, nil
}

func (s *fakeApplicationService) ListServices(_ context.Context, _ domainidentity.Principal, applicationID string) ([]domainapp.Service, error) {
	s.servicesListed = true
	return []domainapp.Service{
		{
			ID:            "svc-1",
			ApplicationID: applicationID,
			Key:           "api",
			Name:          "API",
			ServiceKind:   domainapp.ServiceKindKubernetesWorkload,
			BuildSourceID: "src-1",
			Enabled:       true,
			Metadata:      map[string]any{"token": "secret-token"},
			Containers: []domainapp.ServiceContainer{
				{ID: "ctr-1", Name: "api", ImageRepository: "registry.example.com/api", RuntimePorts: []int{8080}, EnvSchema: map[string]any{"password": "secret"}},
			},
		},
	}, nil
}

type fakeDeliveryService struct {
	triggered       bool
	lastActionInput domaindelivery.ApplicationDeliveryActionInput
	workflowRunID   string
}

type fakeOnCallResolver struct {
	current    map[string]any
	route      map[string]any
	currentErr error
	routeErr   error
	lastRef    string
	lastInput  domainalert.OnCallResolveInput
}

func (r *fakeOnCallResolver) GetCurrentOnCall(_ context.Context, _ domainidentity.Principal, ref string) (map[string]any, error) {
	r.lastRef = ref
	if r.currentErr != nil {
		return nil, r.currentErr
	}
	return r.current, nil
}

func (r *fakeOnCallResolver) ResolveOnCall(_ context.Context, _ domainidentity.Principal, input domainalert.OnCallResolveInput) (map[string]any, error) {
	r.lastInput = input
	if r.routeErr != nil {
		return nil, r.routeErr
	}
	return r.route, nil
}

func (s *fakeDeliveryService) GetApplicationDetail(context.Context, domainidentity.Principal, string) (domaindelivery.ApplicationDetail, error) {
	return domaindelivery.ApplicationDetail{
		Application: domainapp.App{
			ID:       "app-1",
			Name:     "Billing",
			Key:      "billing",
			Metadata: map[string]any{"providerUsage": map[string]any{"inputTokens": 20, "outputTokens": 30, "totalCost": 0.08, "rawOutput": "do-not-store"}},
			BuildSources: []domainapp.BuildSource{
				{ID: "src-1", Name: "Dockerfile", Type: domainapp.BuildSourceTypeRepoDockerfile, Enabled: true, IsDefault: true, Config: map[string]any{"token": "secret-token"}},
			},
		},
		Bindings: []domaindelivery.ApplicationBindingSummary{
			{
				ApplicationEnvironmentID: "binding-1",
				EnvironmentID:            "env-1",
				EnvironmentName:          "Prod",
				EnvironmentKey:           "prod",
				RequiresApproval:         true,
				BuildSourceID:            "src-1",
				TargetCount:              1,
				Targets: []domaincatalog.ReleaseTarget{
					{ID: "target-1", ApplicationEnvironmentID: "binding-1", ClusterID: "cluster-a", Namespace: "prod", WorkloadKind: "Deployment", WorkloadName: "api", Enabled: true},
				},
				LatestBundle:        &domaindelivery.ReleaseBundle{ID: "bundle-0", ApplicationID: "app-1", ApplicationEnvironmentID: "binding-1", Version: "v0", Status: "ready", ArtifactRef: "image:v0"},
				LatestExecutionTask: &domaindelivery.ExecutionTask{ID: "task-0", ApplicationID: "app-1", ApplicationEnvironmentID: "binding-1", Status: "completed"},
			},
		},
		LatestBundle:        &domaindelivery.ReleaseBundle{ID: "bundle-0", ApplicationID: "app-1", ApplicationEnvironmentID: "binding-1", Version: "v0", Status: "ready", ArtifactRef: "image:v0"},
		LatestExecutionTask: &domaindelivery.ExecutionTask{ID: "task-0", ApplicationID: "app-1", ApplicationEnvironmentID: "binding-1", Status: "completed"},
	}, nil
}

func (s *fakeDeliveryService) GetApplicationEnvironmentDetail(context.Context, domainidentity.Principal, string) (domaindelivery.ApplicationEnvironmentDetail, error) {
	return domaindelivery.ApplicationEnvironmentDetail{
		Binding: domaincatalog.ApplicationEnvironment{
			ID:            "binding-1",
			ApplicationID: "app-1",
			EnvironmentID: "env-1",
			Targets: []domaincatalog.ReleaseTarget{
				{ID: "target-1", ApplicationEnvironmentID: "binding-1", ClusterID: "cluster-a", Namespace: "prod", WorkloadKind: "Deployment", WorkloadName: "api", Enabled: true},
			},
		},
		Application: domainapp.App{ID: "app-1", Name: "Billing", Key: "billing"},
	}, nil
}

func (s *fakeDeliveryService) TriggerApplicationDeliveryAction(_ context.Context, _ domainidentity.Principal, applicationID string, input domaindelivery.ApplicationDeliveryActionInput) (domaindelivery.ApplicationDeliveryActionResult, error) {
	s.triggered = true
	s.lastActionInput = input
	relatedIDs := domaindelivery.ApplicationDeliveryActionRelatedIDs{
		ReleaseBundleID: "bundle-1",
		ExecutionTaskID: "task-1",
	}
	if strings.TrimSpace(s.workflowRunID) != "" {
		relatedIDs.WorkflowRunID = strings.TrimSpace(s.workflowRunID)
	}
	return domaindelivery.ApplicationDeliveryActionResult{
		Action:                   input.Action,
		ApplicationID:            applicationID,
		ApplicationEnvironmentID: input.ApplicationEnvironmentID,
		RelatedIDs:               relatedIDs,
	}, nil
}

func (s *fakeDeliveryService) ListReleaseBundles(context.Context, domainidentity.Principal, domaindelivery.ReleaseBundleFilter) ([]domaindelivery.ReleaseBundle, error) {
	return []domaindelivery.ReleaseBundle{
		{ID: "bundle-1", ApplicationID: "app-1", ApplicationEnvironmentID: "binding-1", Version: "v1", Status: "failed", ArtifactRef: "image:v1", ArtifactDigest: "sha256:new"},
		{ID: "bundle-0", ApplicationID: "app-1", ApplicationEnvironmentID: "binding-1", Version: "v0", Status: "ready", ArtifactRef: "image:v0", ArtifactDigest: "sha256:old"},
	}, nil
}

func (s *fakeDeliveryService) GetReleaseBundle(_ context.Context, _ domainidentity.Principal, bundleID string) (domaindelivery.ReleaseBundle, error) {
	return domaindelivery.ReleaseBundle{ID: bundleID, ApplicationID: "app-1", ApplicationEnvironmentID: "binding-1", Version: bundleID, Status: "ready"}, nil
}

func (s *fakeDeliveryService) ListReleaseBundleArtifacts(context.Context, domainidentity.Principal, string) ([]domaindelivery.ExecutionArtifact, error) {
	return []domaindelivery.ExecutionArtifact{{ID: "artifact-1"}}, nil
}

func (s *fakeDeliveryService) ListExecutionTasks(context.Context, domainidentity.Principal, domaindelivery.ExecutionTaskFilter) ([]domaindelivery.ExecutionTask, error) {
	return []domaindelivery.ExecutionTask{{ID: "task-1", ApplicationID: "app-1", ApplicationEnvironmentID: "binding-1", ReleaseBundleID: "bundle-1", Status: "failed"}}, nil
}

func (s *fakeDeliveryService) GetExecutionTask(_ context.Context, _ domainidentity.Principal, taskID string) (domaindelivery.ExecutionTask, error) {
	return domaindelivery.ExecutionTask{ID: taskID, ApplicationID: "app-1", ApplicationEnvironmentID: "binding-1", ReleaseBundleID: "bundle-1", Status: "failed"}, nil
}

func (s *fakeDeliveryService) ListExecutionLogs(context.Context, domainidentity.Principal, string, int) ([]domaindelivery.ExecutionLog, error) {
	return []domaindelivery.ExecutionLog{{ID: "log-1", Message: "build failed token=secret-token", Metadata: map[string]any{"password": "secret"}}}, nil
}

type fakeCatalogService struct {
	listedWorkflowTemplates bool
}

func (s *fakeCatalogService) ListWorkflowTemplates(context.Context, domainidentity.Principal) ([]domaincatalog.WorkflowTemplate, error) {
	s.listedWorkflowTemplates = true
	return []domaincatalog.WorkflowTemplate{{ID: "wf-1", Key: "release", Name: "Release DAG", Enabled: true, Definition: map[string]any{"mode": "release_dag"}}}, nil
}

type fakeResourceService struct {
	listedPods    bool
	readLogs      bool
	listedEvents  bool
	tailLines     int64
	eventLimit    int
	clusterID     string
	namespace     string
	podName       string
	deployments   []domainresource.DeploymentView
	services      []domainresource.ServiceView
	pods          []domainresource.PodView
	ingresses     []domainresource.IngressView
	clusterEvents []domainresource.ClusterEventView
}

type fakeAnalysisArtifactRecorder struct {
	input       domaincopilot.GatewayAnalysisArtifactInput
	queuedInput domaincopilot.GatewayAnalysisAgentRunInput
	run         domaincopilot.AgentRun
	queuedRun   domaincopilot.AgentRun
}

func (r *fakeAnalysisArtifactRecorder) RecordGatewayAnalysisArtifact(_ context.Context, _ domainidentity.Principal, input domaincopilot.GatewayAnalysisArtifactInput) (domaincopilot.AgentRun, error) {
	r.input = input
	if r.run.ID != "" {
		return r.run, nil
	}
	return domaincopilot.AgentRun{
		ID:           "agent:gateway-1",
		ProviderID:   "internal",
		ProviderKind: "internal",
		CapabilityID: input.CapabilityID,
		Status:       domaincopilot.AgentRunStatusCompleted,
		AnalysisArtifacts: []domaincopilot.AnalysisArtifact{{
			Kind:    input.CapabilityID,
			RunID:   "agent:gateway-1",
			Title:   input.Title,
			Summary: input.Summary,
		}},
	}, nil
}

func (r *fakeAnalysisArtifactRecorder) QueueGatewayAnalysisAgentRun(_ context.Context, _ domainidentity.Principal, input domaincopilot.GatewayAnalysisAgentRunInput) (domaincopilot.AgentRun, error) {
	r.queuedInput = input
	if r.queuedRun.ID != "" {
		return r.queuedRun, nil
	}
	return domaincopilot.AgentRun{
		ID:           "agent:queued-1",
		ProviderID:   firstNonEmpty(input.AgentProviderID, "hermes"),
		ProviderKind: firstNonEmpty(input.AgentProviderID, "hermes"),
		CapabilityID: firstNonEmpty(input.CapabilityID, "delivery_failure"),
		Status:       domaincopilot.AgentRunStatusQueued,
	}, nil
}

func (s *fakeResourceService) ListPods(_ context.Context, _ domainidentity.Principal, clusterID, namespace string) ([]domainresource.PodView, error) {
	s.listedPods = true
	s.clusterID = clusterID
	s.namespace = namespace
	if s.pods != nil {
		return s.pods, nil
	}
	return []domainresource.PodView{{Name: "api-7d9f", Namespace: namespace, Phase: "CrashLoopBackOff", Restarts: 4}}, nil
}

func (s *fakeResourceService) GetPodDetail(_ context.Context, _ domainidentity.Principal, clusterID, namespace, name string) (domainresource.PodDetailView, error) {
	s.clusterID = clusterID
	s.namespace = namespace
	s.podName = name
	return domainresource.PodDetailView{
		Name:      name,
		Namespace: namespace,
		Phase:     "Running",
		NodeName:  "node-a",
		Containers: []domainresource.WorkloadContainerView{
			{Name: "api", Image: "registry.example.com/api:v1", Ready: true, RestartCount: 1, State: "running"},
		},
		Conditions: []domainresource.WorkloadConditionView{{Type: "Ready", Status: "True"}},
		RelatedResources: []domainresource.PodRelatedResourceView{
			{Kind: "Service", Namespace: namespace, Name: "api", Relations: []string{"selected-by-service"}},
		},
	}, nil
}

func (s *fakeResourceService) GetPodLogs(_ context.Context, _ domainidentity.Principal, clusterID, namespace, name, container string, tailLines, sinceSeconds int64, previous bool) (domainresource.PodLogsView, error) {
	s.readLogs = true
	s.clusterID = clusterID
	s.namespace = namespace
	s.podName = name
	s.tailLines = tailLines
	return domainresource.PodLogsView{PodName: name, Namespace: namespace, Container: container, Content: "startup failed password=supersecret", ContentBytes: 35, TailLines: tailLines}, nil
}

func (s *fakeResourceService) ListDeployments(_ context.Context, _ domainidentity.Principal, clusterID, namespace string) ([]domainresource.DeploymentView, error) {
	s.clusterID = clusterID
	s.namespace = namespace
	if s.deployments != nil {
		return s.deployments, nil
	}
	return []domainresource.DeploymentView{{Name: "api", Namespace: namespace, DesiredReplicas: 2, ReadyReplicas: 1}}, nil
}

func (s *fakeResourceService) GetDeploymentRolloutStatus(_ context.Context, _ domainidentity.Principal, clusterID, namespace, name string) (domainresource.DeploymentRolloutStatusView, error) {
	s.clusterID = clusterID
	s.namespace = namespace
	return domainresource.DeploymentRolloutStatusView{Name: name, Namespace: namespace, Revision: "3", Status: "progressing", DesiredReplicas: 2, ReadyReplicas: 1}, nil
}

func (s *fakeResourceService) ListServices(_ context.Context, _ domainidentity.Principal, clusterID, namespace string) ([]domainresource.ServiceView, error) {
	s.clusterID = clusterID
	s.namespace = namespace
	if s.services != nil {
		return s.services, nil
	}
	return []domainresource.ServiceView{{Name: "api", Namespace: namespace, Type: "ClusterIP", Selector: map[string]string{"app": "api"}}}, nil
}

func (s *fakeResourceService) ListIngresses(_ context.Context, _ domainidentity.Principal, clusterID, namespace string) ([]domainresource.IngressView, error) {
	s.clusterID = clusterID
	s.namespace = namespace
	if s.ingresses != nil {
		return s.ingresses, nil
	}
	return []domainresource.IngressView{{Name: "api", Namespace: namespace, Hosts: []string{"api.example.com"}, BackendServices: []string{"api"}}}, nil
}

func (s *fakeResourceService) ListGatewayClasses(context.Context, domainidentity.Principal, string) ([]domainresource.GatewayClassView, error) {
	return []domainresource.GatewayClassView{{Name: "public", ControllerName: "example.com/gateway-controller", Accepted: "True"}}, nil
}

func (s *fakeResourceService) ListGateways(_ context.Context, _ domainidentity.Principal, _ string, namespace string) ([]domainresource.GatewayView, error) {
	return []domainresource.GatewayView{{Name: "edge", Namespace: namespace, GatewayClass: "public", ListenerCount: 1}}, nil
}

func (s *fakeResourceService) ListHTTPRoutes(_ context.Context, _ domainidentity.Principal, _ string, namespace string) ([]domainresource.HTTPRouteView, error) {
	return []domainresource.HTTPRouteView{{Name: "api", Namespace: namespace, Hostnames: []string{"api.example.com"}, BackendServices: []string{"api"}}}, nil
}

func (s *fakeResourceService) ListBackendTLSPolicies(_ context.Context, _ domainidentity.Principal, _ string, namespace string) ([]domainresource.BackendTLSPolicyView, error) {
	return []domainresource.BackendTLSPolicyView{{Name: "api-tls", Namespace: namespace, TargetRefs: []string{"Service/api"}}}, nil
}

func (s *fakeResourceService) ListGRPCRoutes(_ context.Context, _ domainidentity.Principal, _ string, namespace string) ([]domainresource.GRPCRouteView, error) {
	return []domainresource.GRPCRouteView{{Name: "api-grpc", Namespace: namespace, BackendServices: []string{"api"}}}, nil
}

func (s *fakeResourceService) ListReferenceGrants(_ context.Context, _ domainidentity.Principal, _ string, namespace string) ([]domainresource.ReferenceGrantView, error) {
	return []domainresource.ReferenceGrantView{{Name: "allow-api", Namespace: namespace}}, nil
}

func (s *fakeResourceService) ListPersistentVolumeClaims(_ context.Context, _ domainidentity.Principal, _ string, namespace string) ([]domainresource.PersistentVolumeClaimView, error) {
	return []domainresource.PersistentVolumeClaimView{{Name: "data", Namespace: namespace, Status: "Pending", VolumeName: "pv-data", StorageClass: "fast"}}, nil
}

func (s *fakeResourceService) ListPersistentVolumes(context.Context, domainidentity.Principal, string) ([]domainresource.PersistentVolumeView, error) {
	return []domainresource.PersistentVolumeView{{Name: "pv-data", Status: "Available", StorageClass: "fast", Capacity: "10Gi"}}, nil
}

func (s *fakeResourceService) ListStorageClasses(context.Context, domainidentity.Principal, string) ([]domainresource.StorageClassView, error) {
	return []domainresource.StorageClassView{{Name: "fast", Provisioner: "example.com/csi", ReclaimPolicy: "Delete"}}, nil
}

func (s *fakeResourceService) GetNodeDetail(_ context.Context, _ domainidentity.Principal, clusterID, nodeName string) (domainresource.NodeDetailView, error) {
	s.clusterID = clusterID
	return domainresource.NodeDetailView{
		Name:       nodeName,
		Status:     "Ready",
		Version:    "v1.30.0",
		PodCount:   1,
		Conditions: []domainresource.WorkloadConditionView{{Type: "Ready", Status: "True"}},
		Pods:       []domainresource.NodePodView{{Name: "api-7d9f", Namespace: "prod", Phase: "Running"}},
	}, nil
}

func (s *fakeResourceService) ListClusterEvents(_ context.Context, _ domainidentity.Principal, clusterID, namespace string, limit int) ([]domainresource.ClusterEventView, error) {
	s.listedEvents = true
	s.clusterID = clusterID
	s.namespace = namespace
	s.eventLimit = limit
	if s.clusterEvents != nil {
		return s.clusterEvents, nil
	}
	return []domainresource.ClusterEventView{{Name: "event-1", Namespace: namespace, Type: "Warning", Reason: "BackOff", InvolvedKind: "Pod", InvolvedName: "api-7d9f", Message: "Back-off restarting container"}}, nil
}

func TestCapabilitiesRequiresAIGatewayView(t *testing.T) {
	audit := &captureAuditRecorder{}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {appaccess.PermDeliveryApplicationsView},
		},
	}), audit)

	_, err := service.Capabilities(context.Background(), testPrincipal("developer"), domainaigateway.ManifestRequest{})
	if err == nil {
		t.Fatalf("Capabilities should reject callers without %s", appaccess.PermAIGatewayView)
	}
	if len(audit.entries) != 1 || audit.entries[0].Result != "deny" {
		t.Fatalf("expected deny audit entry, got %#v", audit.entries)
	}
}

func TestCapabilitiesFiltersToolsByBusinessPermissions(t *testing.T) {
	audit := &captureAuditRecorder{}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayView,
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsView,
			},
		},
	}), audit)

	manifest, err := service.Capabilities(context.Background(), testPrincipal("developer"), domainaigateway.ManifestRequest{AIClientName: "codex"})
	if err != nil {
		t.Fatalf("Capabilities returned error: %v", err)
	}
	if !hasTool(manifest.Tools, "delivery.applications.list") {
		t.Fatalf("expected application list tool, got %#v", manifest.Tools)
	}
	if hasTool(manifest.Tools, "delivery.actions.trigger") {
		t.Fatalf("did not expect trigger tool without delivery trigger permissions")
	}
	if manifest.Summary.DeniedCount == 0 {
		t.Fatalf("expected denied count for filtered tools")
	}
	if len(audit.entries) != 1 || audit.entries[0].Result != "success" {
		t.Fatalf("expected success audit entry, got %#v", audit.entries)
	}
}

func TestCapabilitiesUsesInjectedCapabilityProvider(t *testing.T) {
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {appaccess.PermAIGatewayView, appaccess.PermAIGatewayInvoke},
		},
	}), nil)
	service.SetCapabilityProviders(testCapabilityProvider{
		tools: []domainaigateway.ToolCapability{
			{
				Name:           "custom.echo",
				Title:          "Custom Echo",
				Description:    "Custom provider echo tool.",
				Domain:         "custom",
				Action:         "read",
				RiskLevel:      domainaigateway.RiskLevelRead,
				PermissionKeys: []string{appaccess.PermAIGatewayInvoke},
				InputSchema:    map[string]any{"type": "object"},
			},
		},
		resources: []domainaigateway.ResourceCapability{
			{
				Name:           "soha://custom/context",
				Description:    "Custom provider context resource.",
				PermissionKeys: []string{appaccess.PermAIGatewayInvoke},
			},
		},
		prompts: []domainaigateway.PromptCapability{
			{
				Name:           "custom.prompt",
				Description:    "Custom provider prompt.",
				PermissionKeys: []string{appaccess.PermAIGatewayInvoke},
			},
		},
		skills: []domainaigateway.SkillCapability{
			{
				ID:             "custom-skill",
				Name:           "Custom Skill",
				Category:       "custom",
				Description:    "Custom provider skill.",
				CapabilityRefs: []string{"custom.echo"},
				PermissionKeys: []string{appaccess.PermAIGatewayInvoke},
			},
		},
		resourceRefs: []ResourceCapabilityRefs{
			{
				Resource: "soha://custom/context",
				Tools:    []string{"custom.echo"},
				Prompts:  []string{"custom.prompt"},
				Skills:   []string{"custom-skill"},
			},
		},
	})

	manifest, err := service.Capabilities(context.Background(), testPrincipal("developer"), domainaigateway.ManifestRequest{})
	if err != nil {
		t.Fatalf("Capabilities returned error: %v", err)
	}
	if !hasTool(manifest.Tools, "custom.echo") || hasTool(manifest.Tools, "delivery.applications.list") {
		t.Fatalf("expected injected tools only, got %#v", manifest.Tools)
	}
	if !hasResource(manifest.Resources, "soha://custom/context") || !hasPrompt(manifest.Prompts, "custom.prompt") || !hasSkill(manifest.Skills, "custom-skill") {
		t.Fatalf("expected injected resources/prompts/skills, got %#v %#v %#v", manifest.Resources, manifest.Prompts, manifest.Skills)
	}
	if manifest.Summary.ToolCount != 1 || manifest.Summary.ResourceCount != 1 || manifest.Summary.PromptCount != 1 || manifest.Summary.SkillCount != 1 {
		t.Fatalf("unexpected manifest summary for injected provider: %#v", manifest.Summary)
	}
}

func TestCapabilitiesAddProviderPreservesDefaultCapabilities(t *testing.T) {
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayView,
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsView,
			},
		},
	}), nil)
	service.AddCapabilityProviders(testCapabilityProvider{
		tools: []domainaigateway.ToolCapability{
			{
				Name:           "custom.echo",
				Title:          "Custom Echo",
				Description:    "Custom provider echo tool.",
				Domain:         "custom",
				Action:         "read",
				RiskLevel:      domainaigateway.RiskLevelRead,
				PermissionKeys: []string{appaccess.PermAIGatewayInvoke},
				InputSchema:    map[string]any{"type": "object"},
			},
		},
	})

	manifest, err := service.Capabilities(context.Background(), testPrincipal("developer"), domainaigateway.ManifestRequest{})
	if err != nil {
		t.Fatalf("Capabilities returned error: %v", err)
	}
	if !hasTool(manifest.Tools, "custom.echo") {
		t.Fatalf("expected added custom tool, got %#v", manifest.Tools)
	}
	if !hasTool(manifest.Tools, "delivery.applications.list") {
		t.Fatalf("expected default tools to remain, got %#v", manifest.Tools)
	}
}

func TestCapabilitiesExposeExecuteToolWithTriggerPermissions(t *testing.T) {
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayView,
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryBuildsTrigger,
				appaccess.PermDeliveryReleasesTrigger,
			},
		},
	}), nil)

	manifest, err := service.Capabilities(context.Background(), testPrincipal("developer"), domainaigateway.ManifestRequest{})
	if err != nil {
		t.Fatalf("Capabilities returned error: %v", err)
	}
	if !hasTool(manifest.Tools, "delivery.actions.trigger") {
		t.Fatalf("expected trigger tool with delivery trigger permissions, got %#v", manifest.Tools)
	}
}

func TestCapabilitiesExposeSecurityChangeSkillWithGatewayInvoke(t *testing.T) {
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayView,
				appaccess.PermAIGatewayInvoke,
			},
		},
	}), nil)

	manifest, err := service.Capabilities(context.Background(), testPrincipal("developer"), domainaigateway.ManifestRequest{})
	if err != nil {
		t.Fatalf("Capabilities returned error: %v", err)
	}
	if !hasSkill(manifest.Skills, "security-change") {
		t.Fatalf("expected security-change skill with gateway invoke permission, got %#v", manifest.Skills)
	}
}

func TestCapabilitiesDeliveryDeveloperSkillIncludesDeliveryContextTools(t *testing.T) {
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayView,
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsView,
			},
		},
	}), nil)

	manifest, err := service.Capabilities(context.Background(), testPrincipal("developer"), domainaigateway.ManifestRequest{})
	if err != nil {
		t.Fatalf("Capabilities returned error: %v", err)
	}
	var skill domainaigateway.SkillCapability
	for _, item := range manifest.Skills {
		if item.ID == "delivery-developer" {
			skill = item
			break
		}
	}
	if skill.ID == "" {
		t.Fatalf("expected delivery-developer skill, got %#v", manifest.Skills)
	}
	for _, want := range []string{
		"delivery.applications.create",
		"delivery.application_environments.list",
		"delivery.release_targets.list",
		"delivery.release_bundles.list",
		"delivery.execution_tasks.list",
		"delivery.execution_logs.list",
		"delivery.release_context.diff",
		"delivery.rollback.context",
		"delivery.actions.trigger",
	} {
		if !slices.Contains(skill.CapabilityRefs, want) {
			t.Fatalf("delivery-developer capabilityRefs missing %s: %#v", want, skill.CapabilityRefs)
		}
	}
}

func TestDefaultToolMCPNamesMatchCanonicalNames(t *testing.T) {
	for _, tool := range defaultTools() {
		if tool.MCPToolName == "" {
			continue
		}
		if tool.MCPToolName != tool.Name {
			t.Fatalf("tool %s has drifting MCPToolName %s", tool.Name, tool.MCPToolName)
		}
	}
}

func TestDefaultToolInputSchemasCoverHighFrequencyMCPTools(t *testing.T) {
	for _, item := range []struct {
		tool     string
		required []string
	}{
		{tool: "delivery.applications.list"},
		{tool: "delivery.applications.detail", required: []string{"applicationId"}},
		{tool: "delivery.applications.create", required: []string{"name", "key"}},
		{tool: "delivery.application_environments.list"},
		{tool: "delivery.application_services.list", required: []string{"applicationId"}},
		{tool: "delivery.build_sources.list", required: []string{"applicationId"}},
		{tool: "delivery.release_targets.list"},
		{tool: "delivery.actions.trigger", required: []string{"applicationId", "action"}},
		{tool: "delivery.release_bundles.list"},
		{tool: "delivery.execution_tasks.list"},
		{tool: "delivery.execution_logs.list", required: []string{"taskId"}},
		{tool: "delivery.workflow_templates.list"},
		{tool: "delivery.onboarding.analyze_repo", required: []string{"repositoryPath"}},
		{tool: "delivery.standards.dockerfile.generate", required: []string{"language"}},
		{tool: "delivery.standards.dockerfile.validate", required: []string{"content"}},
		{tool: "delivery.standards.helm.generate", required: []string{"serviceName", "imageRepository"}},
		{tool: "delivery.standards.k8s.validate", required: []string{"manifests"}},
		{tool: "delivery.spec.render", required: []string{"applicationDraft"}},
		{tool: "delivery.application.bootstrap"},
		{tool: "delivery.release.plan", required: []string{"applicationId", "applicationEnvironmentId", "action"}},
		{tool: "delivery.release_context.diff", required: []string{"applicationId"}},
		{tool: "delivery.rollback.context", required: []string{"applicationId"}},
		{tool: "k8s.pods.list", required: []string{"clusterId"}},
		{tool: "k8s.pods.logs", required: []string{"clusterId", "namespace", "podName"}},
		{tool: "k8s.pods.describe", required: []string{"clusterId", "namespace", "podName"}},
		{tool: "k8s.deployments.list", required: []string{"clusterId"}},
		{tool: "k8s.deployments.rollout_status", required: []string{"clusterId", "namespace", "deploymentName"}},
		{tool: "k8s.deployments.events", required: []string{"clusterId", "namespace", "deploymentName"}},
		{tool: "k8s.services.list", required: []string{"clusterId"}},
		{tool: "k8s.services.backends", required: []string{"clusterId", "namespace", "serviceName"}},
		{tool: "k8s.routes.context", required: []string{"clusterId"}},
		{tool: "k8s.storage.context", required: []string{"clusterId"}},
		{tool: "k8s.nodes.detail", required: []string{"clusterId", "nodeName"}},
		{tool: "k8s.events.list", required: []string{"clusterId"}},
		{tool: "diagnosis.release_failure.analyze", required: []string{"applicationId"}},
	} {
		tool, ok := toolByName(item.tool)
		if !ok {
			t.Fatalf("expected tool %s", item.tool)
		}
		if tool.InputSchema["type"] != "object" {
			t.Fatalf("tool %s missing object input schema: %#v", item.tool, tool.InputSchema)
		}
		properties, ok := tool.InputSchema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("tool %s missing input schema properties: %#v", item.tool, tool.InputSchema)
		}
		required := anyStringSet(tool.InputSchema["required"])
		for _, field := range item.required {
			if _, ok := properties[field]; !ok {
				t.Fatalf("tool %s schema missing property %s: %#v", item.tool, field, tool.InputSchema)
			}
			if !required[field] {
				t.Fatalf("tool %s schema missing required field %s: %#v", item.tool, field, tool.InputSchema)
			}
		}
	}
	for _, tool := range defaultTools() {
		if tool.InputSchema["type"] != "object" {
			t.Fatalf("default tool %s must expose object input schema: %#v", tool.Name, tool.InputSchema)
		}
		if _, ok := tool.InputSchema["properties"].(map[string]any); !ok {
			t.Fatalf("default tool %s must expose input schema properties: %#v", tool.Name, tool.InputSchema)
		}
	}
}

func TestCapabilitiesFiltersToolsByToolGrantAllowList(t *testing.T) {
	repo := &memoryGatewayRepository{
		toolGrants: []domainaigateway.ToolGrant{
			{
				ID:          "grant-1",
				SubjectType: "user",
				SubjectID:   "user-1",
				AIClientID:  "codex",
				ToolName:    "delivery.applications.list",
				Effect:      "allow",
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayView,
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsView,
				appaccess.PermDeliveryBuildsTrigger,
				appaccess.PermDeliveryReleasesTrigger,
			},
		},
	}), nil, repo)

	manifest, err := service.Capabilities(context.Background(), testPrincipal("developer"), domainaigateway.ManifestRequest{AIClientID: "codex"})
	if err != nil {
		t.Fatalf("Capabilities returned error: %v", err)
	}
	if !hasTool(manifest.Tools, "delivery.applications.list") {
		t.Fatalf("expected granted application list tool, got %#v", manifest.Tools)
	}
	if hasTool(manifest.Tools, "delivery.actions.trigger") {
		t.Fatalf("did not expect trigger tool outside allow-list grants")
	}
}

func TestCapabilitiesUsesRoleAndAIClientToolGrants(t *testing.T) {
	repo := &memoryGatewayRepository{
		toolGrants: []domainaigateway.ToolGrant{
			{
				ID:          "role-allow",
				SubjectType: "role",
				SubjectID:   "developer",
				ToolName:    "delivery.*",
				Effect:      "allow",
			},
			{
				ID:          "client-deny",
				SubjectType: "ai_client",
				SubjectID:   "codex",
				ToolName:    "delivery.actions.trigger",
				Effect:      "deny",
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayView,
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsView,
				appaccess.PermDeliveryBuildsTrigger,
				appaccess.PermDeliveryReleasesTrigger,
			},
		},
	}), nil, repo)

	manifest, err := service.Capabilities(context.Background(), testPrincipal("developer"), domainaigateway.ManifestRequest{AIClientID: "codex"})
	if err != nil {
		t.Fatalf("Capabilities returned error: %v", err)
	}
	if !hasTool(manifest.Tools, "delivery.applications.list") {
		t.Fatalf("expected role grant to keep delivery list tool, got %#v", manifest.Tools)
	}
	if hasTool(manifest.Tools, "delivery.actions.trigger") {
		t.Fatalf("expected AI client deny grant to hide trigger tool")
	}
}

func TestCapabilitiesUsesOrganizationToolGrants(t *testing.T) {
	repo := &memoryGatewayRepository{
		toolGrants: []domainaigateway.ToolGrant{
			{
				ID:          "team-allow",
				SubjectType: "team",
				SubjectID:   "platform-org",
				AIClientID:  "codex",
				ToolName:    "delivery.applications.list",
				Effect:      "allow",
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayView,
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsView,
				appaccess.PermDeliveryBuildsTrigger,
			},
		},
	}), nil, repo)
	principal := testPrincipal("developer")
	principal.Teams = []string{"platform-org"}

	manifest, err := service.Capabilities(context.Background(), principal, domainaigateway.ManifestRequest{AIClientID: "codex"})
	if err != nil {
		t.Fatalf("Capabilities returned error: %v", err)
	}
	if !hasTool(manifest.Tools, "delivery.applications.list") {
		t.Fatalf("expected organization grant to keep application list tool, got %#v", manifest.Tools)
	}
	if hasTool(manifest.Tools, "delivery.actions.trigger") {
		t.Fatalf("did not expect trigger tool outside organization allow-list grants")
	}
}

func TestCapabilitiesFiltersToolsByAccessPolicyAllowList(t *testing.T) {
	repo := &memoryGatewayRepository{
		accessPolicies: []domainaigateway.AccessPolicy{
			{
				ID:           "policy-1",
				Enabled:      true,
				SubjectType:  "user",
				SubjectID:    "user-1",
				AIClientID:   "codex",
				Effect:       "allow",
				ToolPatterns: []string{"delivery.applications.list"},
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayView,
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsView,
				appaccess.PermDeliveryBuildsTrigger,
				appaccess.PermDeliveryReleasesTrigger,
			},
		},
	}), nil, repo)

	manifest, err := service.Capabilities(context.Background(), testPrincipal("developer"), domainaigateway.ManifestRequest{AIClientID: "codex"})
	if err != nil {
		t.Fatalf("Capabilities returned error: %v", err)
	}
	if !hasTool(manifest.Tools, "delivery.applications.list") {
		t.Fatalf("expected policy-allowed application list tool, got %#v", manifest.Tools)
	}
	if hasTool(manifest.Tools, "delivery.actions.trigger") {
		t.Fatalf("did not expect trigger tool outside access policy allow-list")
	}
}

func TestCapabilitiesMarksApprovalFromAccessPolicy(t *testing.T) {
	repo := &memoryGatewayRepository{
		accessPolicies: []domainaigateway.AccessPolicy{
			{
				ID:             "policy-1",
				Enabled:        true,
				SubjectType:    "role",
				SubjectID:      "developer",
				Effect:         "allow",
				ToolPatterns:   []string{"delivery.applications.create"},
				ApprovalPolicy: map[string]any{"requiresApproval": true},
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayView,
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsCreate,
			},
		},
	}), nil, repo)

	manifest, err := service.Capabilities(context.Background(), testPrincipal("developer"), domainaigateway.ManifestRequest{})
	if err != nil {
		t.Fatalf("Capabilities returned error: %v", err)
	}
	found := false
	for _, tool := range manifest.Tools {
		if tool.Name != "delivery.applications.create" {
			continue
		}
		found = true
		if !tool.RequiresApproval {
			t.Fatalf("expected access policy to mark create tool as approval-required")
		}
	}
	if !found {
		t.Fatalf("expected create tool in manifest, got %#v", manifest.Tools)
	}
}

func TestEvaluateToolAccessPoliciesUsesEvaluationContext(t *testing.T) {
	tool := domainaigateway.ToolCapability{
		Name:      "delivery.actions.trigger",
		RiskLevel: domainaigateway.RiskLevelHigh,
	}
	knownSkills := []domainaigateway.SkillCapability{
		{
			ID:             "delivery-skill",
			CapabilityRefs: []string{"delivery.actions.trigger"},
		},
	}

	tests := []struct {
		name       string
		ctx        policyEvaluationContext
		policies   []domainaigateway.AccessPolicy
		wantAllow  bool
		wantReason string
		wantHold   bool
	}{
		{
			name: "allow policy uses explicit skill and scope",
			ctx: policyEvaluationContext{
				Tool:            tool,
				SkillID:         "delivery-skill",
				InvocationScope: map[string]string{"applicationId": "app-1"},
				KnownSkills:     knownSkills,
			},
			policies: []domainaigateway.AccessPolicy{
				{
					ID:             "policy-allow",
					Effect:         "allow",
					SkillIDs:       []string{"delivery-skill"},
					ResourceScopes: map[string]any{"applicationId": "app-1"},
				},
			},
			wantAllow: true,
		},
		{
			name: "deny policy wins before allow",
			ctx: policyEvaluationContext{
				Tool:            tool,
				InvocationScope: map[string]string{"applicationId": "app-1"},
				KnownSkills:     knownSkills,
			},
			policies: []domainaigateway.AccessPolicy{
				{ID: "policy-deny", Effect: "deny", ToolPatterns: []string{"delivery.*"}},
				{ID: "policy-allow", Effect: "allow", ToolPatterns: []string{"delivery.*"}},
			},
			wantReason: "matched deny policy policy-deny",
		},
		{
			name: "scope mismatch reports scoped allow reason",
			ctx: policyEvaluationContext{
				Tool:            tool,
				InvocationScope: map[string]string{"applicationId": "app-2"},
				KnownSkills:     knownSkills,
			},
			policies: []domainaigateway.AccessPolicy{
				{
					ID:             "policy-scoped",
					Effect:         "allow",
					ToolPatterns:   []string{"delivery.*"},
					ResourceScopes: map[string]any{"applicationId": "app-1"},
				},
			},
			wantReason: "matching allow policy does not allow the requested resource scope",
		},
		{
			name: "approval policy returns hold decision",
			ctx: policyEvaluationContext{
				Tool:        tool,
				KnownSkills: knownSkills,
			},
			policies: []domainaigateway.AccessPolicy{
				{
					ID:             "policy-approval",
					Effect:         "allow",
					ToolPatterns:   []string{"delivery.*"},
					ApprovalPolicy: map[string]any{"requiresApproval": true},
				},
			},
			wantAllow: true,
			wantHold:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allowed, decision, reason := evaluateToolAccessPolicies(tt.ctx, tt.policies)
			if allowed != tt.wantAllow {
				t.Fatalf("allowed = %v, want %v (reason %q)", allowed, tt.wantAllow, reason)
			}
			if reason != tt.wantReason {
				t.Fatalf("reason = %q, want %q", reason, tt.wantReason)
			}
			if decision.shouldHoldExecution() != tt.wantHold {
				t.Fatalf("decision.shouldHoldExecution() = %v, want %v (decision %#v)", decision.shouldHoldExecution(), tt.wantHold, decision)
			}
		})
	}
}

func TestCapabilitiesAccessPolicySkillIDsUseInjectedSkills(t *testing.T) {
	repo := &memoryGatewayRepository{
		accessPolicies: []domainaigateway.AccessPolicy{
			{
				ID:          "policy-1",
				Enabled:     true,
				SubjectType: "role",
				SubjectID:   "developer",
				Effect:      "allow",
				SkillIDs:    []string{"custom-skill"},
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {appaccess.PermAIGatewayView, appaccess.PermAIGatewayInvoke},
		},
	}), nil, repo)
	service.SetCapabilityProviders(customTestCapabilityProvider())

	manifest, err := service.Capabilities(context.Background(), testPrincipal("developer"), domainaigateway.ManifestRequest{})
	if err != nil {
		t.Fatalf("Capabilities returned error: %v", err)
	}
	if !hasTool(manifest.Tools, "custom.echo") || hasTool(manifest.Tools, "custom.blocked") {
		t.Fatalf("expected access policy SkillIDs to use injected skill refs, got %#v", manifest.Tools)
	}
}

func TestCapabilitiesSkillBindingsNarrowSkillsAndTools(t *testing.T) {
	repo := &memoryGatewayRepository{
		skillBindings: []domainaigateway.SkillBinding{
			{
				ID:             "binding-1",
				SubjectType:    "role",
				SubjectID:      "developer",
				SkillID:        "k8s-sre",
				CapabilityRefs: []string{"k8s.pods.logs"},
				Enabled:        true,
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayView,
				appaccess.PermAIGatewayInvoke,
				appaccess.PermWorkspaceResourceView,
				appaccess.PermPlatformWorkloadsView,
				appaccess.PermDeliveryApplicationsView,
			},
		},
	}), nil, repo)

	manifest, err := service.Capabilities(context.Background(), testPrincipal("developer"), domainaigateway.ManifestRequest{})
	if err != nil {
		t.Fatalf("Capabilities returned error: %v", err)
	}
	if len(manifest.Skills) != 1 || manifest.Skills[0].ID != "k8s-sre" {
		t.Fatalf("expected only bound k8s-sre skill, got %#v", manifest.Skills)
	}
	if !hasTool(manifest.Tools, "k8s.pods.logs") {
		t.Fatalf("expected bound pod log capability, got %#v", manifest.Tools)
	}
	if hasTool(manifest.Tools, "delivery.applications.list") || hasTool(manifest.Tools, "k8s.pods.list") {
		t.Fatalf("did not expect tools outside bound capability refs, got %#v", manifest.Tools)
	}
	if !hasResource(manifest.Resources, "soha://k8s/runtime") || hasResource(manifest.Resources, "soha://delivery/applications") {
		t.Fatalf("expected resources to follow bound tool refs, got %#v", manifest.Resources)
	}
	if !hasPrompt(manifest.Prompts, "soha.k8s.diagnose_workload") || hasPrompt(manifest.Prompts, "soha.delivery.plan_release") {
		t.Fatalf("expected prompts to follow bound resource refs, got %#v", manifest.Prompts)
	}
	if len(manifest.Resources) != 1 || fmt.Sprint(manifest.Resources[0].ContextSchema["required"]) != "[clusterId]" {
		t.Fatalf("expected resource context schema for k8s runtime, got %#v", manifest.Resources)
	}
	if len(manifest.Prompts) != 1 || fmt.Sprint(manifest.Prompts[0].ArgumentSchema["required"]) != "[clusterId]" {
		t.Fatalf("expected prompt argument schema for k8s diagnose, got %#v", manifest.Prompts)
	}
}

func TestCapabilitiesSkillBindingsUseInjectedSkillDefaults(t *testing.T) {
	repo := &memoryGatewayRepository{
		skillBindings: []domainaigateway.SkillBinding{
			{
				ID:          "binding-1",
				SubjectType: "user",
				SubjectID:   "user-1",
				SkillID:     "custom-skill",
				Enabled:     true,
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {appaccess.PermAIGatewayView, appaccess.PermAIGatewayInvoke},
		},
	}), nil, repo)
	service.SetCapabilityProviders(customTestCapabilityProvider())

	manifest, err := service.Capabilities(context.Background(), testPrincipal("developer"), domainaigateway.ManifestRequest{SkillID: "custom-skill"})
	if err != nil {
		t.Fatalf("Capabilities returned error: %v", err)
	}
	if !hasSkill(manifest.Skills, "custom-skill") || !hasTool(manifest.Tools, "custom.echo") || hasTool(manifest.Tools, "custom.blocked") {
		t.Fatalf("expected empty binding refs to expand from injected skill, got skills=%#v tools=%#v", manifest.Skills, manifest.Tools)
	}
	if !hasResource(manifest.Resources, "soha://custom/context") || !hasPrompt(manifest.Prompts, "custom.prompt") || hasPrompt(manifest.Prompts, "custom.blocked_prompt") {
		t.Fatalf("expected resources/prompts to follow injected resource refs, got resources=%#v prompts=%#v", manifest.Resources, manifest.Prompts)
	}
}

func TestReadResourceUsesGatewayPermissionAndAudit(t *testing.T) {
	audit := &captureAuditRecorder{}
	repo := &memoryGatewayRepository{}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsView,
			},
		},
	}), audit, repo)

	result, err := service.ReadResource(context.Background(), testPrincipal("developer"), domainaigateway.ResourceReadRequest{
		URI:        "soha://resource/soha://delivery/applications",
		Context:    map[string]any{"applicationId": "app-1", "password": "secret"},
		AIClientID: "codex",
		SkillID:    "delivery-developer",
	})
	if err != nil {
		t.Fatalf("ReadResource returned error: %v", err)
	}
	if result.URI != "soha://delivery/applications" || result.MIMEType != "application/json" {
		t.Fatalf("unexpected resource result: %#v", result)
	}
	if !strings.Contains(result.Text, "delivery.applications.detail") {
		t.Fatalf("expected related delivery tools in resource text: %s", result.Text)
	}
	if strings.Contains(result.Text, "secret") {
		t.Fatalf("resource text leaked sensitive context: %s", result.Text)
	}
	if len(audit.entries) != 1 || audit.entries[0].ResourceKind != "AIGatewayResource" || audit.entries[0].Result != "success" {
		t.Fatalf("expected resource audit entry, got %#v", audit.entries)
	}
	if len(repo.auditLogs) != 1 || repo.auditLogs[0].Action != "ai_gateway.resource.read" || repo.auditLogs[0].ResourceScope["applicationId"] != "app-1" {
		t.Fatalf("expected dedicated resource audit log, got %#v", repo.auditLogs)
	}
}

func TestReadResourceUsesInjectedCapabilityMetadata(t *testing.T) {
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {appaccess.PermAIGatewayInvoke},
		},
	}), nil, &memoryGatewayRepository{})
	service.SetCapabilityProviders(testCapabilityProvider{
		tools: []domainaigateway.ToolCapability{
			{Name: "custom.echo", Description: "Injected custom echo tool.", RiskLevel: domainaigateway.RiskLevelRead, PermissionKeys: []string{appaccess.PermAIGatewayInvoke}},
		},
		resources: []domainaigateway.ResourceCapability{
			{Name: "soha://custom/context", Description: "Injected custom context resource.", PermissionKeys: []string{appaccess.PermAIGatewayInvoke}},
		},
		prompts: []domainaigateway.PromptCapability{
			{Name: "custom.prompt", Description: "Injected custom prompt.", PermissionKeys: []string{appaccess.PermAIGatewayInvoke}},
		},
		skills: []domainaigateway.SkillCapability{
			{ID: "custom-skill", Name: "Injected Custom Skill", Category: "custom", CapabilityRefs: []string{"custom.echo"}, PermissionKeys: []string{appaccess.PermAIGatewayInvoke}},
		},
		resourceRefs: []ResourceCapabilityRefs{
			{
				Resource: "soha://custom/context",
				Tools:    []string{"custom.echo"},
				Prompts:  []string{"custom.prompt"},
				Skills:   []string{"custom-skill"},
			},
		},
	})

	result, err := service.ReadResource(context.Background(), testPrincipal("developer"), domainaigateway.ResourceReadRequest{URI: "soha://custom/context"})
	if err != nil {
		t.Fatalf("ReadResource returned error: %v", err)
	}
	if !strings.Contains(result.Text, "Injected custom echo tool.") || !strings.Contains(result.Text, "Injected custom prompt.") || !strings.Contains(result.Text, "Injected Custom Skill") {
		t.Fatalf("expected resource document to use injected metadata, got %s", result.Text)
	}
	if result.RelatedIDs["relatedToolCount"] != 1 || result.RelatedIDs["relatedPromptCount"] != 1 || result.RelatedIDs["relatedSkillCount"] != 1 {
		t.Fatalf("expected related counts to use injected refs, got %#v", result.RelatedIDs)
	}
}

func TestReadResourceRejectsSkillBindingCapabilityMismatch(t *testing.T) {
	audit := &captureAuditRecorder{}
	repo := &memoryGatewayRepository{
		skillBindings: []domainaigateway.SkillBinding{
			{
				ID:             "binding-1",
				SubjectType:    "user",
				SubjectID:      "user-1",
				SkillID:        "k8s-sre",
				CapabilityRefs: []string{"k8s.pods.logs"},
				Enabled:        true,
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermWorkspaceResourceView,
				appaccess.PermDeliveryApplicationsView,
			},
		},
	}), audit, repo)

	result, err := service.ReadResource(context.Background(), testPrincipal("developer"), domainaigateway.ResourceReadRequest{
		URI:     "soha://k8s/runtime",
		SkillID: "k8s-sre",
	})
	if err != nil {
		t.Fatalf("ReadResource returned error: %v", err)
	}
	if strings.Contains(result.Text, "k8s.pods.list") || strings.Contains(result.Text, "security-change") {
		t.Fatalf("resource text leaked unbound capability refs: %s", result.Text)
	}
	if !strings.Contains(result.Text, "k8s.pods.logs") || !strings.Contains(result.Text, "soha.k8s.diagnose_workload") {
		t.Fatalf("resource text missing bound capability refs: %s", result.Text)
	}

	_, err = service.ReadResource(context.Background(), testPrincipal("developer"), domainaigateway.ResourceReadRequest{
		URI:     "soha://delivery/applications",
		SkillID: "k8s-sre",
	})
	if err == nil || !strings.Contains(err.Error(), "skill binding rejected") {
		t.Fatalf("expected skill binding rejection for delivery resource, got %v", err)
	}
}

func TestInvokeToolUsesInjectedProviderInvoker(t *testing.T) {
	invoked := false
	repo := &memoryGatewayRepository{}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {appaccess.PermAIGatewayInvoke},
		},
	}), nil, repo)
	service.SetCapabilityProviders(testCapabilityProvider{
		tools: []domainaigateway.ToolCapability{
			{Name: "custom.echo", RiskLevel: domainaigateway.RiskLevelRead, PermissionKeys: []string{appaccess.PermAIGatewayInvoke}},
		},
		invoke: func(_ context.Context, principal domainidentity.Principal, tool domainaigateway.ToolCapability, input map[string]any) (any, map[string]any, error) {
			invoked = true
			if principal.UserID != "user-1" || tool.Name != "custom.echo" || input["message"] != "hello" {
				t.Fatalf("unexpected provider invocation context principal=%#v tool=%#v input=%#v", principal, tool, input)
			}
			return map[string]any{"echo": input["message"]}, map[string]any{"customId": "custom-1"}, nil
		},
	})

	result, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName: "custom.echo",
		Input:    map[string]any{"message": "hello"},
	})
	if err != nil {
		t.Fatalf("InvokeTool returned error: %v", err)
	}
	if !invoked || mustValueAs[map[string]any](t, result.Output)["echo"] != "hello" || result.RelatedIDs["customId"] != "custom-1" {
		t.Fatalf("expected injected provider to execute custom tool, result=%#v invoked=%v", result, invoked)
	}
	if len(repo.auditLogs) != 1 || repo.auditLogs[0].ToolName != "custom.echo" || repo.auditLogs[0].Result != "success" {
		t.Fatalf("expected custom provider invocation to stay inside Gateway audit boundary, got %#v", repo.auditLogs)
	}
	_, err = service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{ToolName: "delivery.applications.list"})
	if err == nil || !strings.Contains(err.Error(), "unknown AI Gateway tool") {
		t.Fatalf("expected replaced default tool to be unknown, got %v", err)
	}
}

func TestInvokeToolRecordsFailedHighRiskProviderAuditAndOperation(t *testing.T) {
	secret := fakeGitHubPATForTest()
	audit := &captureAuditRecorder{}
	operations := &captureOperationRecorder{}
	repo := &memoryGatewayRepository{}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {appaccess.PermAIGatewayInvoke},
		},
	}), audit, repo)
	service.SetOperationRecorder(operations)
	service.SetCapabilityProviders(testCapabilityProvider{
		tools: []domainaigateway.ToolCapability{
			{Name: "custom.mutate", RiskLevel: domainaigateway.RiskLevelHigh, PermissionKeys: []string{appaccess.PermAIGatewayInvoke}},
		},
		invoke: func(context.Context, domainidentity.Principal, domainaigateway.ToolCapability, map[string]any) (any, map[string]any, error) {
			return nil, map[string]any{"providerRequestId": "provider-1"}, fmt.Errorf("provider rejected token %s", secret)
		},
	})

	_, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName: "custom.mutate",
		Input: map[string]any{
			"clusterId": "cluster-a",
			"namespace": "prod",
			"action":    "restart",
		},
	})
	if err == nil || !strings.Contains(err.Error(), secret) {
		t.Fatalf("expected raw provider error to be returned to caller, got %v", err)
	}
	if len(audit.entries) != 1 || audit.entries[0].Result != "failure" {
		t.Fatalf("expected failed generic audit entry, got %#v", audit.entries)
	}
	if len(repo.auditLogs) != 1 {
		t.Fatalf("expected failed Gateway audit log, got %#v", repo.auditLogs)
	}
	gatewayAudit := repo.auditLogs[0]
	if gatewayAudit.Result != "failure" || gatewayAudit.RiskLevel != domainaigateway.RiskLevelHigh {
		t.Fatalf("expected failed high-risk Gateway audit log, got %#v", gatewayAudit)
	}
	if gatewayAudit.ResourceScope["clusterId"] != "cluster-a" || gatewayAudit.ResourceScope["namespace"] != "prod" {
		t.Fatalf("expected Gateway audit scope, got %#v", gatewayAudit.ResourceScope)
	}
	if len(operations.entries) != 1 {
		t.Fatalf("expected failed operation log, got %#v", operations.entries)
	}
	operation := operations.entries[0]
	if operation.OperationType != "ai_gateway.tool.invoke" || operation.Result != "failure" {
		t.Fatalf("expected failed AI Gateway operation log, got %#v", operation)
	}
	if operation.TargetScope["riskLevel"] != domainaigateway.RiskLevelHigh || operation.TargetScope["clusterId"] != "cluster-a" || operation.TargetScope["namespace"] != "prod" {
		t.Fatalf("expected operation target scope, got %#v", operation.TargetScope)
	}
	for name, value := range map[string]any{
		"generic audit": audit.entries,
		"Gateway audit": repo.auditLogs,
		"operation log": operations.entries,
	} {
		if text := fmt.Sprint(value); strings.Contains(text, secret) {
			t.Fatalf("%s leaked raw provider secret: %s", name, text)
		}
	}
}

func TestInvokeToolRejectsInjectedProviderWithoutInvoker(t *testing.T) {
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {appaccess.PermAIGatewayInvoke},
		},
	}), nil, &memoryGatewayRepository{})
	service.SetCapabilityProviders(testCapabilityProvider{
		tools: []domainaigateway.ToolCapability{
			{Name: "custom.echo", RiskLevel: domainaigateway.RiskLevelRead, PermissionKeys: []string{appaccess.PermAIGatewayInvoke}},
		},
	})

	_, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{ToolName: "custom.echo"})
	if err == nil || !strings.Contains(err.Error(), "not implemented yet") {
		t.Fatalf("expected custom tool without invoker to be rejected, got %v", err)
	}
}

func TestInvokeToolAppliesRiskPolicyBeforeInjectedProviderInvoker(t *testing.T) {
	invoked := false
	repo := &memoryGatewayRepository{
		accessPolicies: []domainaigateway.AccessPolicy{
			{
				ID:             "policy-dry-run",
				Enabled:        true,
				SubjectType:    "role",
				SubjectID:      "developer",
				Effect:         "allow",
				ToolPatterns:   []string{"custom.mutate"},
				ApprovalPolicy: map[string]any{"strategy": "dry_run_only"},
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {appaccess.PermAIGatewayInvoke},
		},
	}), nil, repo)
	service.SetCapabilityProviders(testCapabilityProvider{
		tools: []domainaigateway.ToolCapability{
			{Name: "custom.mutate", RiskLevel: domainaigateway.RiskLevelHigh, PermissionKeys: []string{appaccess.PermAIGatewayInvoke}},
		},
		invoke: func(context.Context, domainidentity.Principal, domainaigateway.ToolCapability, map[string]any) (any, map[string]any, error) {
			invoked = true
			return map[string]any{"mutated": true}, nil, nil
		},
	})

	result, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName: "custom.mutate",
		Input:    map[string]any{"target": "prod"},
	})
	if err != nil {
		t.Fatalf("InvokeTool returned error: %v", err)
	}
	if invoked {
		t.Fatalf("provider invoker should not run when Gateway risk policy holds execution")
	}
	if result.Result != "dry_run" || result.RelatedIDs["dryRunId"] == "" {
		t.Fatalf("expected dry-run result before provider invocation, got %#v", result)
	}
	if len(repo.auditLogs) != 1 || repo.auditLogs[0].Result != "dry_run" || repo.auditLogs[0].ToolName != "custom.mutate" {
		t.Fatalf("expected dry-run Gateway audit for custom tool, got %#v", repo.auditLogs)
	}
}

func TestGetPromptCombinesSkillContextAndAudit(t *testing.T) {
	audit := &captureAuditRecorder{}
	repo := &memoryGatewayRepository{}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermWorkspaceResourceView,
			},
		},
	}), audit, repo)

	result, err := service.GetPrompt(context.Background(), testPrincipal("developer"), domainaigateway.PromptGetRequest{
		Name:      "soha.k8s.diagnose_workload",
		Arguments: map[string]any{"clusterId": "cluster-a", "namespace": "prod", "token": "secret"},
		SkillID:   "k8s-sre",
	})
	if err != nil {
		t.Fatalf("GetPrompt returned error: %v", err)
	}
	if len(result.Messages) != 1 {
		t.Fatalf("expected one prompt message, got %#v", result.Messages)
	}
	content := result.Messages[0].Content
	if !strings.Contains(content, "K8s SRE") || !strings.Contains(content, "cluster-a") || !strings.Contains(content, "k8s.deployments.rollout_status") {
		t.Fatalf("prompt did not combine skill and context: %s", content)
	}
	if strings.Contains(content, "secret") {
		t.Fatalf("prompt leaked sensitive arguments: %s", content)
	}
	if len(audit.entries) != 1 || audit.entries[0].ResourceKind != "AIGatewayPrompt" || audit.entries[0].Result != "success" {
		t.Fatalf("expected prompt audit entry, got %#v", audit.entries)
	}
	if len(repo.auditLogs) != 1 || repo.auditLogs[0].Action != "ai_gateway.prompt.get" || repo.auditLogs[0].ResourceScope["clusterId"] != "cluster-a" {
		t.Fatalf("expected dedicated prompt audit log, got %#v", repo.auditLogs)
	}
}

func TestGetPromptRejectsSkillBindingCapabilityMismatch(t *testing.T) {
	repo := &memoryGatewayRepository{
		skillBindings: []domainaigateway.SkillBinding{
			{
				ID:             "binding-1",
				SubjectType:    "user",
				SubjectID:      "user-1",
				SkillID:        "k8s-sre",
				CapabilityRefs: []string{"k8s.pods.logs"},
				Enabled:        true,
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermWorkspaceResourceView,
				appaccess.PermDeliveryApplicationsView,
			},
		},
	}), nil, repo)

	result, err := service.GetPrompt(context.Background(), testPrincipal("developer"), domainaigateway.PromptGetRequest{
		Name:    "soha.k8s.diagnose_workload",
		SkillID: "k8s-sre",
	})
	if err != nil {
		t.Fatalf("GetPrompt returned error: %v", err)
	}
	content := result.Messages[0].Content
	if strings.Contains(content, "k8s.pods.list") || !strings.Contains(content, "k8s.pods.logs") {
		t.Fatalf("prompt skill context did not narrow capability refs: %s", content)
	}

	_, err = service.GetPrompt(context.Background(), testPrincipal("developer"), domainaigateway.PromptGetRequest{
		Name:    "soha.delivery.plan_release",
		SkillID: "k8s-sre",
	})
	if err == nil || !strings.Contains(err.Error(), "skill binding rejected") {
		t.Fatalf("expected skill binding rejection for delivery prompt, got %v", err)
	}
}

func TestGetPromptUsesInjectedResourceCapabilityRefs(t *testing.T) {
	repo := &memoryGatewayRepository{
		skillBindings: []domainaigateway.SkillBinding{
			{
				ID:          "binding-1",
				SubjectType: "user",
				SubjectID:   "user-1",
				SkillID:     "custom-skill",
				Enabled:     true,
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {appaccess.PermAIGatewayInvoke},
		},
	}), nil, repo)
	service.SetCapabilityProviders(testCapabilityProvider{
		tools: []domainaigateway.ToolCapability{
			{Name: "custom.echo", RiskLevel: domainaigateway.RiskLevelRead, PermissionKeys: []string{appaccess.PermAIGatewayInvoke}},
		},
		resources: []domainaigateway.ResourceCapability{
			{Name: "soha://custom/context", PermissionKeys: []string{appaccess.PermAIGatewayInvoke}},
		},
		prompts: []domainaigateway.PromptCapability{
			{Name: "custom.prompt", Description: "Custom prompt.", PermissionKeys: []string{appaccess.PermAIGatewayInvoke}},
			{Name: "custom.blocked_prompt", Description: "Blocked prompt.", PermissionKeys: []string{appaccess.PermAIGatewayInvoke}},
		},
		skills: []domainaigateway.SkillCapability{
			{ID: "custom-skill", Name: "Custom Skill", CapabilityRefs: []string{"custom.echo"}, PermissionKeys: []string{appaccess.PermAIGatewayInvoke}},
		},
		resourceRefs: []ResourceCapabilityRefs{
			{Resource: "soha://custom/context", Tools: []string{"custom.echo"}, Prompts: []string{"custom.prompt"}, Skills: []string{"custom-skill"}},
		},
	})

	result, err := service.GetPrompt(context.Background(), testPrincipal("developer"), domainaigateway.PromptGetRequest{
		Name:    "custom.prompt",
		SkillID: "custom-skill",
	})
	if err != nil {
		t.Fatalf("GetPrompt returned error: %v", err)
	}
	if !strings.Contains(result.Messages[0].Content, "Custom Skill") {
		t.Fatalf("expected injected skill context, got %s", result.Messages[0].Content)
	}

	_, err = service.GetPrompt(context.Background(), testPrincipal("developer"), domainaigateway.PromptGetRequest{
		Name:    "custom.blocked_prompt",
		SkillID: "custom-skill",
	})
	if err == nil || !strings.Contains(err.Error(), "skill binding rejected") {
		t.Fatalf("expected unlinked prompt to be rejected, got %v", err)
	}
}

func TestCreatePersonalAccessTokenCapsPermissionsToRequest(t *testing.T) {
	repo := &memoryGatewayRepository{}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayView,
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsView,
				appaccess.PermDeliveryBuildsTrigger,
			},
		},
	}), nil, repo)

	created, err := service.CreatePersonalAccessToken(context.Background(), testPrincipal("developer"), domainaigateway.PersonalAccessTokenInput{
		Name:           "codex",
		PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermDeliveryApplicationsView},
	})
	if err != nil {
		t.Fatalf("CreatePersonalAccessToken returned error: %v", err)
	}
	if created.Value == "" || created.Token.TokenHash == "" || created.Token.TokenPrefix == "" {
		t.Fatalf("expected one-time token value plus persisted hash/prefix, got %#v", created)
	}
	if !slices.Contains(created.Token.PermissionKeys, appaccess.PermDeliveryApplicationsView) {
		t.Fatalf("expected requested delivery permission cap, got %#v", created.Token.PermissionKeys)
	}
	if slices.Contains(created.Token.PermissionKeys, appaccess.PermDeliveryBuildsTrigger) {
		t.Fatalf("did not expect unrequested permission to be granted, got %#v", created.Token.PermissionKeys)
	}
}

func TestCreatePersonalAccessTokenRejectsPermissionEscalation(t *testing.T) {
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {appaccess.PermAIGatewayInvoke},
		},
	}), nil, &memoryGatewayRepository{})

	_, err := service.CreatePersonalAccessToken(context.Background(), testPrincipal("developer"), domainaigateway.PersonalAccessTokenInput{
		Name:           "codex",
		PermissionKeys: []string{appaccess.PermDeliveryReleasesTrigger},
	})
	if err == nil {
		t.Fatalf("expected permission escalation to be rejected")
	}
}

func TestCreatePersonalAccessTokenNormalizesLLMRelayMetadata(t *testing.T) {
	repo := &memoryGatewayRepository{}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermAIGatewayRelayInvoke,
			},
		},
	}), nil, repo)

	created, err := service.CreatePersonalAccessToken(context.Background(), testPrincipal("developer"), domainaigateway.PersonalAccessTokenInput{
		Name:           "relay",
		Scopes:         []string{"relay"},
		PermissionKeys: []string{appaccess.PermAIGatewayRelayInvoke},
		Metadata: map[string]any{
			"purpose":              " LLM-Relay ",
			"allowedModels":        []any{"gpt-4.1", "claude-sonnet-4-5", "gpt-4.1"},
			"allowedProviderKinds": []string{"OpenAI", "anthropic", "DeepSeek", "QWEN", "openrouter", "azure_openai", "Gemini", "Cohere"},
			"allowedUpstreamIds":   "upstream-b, upstream-a",
			"allowedIPCIDRs":       []any{"10.0.0.8/32", "10.0.0.0/24"},
			"allowedTeams":         []any{"platform", "ml", "platform"},
			"deniedTeams":          "suspended, blocked",
			"rateLimitProfileId":   " developer-default ",
			"owner":                "platform",
		},
	})
	if err != nil {
		t.Fatalf("CreatePersonalAccessToken returned error: %v", err)
	}
	if created.Token.Metadata["purpose"] != LLMRelayTokenPurpose {
		t.Fatalf("purpose = %#v, want %s", created.Token.Metadata["purpose"], LLMRelayTokenPurpose)
	}
	if created.Token.Metadata["owner"] != "platform" {
		t.Fatalf("unknown metadata should be preserved, got %#v", created.Token.Metadata)
	}
	allowedModels, ok := created.Token.Metadata["allowedModels"].([]string)
	if !ok || !slices.Equal(allowedModels, []string{"claude-sonnet-4-5", "gpt-4.1"}) {
		t.Fatalf("allowedModels = %#v", created.Token.Metadata["allowedModels"])
	}
	allowedProviderKinds, ok := created.Token.Metadata["allowedProviderKinds"].([]string)
	if !ok || !slices.Equal(allowedProviderKinds, []string{"anthropic", "azure-openai", "cohere", "deepseek", "gemini", "openai", "openrouter", "qwen"}) {
		t.Fatalf("allowedProviderKinds = %#v", created.Token.Metadata["allowedProviderKinds"])
	}
	allowedUpstreamIDs, ok := created.Token.Metadata["allowedUpstreamIds"].([]string)
	if !ok || !slices.Equal(allowedUpstreamIDs, []string{"upstream-a", "upstream-b"}) {
		t.Fatalf("allowedUpstreamIds = %#v", created.Token.Metadata["allowedUpstreamIds"])
	}
	allowedIPCIDRs, ok := created.Token.Metadata["allowedIPCIDRs"].([]string)
	if !ok || !slices.Equal(allowedIPCIDRs, []string{"10.0.0.0/24", "10.0.0.8/32"}) {
		t.Fatalf("allowedIPCIDRs = %#v", created.Token.Metadata["allowedIPCIDRs"])
	}
	allowedTeams, ok := created.Token.Metadata["allowedTeams"].([]string)
	if !ok || !slices.Equal(allowedTeams, []string{"ml", "platform"}) {
		t.Fatalf("allowedTeams = %#v", created.Token.Metadata["allowedTeams"])
	}
	deniedTeams, ok := created.Token.Metadata["deniedTeams"].([]string)
	if !ok || !slices.Equal(deniedTeams, []string{"blocked", "suspended"}) {
		t.Fatalf("deniedTeams = %#v", created.Token.Metadata["deniedTeams"])
	}
	if created.Token.Metadata["rateLimitProfileId"] != "developer-default" {
		t.Fatalf("rateLimitProfileId = %#v", created.Token.Metadata["rateLimitProfileId"])
	}
}

func TestCreatePersonalAccessTokenRejectsRelayDebugMetadataWithoutManage(t *testing.T) {
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermAIGatewayRelayInvoke,
			},
		},
	}), nil, &memoryGatewayRepository{})

	_, err := service.CreatePersonalAccessToken(context.Background(), testPrincipal("developer"), domainaigateway.PersonalAccessTokenInput{
		Name:           "relay-debug",
		Scopes:         []string{"relay"},
		PermissionKeys: []string{appaccess.PermAIGatewayRelayInvoke},
		Metadata: map[string]any{
			"purpose":                LLMRelayTokenPurpose,
			"allowRouteTrace":        true,
			"allowUpstreamSelection": true,
		},
	})
	if err == nil {
		t.Fatalf("expected relay debug metadata without manage permission to be rejected")
	}
}

func TestCreatePersonalAccessTokenAllowsRelayDebugMetadataForManager(t *testing.T) {
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"admin": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermAIGatewayRelayInvoke,
				appaccess.PermAIGatewayRelayManage,
			},
		},
	}), nil, &memoryGatewayRepository{})

	principal := testPrincipal("admin")
	principal.PermissionKeys = []string{
		appaccess.PermAIGatewayInvoke,
		appaccess.PermAIGatewayRelayInvoke,
		appaccess.PermAIGatewayRelayManage,
	}
	created, err := service.CreatePersonalAccessToken(context.Background(), principal, domainaigateway.PersonalAccessTokenInput{
		Name:           "relay-debug",
		Scopes:         []string{"relay"},
		PermissionKeys: []string{appaccess.PermAIGatewayRelayInvoke},
		Metadata: map[string]any{
			"purpose":                LLMRelayTokenPurpose,
			"allowRouteTrace":        "true",
			"allowUpstreamSelection": true,
		},
	})
	if err != nil {
		t.Fatalf("CreatePersonalAccessToken returned error: %v", err)
	}
	if created.Token.Metadata["allowRouteTrace"] != true || created.Token.Metadata["allowUpstreamSelection"] != true {
		t.Fatalf("expected relay debug metadata to be preserved as booleans, got %#v", created.Token.Metadata)
	}
}

func TestAuthorizeLLMRelayTokenRejectsLegacyInvokeOnlyToken(t *testing.T) {
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermAIGatewayRelayInvoke,
			},
		},
	}), nil, &memoryGatewayRepository{})
	principal := testPrincipal("developer")
	principal.PermissionKeys = []string{appaccess.PermAIGatewayInvoke}

	_, err := service.AuthorizeLLMRelayToken(context.Background(), principal, domainidentity.AccessContext{
		TokenKind: "personal_access_token",
		Scopes:    []string{"relay"},
		Metadata:  map[string]any{"purpose": LLMRelayTokenPurpose},
	}, LLMRelayAccessRequest{Model: "gpt-4.1"})
	if err == nil {
		t.Fatalf("expected legacy invoke-only token to be rejected")
	}
}

func TestAuthorizeLLMRelayTokenRejectsMissingPurposeOrScope(t *testing.T) {
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {appaccess.PermAIGatewayRelayInvoke},
		},
	}), nil, &memoryGatewayRepository{})
	principal := testPrincipal("developer")
	principal.PermissionKeys = []string{appaccess.PermAIGatewayRelayInvoke}

	_, err := service.AuthorizeLLMRelayToken(context.Background(), principal, domainidentity.AccessContext{
		TokenKind: "personal_access_token",
		Metadata:  map[string]any{"allowedModels": []string{"gpt-4.1"}},
	}, LLMRelayAccessRequest{Model: "gpt-4.1"})
	if err == nil {
		t.Fatalf("expected missing relay purpose/scope to be rejected")
	}
}

func TestAuthorizeLLMRelayTokenParsesAndEnforcesMetadata(t *testing.T) {
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {appaccess.PermAIGatewayRelayInvoke},
		},
	}), nil, &memoryGatewayRepository{})
	principal := testPrincipal("developer")
	principal.PermissionKeys = []string{appaccess.PermAIGatewayRelayInvoke}
	accessCtx := domainidentity.AccessContext{
		TokenKind: "service_account_token",
		Scopes:    []string{"relay"},
		Metadata: map[string]any{
			"allowedModels":        []any{"gpt-4.1"},
			"allowedProviderKinds": []any{"DeepSeek"},
			"allowedUpstreamIds":   []string{"upstream-a"},
			"allowedIPCIDRs":       []string{"10.0.0.0/24"},
			"rateLimitProfileId":   "relay-dev",
		},
	}

	metadata, err := service.AuthorizeLLMRelayToken(context.Background(), principal, accessCtx, LLMRelayAccessRequest{
		Model:        "gpt-4.1",
		ProviderKind: "deepseek",
		UpstreamID:   "upstream-a",
		SourceIP:     "10.0.0.8:1234",
	})
	if err != nil {
		t.Fatalf("AuthorizeLLMRelayToken returned error: %v", err)
	}
	if metadata.RateLimitProfileID != "relay-dev" || !slices.Equal(metadata.AllowedProviderKinds, []string{"deepseek"}) {
		t.Fatalf("metadata = %#v", metadata)
	}

	_, err = service.AuthorizeLLMRelayToken(context.Background(), principal, accessCtx, LLMRelayAccessRequest{
		Model:        "claude-sonnet-4-5",
		ProviderKind: "openai",
		UpstreamID:   "upstream-a",
		SourceIP:     "10.0.0.8",
	})
	if err == nil {
		t.Fatalf("expected disallowed model to be rejected")
	}

	_, err = service.AuthorizeLLMRelayToken(context.Background(), principal, accessCtx, LLMRelayAccessRequest{
		Model:        "gpt-4.1",
		ProviderKind: "openai",
		UpstreamID:   "upstream-a",
		SourceIP:     "192.168.1.8",
	})
	if err == nil {
		t.Fatalf("expected disallowed source IP to be rejected")
	}
}

func TestAuthorizeLLMRelayTokenAllowsAzureOpenAIProviderAlias(t *testing.T) {
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {appaccess.PermAIGatewayRelayInvoke},
		},
	}), nil, &memoryGatewayRepository{})
	principal := testPrincipal("developer")
	principal.PermissionKeys = []string{appaccess.PermAIGatewayRelayInvoke}

	for _, tt := range []struct {
		name            string
		allowedProvider string
		requestProvider string
	}{
		{
			name:            "metadata underscore request hyphen",
			allowedProvider: "azure_openai",
			requestProvider: "azure-openai",
		},
		{
			name:            "metadata hyphen request underscore",
			allowedProvider: "azure-openai",
			requestProvider: "azure_openai",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			metadata, err := service.AuthorizeLLMRelayToken(context.Background(), principal, domainidentity.AccessContext{
				TokenKind: "service_account_token",
				Scopes:    []string{"relay"},
				Metadata: map[string]any{
					"allowedProviderKinds": []string{tt.allowedProvider},
				},
			}, LLMRelayAccessRequest{
				ProviderKind: tt.requestProvider,
			})
			if err != nil {
				t.Fatalf("AuthorizeLLMRelayToken returned error: %v", err)
			}
			if !slices.Equal(metadata.AllowedProviderKinds, []string{"azure-openai"}) {
				t.Fatalf("allowed provider kinds = %#v", metadata.AllowedProviderKinds)
			}
		})
	}
}

func TestAuthorizeLLMRelayTokenAllowsGeminiProviderKind(t *testing.T) {
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {appaccess.PermAIGatewayRelayInvoke},
		},
	}), nil, &memoryGatewayRepository{})
	principal := testPrincipal("developer")
	principal.PermissionKeys = []string{appaccess.PermAIGatewayRelayInvoke}

	metadata, err := service.AuthorizeLLMRelayToken(context.Background(), principal, domainidentity.AccessContext{
		TokenKind: "service_account_token",
		Scopes:    []string{"relay"},
		Metadata: map[string]any{
			"allowedProviderKinds": []string{"Gemini"},
		},
	}, LLMRelayAccessRequest{
		ProviderKind: "gemini",
	})
	if err != nil {
		t.Fatalf("AuthorizeLLMRelayToken returned error: %v", err)
	}
	if !slices.Equal(metadata.AllowedProviderKinds, []string{"gemini"}) {
		t.Fatalf("allowed provider kinds = %#v", metadata.AllowedProviderKinds)
	}
}

func TestAuthorizeLLMRelayTokenAllowsCohereProviderKind(t *testing.T) {
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {appaccess.PermAIGatewayRelayInvoke},
		},
	}), nil, &memoryGatewayRepository{})
	principal := testPrincipal("developer")
	principal.PermissionKeys = []string{appaccess.PermAIGatewayRelayInvoke}

	metadata, err := service.AuthorizeLLMRelayToken(context.Background(), principal, domainidentity.AccessContext{
		TokenKind: "service_account_token",
		Scopes:    []string{"relay"},
		Metadata: map[string]any{
			"allowedProviderKinds": []string{"Cohere"},
		},
	}, LLMRelayAccessRequest{
		ProviderKind: "cohere",
	})
	if err != nil {
		t.Fatalf("AuthorizeLLMRelayToken returned error: %v", err)
	}
	if !slices.Equal(metadata.AllowedProviderKinds, []string{"cohere"}) {
		t.Fatalf("allowed provider kinds = %#v", metadata.AllowedProviderKinds)
	}
}

func TestAuthorizeLLMRelayTokenEnforcesTeamMetadata(t *testing.T) {
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {appaccess.PermAIGatewayRelayInvoke},
		},
	}), nil, &memoryGatewayRepository{})
	principal := testPrincipal("developer")
	principal.PermissionKeys = []string{appaccess.PermAIGatewayRelayInvoke}
	principal.Teams = []string{"platform"}
	accessCtx := domainidentity.AccessContext{
		TokenKind: "service_account_token",
		Scopes:    []string{"relay"},
		Metadata: map[string]any{
			"allowedTeams": []string{"platform", "ml"},
			"deniedTeams":  []string{"suspended"},
		},
	}

	metadata, err := service.AuthorizeLLMRelayToken(context.Background(), principal, accessCtx, LLMRelayAccessRequest{
		Model: "gpt-4.1",
	})
	if err != nil {
		t.Fatalf("AuthorizeLLMRelayToken returned error: %v", err)
	}
	if !slices.Equal(metadata.AllowedTeams, []string{"ml", "platform"}) || !slices.Equal(metadata.DeniedTeams, []string{"suspended"}) {
		t.Fatalf("team metadata = %#v", metadata)
	}

	principal.Teams = []string{"finance"}
	_, err = service.AuthorizeLLMRelayToken(context.Background(), principal, accessCtx, LLMRelayAccessRequest{
		Model: "gpt-4.1",
	})
	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("nonmatching team error = %v, want access denied", err)
	}

	principal.Teams = []string{"platform", "suspended"}
	_, err = service.AuthorizeLLMRelayToken(context.Background(), principal, accessCtx, LLMRelayAccessRequest{
		Model: "gpt-4.1",
	})
	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("denied team error = %v, want access denied", err)
	}
}

func TestListPersonalAccessTokensDefaultsToOwnerAndAllowsManageScopeAll(t *testing.T) {
	repo := &memoryGatewayRepository{
		personalTokens: []domainaigateway.PersonalAccessToken{
			{ID: "pat-owner", UserID: "user-1", Name: "mine"},
			{ID: "pat-other", UserID: "user-2", Name: "other"},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {appaccess.PermAIGatewayView},
			"admin":     {appaccess.PermAIGatewayManage},
		},
	}), nil, repo)

	own, err := service.ListPersonalAccessTokens(context.Background(), testPrincipal("developer"), domainaigateway.PersonalAccessTokenListRequest{})
	if err != nil {
		t.Fatalf("ListPersonalAccessTokens returned error: %v", err)
	}
	if len(own) != 1 || own[0].ID != "pat-owner" {
		t.Fatalf("expected only owner token, got %#v", own)
	}

	all, err := service.ListPersonalAccessTokens(context.Background(), testPrincipal("admin"), domainaigateway.PersonalAccessTokenListRequest{Scope: "all"})
	if err != nil {
		t.Fatalf("ListPersonalAccessTokens scope=all returned error: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected all tokens for manager, got %#v", all)
	}

	filtered, err := service.ListPersonalAccessTokens(context.Background(), testPrincipal("admin"), domainaigateway.PersonalAccessTokenListRequest{UserID: "user-2"})
	if err != nil {
		t.Fatalf("ListPersonalAccessTokens user filter returned error: %v", err)
	}
	if len(filtered) != 1 || filtered[0].ID != "pat-other" {
		t.Fatalf("expected filtered user token, got %#v", filtered)
	}

	if _, err := service.ListPersonalAccessTokens(context.Background(), testPrincipal("developer"), domainaigateway.PersonalAccessTokenListRequest{Scope: "all"}); err == nil {
		t.Fatalf("expected scope=all to require manage permission")
	}
}

func TestManageCanRevokeAnotherUsersPersonalAccessToken(t *testing.T) {
	repo := &memoryGatewayRepository{
		personalTokens: []domainaigateway.PersonalAccessToken{
			{ID: "pat-owner", UserID: "user-1", Name: "mine"},
			{ID: "pat-other", UserID: "user-2", Name: "other"},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {appaccess.PermAIGatewayInvoke},
			"admin":     {appaccess.PermAIGatewayManage},
		},
	}), nil, repo)

	if err := service.RevokePersonalAccessToken(context.Background(), testPrincipal("admin"), "pat-other"); err != nil {
		t.Fatalf("RevokePersonalAccessToken returned error: %v", err)
	}
	if repo.personalTokens[1].RevokedAt == nil {
		t.Fatalf("expected manager to revoke another user's token")
	}

	if err := service.RevokePersonalAccessToken(context.Background(), testPrincipal("developer"), "pat-other"); err == nil {
		t.Fatalf("expected non-manager to be unable to revoke another user's token")
	}
}

func TestRotatePersonalAccessTokenRevokesPreviousAndReturnsReplacement(t *testing.T) {
	expiredAt := time.Now().UTC().Add(-time.Hour)
	repo := &memoryGatewayRepository{
		personalTokens: []domainaigateway.PersonalAccessToken{{
			ID:             "pat-old",
			UserID:         "user-1",
			Name:           "codex",
			TokenHash:      "old-hash",
			TokenPrefix:    "soha_pat_old",
			Scopes:         []string{"mcp"},
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke},
			Metadata:       map[string]any{"client": "codex"},
			ExpiresAt:      &expiredAt,
		}},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {appaccess.PermAIGatewayInvoke},
		},
	}), nil, repo)

	created, err := service.RotatePersonalAccessToken(context.Background(), testPrincipal("developer"), "pat-old", domainaigateway.TokenRotationInput{})
	if err != nil {
		t.Fatalf("RotatePersonalAccessToken returned error: %v", err)
	}
	if created.Value == "" || created.Token.ID == "pat-old" || created.Token.TokenHash != domainaigateway.HashToken(created.Value) {
		t.Fatalf("expected replacement token with returned value, got %#v", created)
	}
	if created.Token.ExpiresAt == nil || created.Token.ExpiresAt.Before(time.Now().UTC().Add(89*24*time.Hour)) {
		t.Fatalf("expected expired token rotation to get default future expiry, got %#v", created.Token.ExpiresAt)
	}
	if !slices.Contains(created.Token.PermissionKeys, appaccess.PermAIGatewayInvoke) || !slices.Contains(created.Token.Scopes, "mcp") {
		t.Fatalf("expected previous scopes and permission keys to be preserved, got %#v", created.Token)
	}
	if created.Token.Metadata["client"] != "codex" {
		t.Fatalf("expected metadata to be copied, got %#v", created.Token.Metadata)
	}
	if repo.personalTokens[0].RevokedAt == nil {
		t.Fatalf("expected previous token to be revoked")
	}
}

func TestCreateServiceAccountTokenUsesServiceAccountRolePermissions(t *testing.T) {
	repo := &memoryGatewayRepository{
		serviceAccounts: map[string]domainaigateway.ServiceAccount{
			"ci": {
				ID:      "ci",
				Name:    "ci",
				Status:  "active",
				RoleIDs: []string{"ci-role"},
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"admin": {
				appaccess.PermAIGatewayManage,
			},
			"ci-role": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryBuildsTrigger,
			},
		},
	}), nil, repo)

	created, err := service.CreateServiceAccountToken(context.Background(), testPrincipal("admin"), "ci", domainaigateway.ServiceAccountTokenInput{
		Name:           "runner",
		PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermDeliveryBuildsTrigger},
	})
	if err != nil {
		t.Fatalf("CreateServiceAccountToken returned error: %v", err)
	}
	if created.Value == "" || created.Token.ServiceAccountID != "ci" {
		t.Fatalf("expected token for service account ci, got %#v", created)
	}
}

func TestRotateServiceAccountTokenUsesCurrentServiceAccountPermissions(t *testing.T) {
	expiresAt := time.Now().UTC().Add(2 * time.Hour)
	repo := &memoryGatewayRepository{
		serviceAccounts: map[string]domainaigateway.ServiceAccount{
			"ci": {
				ID:      "ci",
				Name:    "ci",
				Status:  "active",
				RoleIDs: []string{"ci-role"},
			},
		},
		serviceAccountTokens: []domainaigateway.ServiceAccountToken{{
			ID:               "sat-old",
			ServiceAccountID: "ci",
			Name:             "runner",
			TokenHash:        "old-hash",
			TokenPrefix:      "soha_sat_old",
			Scopes:           []string{"runner"},
			PermissionKeys:   []string{appaccess.PermAIGatewayInvoke, appaccess.PermDeliveryBuildsTrigger},
		}},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"admin":   {appaccess.PermAIGatewayManage},
			"ci-role": {appaccess.PermAIGatewayInvoke, appaccess.PermDeliveryBuildsTrigger},
		},
	}), nil, repo)

	created, err := service.RotateServiceAccountToken(context.Background(), testPrincipal("admin"), "sat-old", domainaigateway.TokenRotationInput{ExpiresAt: &expiresAt})
	if err != nil {
		t.Fatalf("RotateServiceAccountToken returned error: %v", err)
	}
	if created.Value == "" || created.Token.ServiceAccountID != "ci" || created.Token.ID == "sat-old" {
		t.Fatalf("expected replacement service account token, got %#v", created)
	}
	if created.Token.ExpiresAt == nil || !created.Token.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("expected requested expiry to be used, got %#v", created.Token.ExpiresAt)
	}
	if !slices.Contains(created.Token.PermissionKeys, appaccess.PermDeliveryBuildsTrigger) || !slices.Contains(created.Token.Scopes, "runner") {
		t.Fatalf("expected previous token boundaries to be preserved, got %#v", created.Token)
	}
	if repo.serviceAccountTokens[0].RevokedAt == nil {
		t.Fatalf("expected previous service account token to be revoked")
	}
}

func TestListServiceAccountTokensRequiresManageAndHidesHashFromJSON(t *testing.T) {
	repo := &memoryGatewayRepository{
		serviceAccountTokens: []domainaigateway.ServiceAccountToken{{
			ID:               "sat-1",
			ServiceAccountID: "ci",
			Name:             "runner",
			TokenHash:        "hash-must-not-leak",
			TokenPrefix:      "soha_sat_abc",
			PermissionKeys:   []string{appaccess.PermAIGatewayInvoke},
		}},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"admin":     {appaccess.PermAIGatewayManage},
			"developer": {appaccess.PermAIGatewayView},
		},
	}), nil, repo)

	items, err := service.ListServiceAccountTokens(context.Background(), testPrincipal("admin"))
	if err != nil {
		t.Fatalf("ListServiceAccountTokens returned error: %v", err)
	}
	if len(items) != 1 || items[0].ID != "sat-1" || items[0].TokenHash != "hash-must-not-leak" {
		t.Fatalf("expected stored service token metadata, got %#v", items)
	}
	encodedBytes, err := json.Marshal(items)
	if err != nil {
		t.Fatalf("marshal service account tokens: %v", err)
	}
	encoded := string(encodedBytes)
	if strings.Contains(encoded, "hash-must-not-leak") {
		t.Fatalf("token hash should not be visible after JSON encoding: %s", encoded)
	}
	if _, err := service.ListServiceAccountTokens(context.Background(), testPrincipal("developer")); err == nil {
		t.Fatalf("expected ai.gateway.manage to be required")
	}
}

func TestCreateAIClientRequiresManageAndPersistsClient(t *testing.T) {
	repo := &memoryGatewayRepository{}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"admin": {appaccess.PermAIGatewayManage},
		},
	}), nil, repo)

	item, err := service.CreateAIClient(context.Background(), testPrincipal("admin"), domainaigateway.AIClientInput{
		ID:     "codex",
		Name:   "Codex",
		Kind:   "ai_coding",
		Status: "active",
	})
	if err != nil {
		t.Fatalf("CreateAIClient returned error: %v", err)
	}
	if item.ID != "codex" || repo.aiClients["codex"].Name != "Codex" {
		t.Fatalf("expected persisted AI client, got %#v", item)
	}
}

func TestCreateAIClientPendingCreatesRegistrationApprovalRequest(t *testing.T) {
	repo := &memoryGatewayRepository{}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"admin": {appaccess.PermAIGatewayManage},
		},
	}), nil, repo)

	item, err := service.CreateAIClient(context.Background(), testPrincipal("admin"), domainaigateway.AIClientInput{
		ID:     "cursor-team",
		Name:   "Cursor Team",
		Kind:   "mcp_client",
		Status: "pending_approval",
	})
	if err != nil {
		t.Fatalf("CreateAIClient returned error: %v", err)
	}
	if item.Status != "pending_approval" {
		t.Fatalf("expected pending_approval status, got %#v", item)
	}
	if len(repo.approvalRequests) != 1 {
		t.Fatalf("expected registration approval request, got %#v", repo.approvalRequests)
	}
	request := repo.approvalRequests[0]
	if request.ToolName != "ai_gateway.ai_client.registration" || request.AIClientID != "cursor-team" || request.Status != "pending" {
		t.Fatalf("unexpected registration approval request: %#v", request)
	}
	if repo.aiClients["cursor-team"].Metadata["registrationApprovalRequestId"] != request.ID {
		t.Fatalf("expected client metadata to reference approval request, got %#v", repo.aiClients["cursor-team"].Metadata)
	}
}

func TestApproveAIClientRegistrationActivatesClient(t *testing.T) {
	repo := &memoryGatewayRepository{}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"admin": {appaccess.PermAIGatewayManage},
		},
	}), nil, repo)

	item, err := service.CreateAIClient(context.Background(), testPrincipal("admin"), domainaigateway.AIClientInput{
		ID:     "claude-code",
		Name:   "Claude Code",
		Kind:   "mcp_client",
		Status: "pending",
	})
	if err != nil {
		t.Fatalf("CreateAIClient returned error: %v", err)
	}
	requestID := fmt.Sprint(item.Metadata["registrationApprovalRequestId"])
	if requestID == "" {
		t.Fatalf("expected registration approval request id in metadata: %#v", item.Metadata)
	}

	result, err := service.ApproveApprovalRequest(context.Background(), testPrincipal("admin"), requestID, domainaigateway.ApprovalDecisionInput{Comment: "approved"})
	if err != nil {
		t.Fatalf("ApproveApprovalRequest returned error: %v", err)
	}
	if result.Request.Status != "executed" {
		t.Fatalf("expected executed registration approval, got %#v", result.Request)
	}
	if repo.aiClients["claude-code"].Status != "active" {
		t.Fatalf("expected approved client to be active, got %#v", repo.aiClients["claude-code"])
	}
	if result.Invocation != nil {
		t.Fatalf("registration approval should not replay a tool invocation, got %#v", result.Invocation)
	}
}

func TestCreateToolGrantDefaultsRiskFromKnownTool(t *testing.T) {
	repo := &memoryGatewayRepository{}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"admin": {appaccess.PermAIGatewayManage},
		},
	}), nil, repo)

	item, err := service.CreateToolGrant(context.Background(), testPrincipal("admin"), domainaigateway.ToolGrantInput{
		SubjectType: "role",
		SubjectID:   "developer",
		ToolName:    "delivery.actions.trigger",
		Effect:      "allow",
	})
	if err != nil {
		t.Fatalf("CreateToolGrant returned error: %v", err)
	}
	if item.RiskLevel != domainaigateway.RiskLevelExecute || !item.RequiresApproval {
		t.Fatalf("expected risk defaults from catalog tool, got %#v", item)
	}
}

func TestCreateToolGrantDefaultsRiskFromInjectedProvider(t *testing.T) {
	repo := &memoryGatewayRepository{}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"admin": {appaccess.PermAIGatewayManage},
		},
	}), nil, repo)
	service.SetCapabilityProviders(testCapabilityProvider{
		tools: []domainaigateway.ToolCapability{
			{Name: "custom.mutate", RiskLevel: domainaigateway.RiskLevelHigh, RequiresApproval: true, PermissionKeys: []string{appaccess.PermAIGatewayInvoke}},
		},
	})

	item, err := service.CreateToolGrant(context.Background(), testPrincipal("admin"), domainaigateway.ToolGrantInput{
		SubjectType: "role",
		SubjectID:   "developer",
		ToolName:    "custom.mutate",
		Effect:      "allow",
	})
	if err != nil {
		t.Fatalf("CreateToolGrant returned error: %v", err)
	}
	if item.RiskLevel != domainaigateway.RiskLevelHigh || !item.RequiresApproval {
		t.Fatalf("expected risk defaults from injected tool, got %#v", item)
	}
}

func TestCreateAccessPolicyAndSkillBindingRequireManage(t *testing.T) {
	repo := &memoryGatewayRepository{}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"admin": {appaccess.PermAIGatewayManage},
		},
	}), nil, repo)

	policy, err := service.CreateAccessPolicy(context.Background(), testPrincipal("admin"), domainaigateway.AccessPolicyInput{
		Name:         "Codex read-only delivery",
		SubjectType:  "ai_client",
		SubjectID:    "codex",
		Effect:       "allow",
		RiskLevels:   []domainaigateway.RiskLevel{domainaigateway.RiskLevelRead},
		ToolPatterns: []string{"delivery.*"},
	})
	if err != nil {
		t.Fatalf("CreateAccessPolicy returned error: %v", err)
	}
	if policy.ID == "" || policy.Effect != "allow" || !policy.Enabled || len(repo.accessPolicies) != 1 {
		t.Fatalf("expected persisted enabled access policy, got %#v", policy)
	}

	binding, err := service.CreateSkillBinding(context.Background(), testPrincipal("admin"), domainaigateway.SkillBindingInput{
		SubjectType:    "role",
		SubjectID:      "developer",
		SkillID:        "delivery-developer",
		CapabilityRefs: []string{"delivery.applications.list"},
	})
	if err != nil {
		t.Fatalf("CreateSkillBinding returned error: %v", err)
	}
	if binding.ID == "" || binding.SkillID != "delivery-developer" || !binding.Enabled || len(repo.skillBindings) != 1 {
		t.Fatalf("expected persisted skill binding, got %#v", binding)
	}
}

func TestCreateSkillBindingUsesInjectedProvider(t *testing.T) {
	repo := &memoryGatewayRepository{}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"admin": {appaccess.PermAIGatewayManage},
		},
	}), nil, repo)
	service.SetCapabilityProviders(testCapabilityProvider{
		skills: []domainaigateway.SkillCapability{
			{ID: "custom-skill", Name: "Custom Skill", CapabilityRefs: []string{"custom.echo"}, PermissionKeys: []string{appaccess.PermAIGatewayInvoke}},
		},
	})

	binding, err := service.CreateSkillBinding(context.Background(), testPrincipal("admin"), domainaigateway.SkillBindingInput{
		SubjectType: "role",
		SubjectID:   "developer",
		SkillID:     "custom-skill",
	})
	if err != nil {
		t.Fatalf("CreateSkillBinding returned error: %v", err)
	}
	if binding.SkillID != "custom-skill" || len(repo.skillBindings) != 1 {
		t.Fatalf("expected injected skill binding to persist, got %#v", binding)
	}
	if _, err := service.CreateSkillBinding(context.Background(), testPrincipal("admin"), domainaigateway.SkillBindingInput{
		SubjectType: "role",
		SubjectID:   "developer",
		SkillID:     "delivery-developer",
	}); err == nil || !strings.Contains(err.Error(), "unknown AI Gateway skill") {
		t.Fatalf("expected replaced default skill to be rejected, got %v", err)
	}
}

func TestGovernanceStatusSummarizesTokensAuditAndPolicyCoverage(t *testing.T) {
	now := time.Now().UTC()
	expiredAt := now.Add(-time.Hour)
	expiringAt := now.Add(48 * time.Hour)
	staleUsedAt := now.Add(-120 * 24 * time.Hour)
	approvalDueSoonAt := now.Add(45 * time.Minute)
	repo := &memoryGatewayRepository{
		personalTokens: []domainaigateway.PersonalAccessToken{
			{ID: "pat-expired", UserID: "user-1", Name: "old", TokenPrefix: "soha_pat_old", ExpiresAt: &expiredAt, CreatedAt: now.Add(-30 * 24 * time.Hour)},
			{ID: "pat-soon", UserID: "user-1", Name: "soon", TokenPrefix: "soha_pat_soon", ExpiresAt: &expiringAt, CreatedAt: now.Add(-2 * time.Hour)},
		},
		serviceAccountTokens: []domainaigateway.ServiceAccountToken{
			{ID: "sat-stale", ServiceAccountID: "ci", Name: "ci-token", TokenPrefix: "soha_sat_ci", LastUsedAt: &staleUsedAt, CreatedAt: now.Add(-180 * 24 * time.Hour)},
		},
		aiClients: map[string]domainaigateway.AIClient{
			"codex": {ID: "codex", Name: "Codex", Status: "active", Metadata: map[string]any{"registrationApprovalRequired": true}},
		},
		accessPolicies: []domainaigateway.AccessPolicy{
			{
				ID:          "policy-1",
				Enabled:     true,
				SubjectType: "role",
				SubjectID:   "developer",
				Effect:      "allow",
				Conditions: map[string]any{
					"budget":          map[string]any{"daily": 100},
					"rateLimit":       map[string]any{"maxCallsPerMinute": 60},
					"redactionPolicy": map[string]any{"mode": "strict"},
				},
			},
		},
		toolGrants:    []domainaigateway.ToolGrant{{ID: "grant-1", SubjectType: "role", SubjectID: "developer", ToolName: "k8s.*", Effect: "allow"}},
		skillBindings: []domainaigateway.SkillBinding{{ID: "binding-1", SubjectType: "role", SubjectID: "developer", SkillID: "k8s-sre", Enabled: true}},
		auditLogs: []domainaigateway.AuditLog{
			{ID: "audit-1", ActorType: "user", ActorID: "user-1", AIClientID: "codex", ToolName: "k8s.pods.logs", RiskLevel: domainaigateway.RiskLevelRead, Action: "ai_gateway.tool.invoke", Result: "deny", Metadata: map[string]any{"redaction": map[string]any{"totalMatches": 3, "fieldMatches": 1, "sensitiveKeyMatches": 1, "secretClassifierMatches": 1, "targets": []any{"input"}, "fieldPaths": []any{"metadata.apiToken"}, "matchTypes": []any{"field", "sensitive_key", "secret_classifier"}, "classifiers": []any{"openai"}, "policyIds": []any{"policy-1"}}}, CreatedAt: now.Add(-time.Hour)},
			{ID: "audit-2", ActorType: "user", ActorID: "user-1", AIClientID: "codex", ToolName: "delivery.actions.trigger", RiskLevel: domainaigateway.RiskLevelRead, Action: "ai_gateway.tool.invoke", Result: "failure", Metadata: map[string]any{"redaction": map[string]any{"totalMatches": 2, "valuePatternMatches": 1, "structuredSecretMatches": 1, "targets": []string{"output"}, "fieldPaths": []string{"output.bundle.secret"}, "matchTypes": []string{"value_pattern", "structured_secret"}, "classifiers": []string{"github"}, "policyIds": []string{"policy-1"}}}, CreatedAt: now.Add(-30 * time.Minute)},
			{ID: "audit-3", ActorType: "user", ActorID: "user-1", AIClientID: "codex", ToolName: "k8s.pods.logs", RiskLevel: domainaigateway.RiskLevelRead, Action: "ai_gateway.tool.invoke", Result: "deny", CreatedAt: now.Add(-15 * time.Minute)},
		},
		approvalRequests: []domainaigateway.ApprovalRequest{
			{ID: "approval-due-soon", Status: "pending", ActorType: "user", ActorID: "user-1", ToolName: "delivery.actions.trigger", RiskLevel: domainaigateway.RiskLevelExecute, ExpiresAt: &approvalDueSoonAt, CreatedAt: now.Add(-2 * time.Hour)},
			{ID: "approval-stale", Status: "pending", ActorType: "user", ActorID: "user-1", ToolName: "delivery.actions.trigger", RiskLevel: domainaigateway.RiskLevelExecute, CreatedAt: now.Add(-26 * time.Hour)},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"admin": {appaccess.PermAIGatewayManage},
		},
	}), nil, repo)

	status, err := service.GovernanceStatus(context.Background(), testPrincipal("admin"), domainaigateway.GovernanceStatusRequest{WindowHours: 24})
	if err != nil {
		t.Fatalf("GovernanceStatus returned error: %v", err)
	}
	assertGovernanceTokenAndPolicySummary(t, status)
	assertGovernanceRedactionSummary(t, status)
	assertGovernanceRecommendations(t, status)
}

func TestGovernanceApprovalSummaryTracksOverdueDueSoonAndStale(t *testing.T) {
	now := time.Now().UTC()
	overdueAt := now.Add(-5 * time.Minute)
	dueSoonAt := now.Add(30 * time.Minute)
	laterAt := now.Add(2 * time.Hour)

	summary := governanceApprovalSummary(now, []domainaigateway.ApprovalRequest{
		{ID: "approval-overdue", Status: "pending", ExpiresAt: &overdueAt, CreatedAt: now.Add(-2 * time.Hour)},
		{ID: "approval-due", Status: "pending", ExpiresAt: &dueSoonAt, CreatedAt: now.Add(-3 * time.Hour)},
		{ID: "approval-stale", Status: "pending", ExpiresAt: &laterAt, CreatedAt: now.Add(-25 * time.Hour)},
		{ID: "approval-executed", Status: "executed", ExpiresAt: &overdueAt, CreatedAt: now.Add(-25 * time.Hour)},
	})

	if summary.Pending != 3 || summary.Overdue != 1 || summary.DueSoon != 1 || summary.StalePending != 1 {
		t.Fatalf("unexpected approval SLA summary: %#v", summary)
	}
	if summary.NextDueRequestID != "approval-due" || summary.OldestPendingRequestID != "approval-stale" || summary.OldestPendingHours < 24 {
		t.Fatalf("unexpected approval SLA ids or age: %#v", summary)
	}
	if !slices.Contains(summary.OverdueRequestIDs, "approval-overdue") || !slices.Contains(summary.DueSoonRequestIDs, "approval-due") || !slices.Contains(summary.StalePendingRequestIDs, "approval-stale") {
		t.Fatalf("unexpected approval SLA request ids: %#v", summary)
	}
}

func TestGovernancePolicyCoverageIgnoresInactiveControls(t *testing.T) {
	now := time.Now().UTC()
	expiredAt := now.Add(-time.Hour)

	coverage := governancePolicyCoverage(now,
		[]domainaigateway.AccessPolicy{
			{
				ID:             "policy-disabled-governance",
				Enabled:        false,
				Effect:         "allow",
				ResourceScopes: map[string]any{"applicationId": "app-1"},
				Conditions: map[string]any{
					"budget":          map[string]any{"maxCallsPerDay": 10},
					"rateLimit":       map[string]any{"maxCallsPerMinute": 1},
					"redactionPolicy": map[string]any{"mode": "strict"},
				},
			},
			{ID: "policy-active-empty", Enabled: true, Effect: "allow"},
		},
		[]domainaigateway.ToolGrant{
			{ID: "grant-expired-scoped", Effect: "allow", ToolName: "delivery.*", ResourceScopes: map[string]any{"applicationId": "app-1"}, ExpiresAt: &expiredAt},
			{ID: "grant-active-unscoped", Effect: "allow", ToolName: "k8s.*"},
		},
		[]domainaigateway.SkillBinding{
			{ID: "binding-enabled", Enabled: true},
			{ID: "binding-disabled", Enabled: false},
		},
	)

	if coverage.AccessPolicies != 2 || coverage.ActiveAccessPolicies != 1 || coverage.ToolGrants != 2 || coverage.ActiveToolGrants != 1 || coverage.SkillBindings != 2 || coverage.ActiveSkillBindings != 1 {
		t.Fatalf("unexpected active policy coverage counts: %#v", coverage)
	}
	if coverage.BudgetPolicies != 0 || coverage.RateLimitPolicies != 0 || coverage.RedactionPolicies != 0 || coverage.ResourceScopedAccessPolicies != 0 || coverage.ResourceScopedToolGrants != 0 {
		t.Fatalf("inactive controls should not configure governance coverage: %#v", coverage)
	}
	if coverage.BudgetState != "not_configured" || coverage.RateLimitState != "not_configured" || coverage.RedactionPolicyState != "built_in" || coverage.ResourceScopeState != "not_configured" {
		t.Fatalf("inactive controls should not mark coverage configured: %#v", coverage)
	}
}

func TestGovernancePolicyCoverageCountsOutputRedactionPolicy(t *testing.T) {
	now := time.Now().UTC()
	coverage := governancePolicyCoverage(now,
		[]domainaigateway.AccessPolicy{
			{
				ID:          "policy-output-redaction",
				Enabled:     true,
				Effect:      "allow",
				SubjectType: "role",
				SubjectID:   "developer",
				ToolPatterns: []string{
					"delivery.*",
				},
				Conditions: map[string]any{
					"outputRedactionPolicy": map[string]any{
						"mode":   "sanitize",
						"fields": []any{"result.metadata.token"},
					},
				},
			},
		},
		nil,
		nil,
	)

	if coverage.RedactionPolicies != 1 || coverage.RedactionPolicyState != "configured" {
		t.Fatalf("expected output redaction policy to configure governance coverage, got %#v", coverage)
	}
}

func TestGovernanceStatusReportsResourceScopeCoverageHealth(t *testing.T) {
	governanceConditions := map[string]any{
		"budget":          map[string]any{"dailyInvocations": 100},
		"rateLimit":       map[string]any{"maxCallsPerMinute": 10},
		"redactionPolicy": map[string]any{"mode": "sanitize"},
	}
	newService := func(resourceScopes map[string]any) *Service {
		repo := &memoryGatewayRepository{
			aiClients: map[string]domainaigateway.AIClient{
				"codex": {ID: "codex", Name: "Codex", Status: "active", Metadata: map[string]any{"registrationApprovalRequired": true}},
			},
			accessPolicies: []domainaigateway.AccessPolicy{
				{
					ID:             "policy-read",
					Enabled:        true,
					SubjectType:    "role",
					SubjectID:      "developer",
					AIClientID:     "codex",
					Effect:         "allow",
					ToolPatterns:   []string{"k8s.pods.logs"},
					ResourceScopes: mapValue(resourceScopes),
					Conditions:     governanceConditions,
				},
			},
		}
		return newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
			matrix: map[string][]string{
				"admin": {appaccess.PermAIGatewayManage},
			},
		}), nil, repo)
	}

	status, err := newService(nil).GovernanceStatus(context.Background(), testPrincipal("admin"), domainaigateway.GovernanceStatusRequest{WindowHours: 24})
	if err != nil {
		t.Fatalf("GovernanceStatus returned error: %v", err)
	}
	var scopeCoverageCheck domainaigateway.GovernanceHealthCheck
	for _, check := range status.Health.Checks {
		if check.Name == "resource_scope_coverage" {
			scopeCoverageCheck = check
			break
		}
	}
	if scopeCoverageCheck.Status != "degraded" || scopeCoverageCheck.Count != 0 {
		t.Fatalf("expected degraded resource-scope coverage health check, got %#v", status.Health.Checks)
	}
	if status.PolicyCoverage.ResourceScopeState != "not_configured" {
		t.Fatalf("expected resource scope coverage to be absent, got %#v", status.PolicyCoverage)
	}
	if !slices.ContainsFunc(status.Recommendations, func(item string) bool {
		return strings.Contains(item, "resourceScopes") && strings.Contains(item, "cross-environment")
	}) {
		t.Fatalf("expected general resource scope recommendation, got %#v", status.Recommendations)
	}
	if !slices.ContainsFunc(status.RecommendationActions, func(item domainaigateway.GovernanceRecommendationAction) bool {
		return item.Type == "resource_scope_coverage" && item.Action == "create_resource_scope_guardrail_policy" && item.Metadata["policyTemplate"] == "resource_scopes"
	}) {
		t.Fatalf("expected resource scope coverage recommendation action, got %#v", status.RecommendationActions)
	}

	scopedStatus, err := newService(map[string]any{"applicationId": "app-1"}).GovernanceStatus(context.Background(), testPrincipal("admin"), domainaigateway.GovernanceStatusRequest{WindowHours: 24})
	if err != nil {
		t.Fatalf("GovernanceStatus returned error: %v", err)
	}
	scopeCoverageCheck = domainaigateway.GovernanceHealthCheck{}
	for _, check := range scopedStatus.Health.Checks {
		if check.Name == "resource_scope_coverage" {
			scopeCoverageCheck = check
			break
		}
	}
	if scopeCoverageCheck.Status != "healthy" || scopeCoverageCheck.Count != 1 {
		t.Fatalf("expected healthy resource-scope coverage health check, got %#v", scopedStatus.Health.Checks)
	}
	if scopedStatus.PolicyCoverage.ResourceScopeState != "configured" || scopedStatus.PolicyCoverage.ResourceScopedAccessPolicies != 1 {
		t.Fatalf("expected configured resource scope coverage, got %#v", scopedStatus.PolicyCoverage)
	}
}

func TestGovernanceStatusMergesRelayHealthChecks(t *testing.T) {
	now := time.Now().UTC()
	baseStatus, err := newTestServiceWithRelay(governanceAdminPermissions(), nil, governanceHealthyControlRepo(), nil).GovernanceStatus(context.Background(), testPrincipal("admin"), domainaigateway.GovernanceStatusRequest{WindowHours: 24})
	if err != nil {
		t.Fatalf("baseline GovernanceStatus returned error: %v", err)
	}
	if baseStatus.Health.Status != "healthy" {
		t.Fatalf("expected healthy baseline governance status, got %#v", baseStatus.Health)
	}
	if slices.ContainsFunc(baseStatus.Health.Checks, func(check domainaigateway.GovernanceHealthCheck) bool {
		return strings.HasPrefix(check.Name, "relay_")
	}) {
		t.Fatalf("expected governance status without relay repo to omit relay checks, got %#v", baseStatus.Health.Checks)
	}
	if _, ok := baseStatus.Metadata["relayHealthMerged"]; ok {
		t.Fatalf("expected governance status without relay repo to omit relay metadata, got %#v", baseStatus.Metadata)
	}
	relayRepo := &relayTestRepository{
		upstreams: []domainaigateway.LLMUpstream{
			{ID: "upstream-ok", Name: "OpenAI primary", ProviderKind: "openai", Status: "active"},
			{ID: "upstream-open", Name: "OpenAI fallback", ProviderKind: "openai", Status: "active", Health: map[string]any{"circuitOpenUntil": now.Add(time.Hour).Format(time.RFC3339)}},
			{ID: "upstream-degraded", Name: "Anthropic degraded", ProviderKind: "anthropic", Status: "degraded"},
		},
		callLogs: []domainaigateway.LLMCallLog{
			{ID: "call-success", Status: "success", PublicModel: "gpt-public", UpstreamID: "upstream-ok", CreatedAt: now.Add(-2 * time.Minute)},
			{ID: "call-failure", Status: "failure", ErrorCode: "upstream_5xx", PublicModel: "gpt-public", UpstreamID: "upstream-open", CreatedAt: now.Add(-time.Minute)},
		},
	}
	status, err := newTestServiceWithRelay(governanceAdminPermissions(), nil, governanceHealthyControlRepo(), relayRepo).GovernanceStatus(context.Background(), testPrincipal("admin"), domainaigateway.GovernanceStatusRequest{WindowHours: 24})
	if err != nil {
		t.Fatalf("GovernanceStatus returned error: %v", err)
	}
	if status.Health.Status != "degraded" {
		t.Fatalf("expected relay warnings to degrade governance health, got %#v", status.Health)
	}
	if status.Metadata["relayHealthMerged"] != true || status.Metadata["relayHealthSampleLimit"] != 500 {
		t.Fatalf("expected relay health metadata, got %#v", status.Metadata)
	}
	upstreamCheck := governanceHealthCheckByName(t, status.Health.Checks, "relay_upstreams")
	if upstreamCheck.Status != "degraded" || upstreamCheck.Count != 3 {
		t.Fatalf("unexpected relay upstream health check: %#v", upstreamCheck)
	}
	circuitCheck := governanceHealthCheckByName(t, status.Health.Checks, "relay_circuit_breakers")
	if circuitCheck.Status != "degraded" || circuitCheck.Count != 1 {
		t.Fatalf("unexpected relay circuit health check: %#v", circuitCheck)
	}
	modelCallsCheck := governanceHealthCheckByName(t, status.Health.Checks, "relay_model_calls")
	if modelCallsCheck.Status != "degraded" || modelCallsCheck.Count != 1 {
		t.Fatalf("unexpected relay model call health check: %#v", modelCallsCheck)
	}
}

func TestGovernanceStatusReportsCriticalRelayAvailability(t *testing.T) {
	relayRepo := &relayTestRepository{
		upstreams: []domainaigateway.LLMUpstream{
			{ID: "upstream-disabled", Name: "disabled", ProviderKind: "openai", Status: "disabled"},
		},
	}
	status, err := newTestServiceWithRelay(governanceAdminPermissions(), nil, governanceHealthyControlRepo(), relayRepo).GovernanceStatus(context.Background(), testPrincipal("admin"), domainaigateway.GovernanceStatusRequest{WindowHours: 24})
	if err != nil {
		t.Fatalf("GovernanceStatus returned error: %v", err)
	}
	if status.Health.Status != "critical" {
		t.Fatalf("expected unavailable relay upstreams to make governance critical, got %#v", status.Health)
	}
	upstreamCheck := governanceHealthCheckByName(t, status.Health.Checks, "relay_upstreams")
	if upstreamCheck.Status != "critical" || upstreamCheck.Count != 1 {
		t.Fatalf("unexpected relay upstream health check: %#v", upstreamCheck)
	}
	modelCallsCheck := governanceHealthCheckByName(t, status.Health.Checks, "relay_model_calls")
	if modelCallsCheck.Status != "healthy" {
		t.Fatalf("expected empty relay call sample to remain healthy, got %#v", modelCallsCheck)
	}
}

func TestGovernanceStatusRelayHealthUsesWindow(t *testing.T) {
	now := time.Now().UTC()
	relayRepo := &relayTestRepository{
		upstreams: []domainaigateway.LLMUpstream{
			{ID: "upstream-ok", Name: "OpenAI primary", ProviderKind: "openai", Status: "active"},
		},
		callLogs: []domainaigateway.LLMCallLog{
			{ID: "old-failure", Status: "failure", ErrorCode: "upstream_5xx", PublicModel: "gpt-public", UpstreamID: "upstream-ok", CreatedAt: now.Add(-48 * time.Hour)},
			{ID: "recent-success", Status: "success", PublicModel: "gpt-public", UpstreamID: "upstream-ok", CreatedAt: now.Add(-time.Minute)},
		},
	}
	status, err := newTestServiceWithRelay(governanceAdminPermissions(), nil, governanceHealthyControlRepo(), relayRepo).GovernanceStatus(context.Background(), testPrincipal("admin"), domainaigateway.GovernanceStatusRequest{WindowHours: 24})
	if err != nil {
		t.Fatalf("GovernanceStatus returned error: %v", err)
	}
	if status.Health.Status != "healthy" {
		t.Fatalf("expected old relay failure outside window to be ignored, got %#v", status.Health)
	}
	modelCallsCheck := governanceHealthCheckByName(t, status.Health.Checks, "relay_model_calls")
	if modelCallsCheck.Status != "healthy" || modelCallsCheck.Count != 0 {
		t.Fatalf("unexpected relay model call health check: %#v", modelCallsCheck)
	}
}

func governanceHealthyControlRepo() *memoryGatewayRepository {
	return &memoryGatewayRepository{
		aiClients: map[string]domainaigateway.AIClient{
			"codex": {ID: "codex", Name: "Codex", Status: "active", Metadata: map[string]any{"registrationApprovalRequired": true}},
		},
		accessPolicies: []domainaigateway.AccessPolicy{
			{
				ID:             "policy-read",
				Enabled:        true,
				SubjectType:    "role",
				SubjectID:      "developer",
				AIClientID:     "codex",
				Effect:         "allow",
				ToolPatterns:   []string{"k8s.pods.logs"},
				ResourceScopes: map[string]any{"applicationId": "app-1"},
				Conditions: map[string]any{
					"budget":          map[string]any{"dailyInvocations": 100},
					"rateLimit":       map[string]any{"maxCallsPerMinute": 10},
					"redactionPolicy": map[string]any{"mode": "sanitize"},
				},
			},
		},
	}
}

func governanceAdminPermissions() *appaccess.PermissionResolver {
	return appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"admin": {appaccess.PermAIGatewayManage},
		},
	})
}

func governanceHealthCheckByName(t *testing.T, checks []domainaigateway.GovernanceHealthCheck, name string) domainaigateway.GovernanceHealthCheck {
	t.Helper()
	for _, check := range checks {
		if check.Name == name {
			return check
		}
	}
	t.Fatalf("missing governance health check %s in %#v", name, checks)
	return domainaigateway.GovernanceHealthCheck{}
}

func TestGovernanceStatusFlagsUnguardedHighRiskAllows(t *testing.T) {
	now := time.Now().UTC()
	expiredAt := now.Add(-time.Hour)
	governanceConditions := map[string]any{
		"budget":          map[string]any{"dailyInvocations": 100},
		"rateLimit":       map[string]any{"maxCallsPerMinute": 10},
		"redactionPolicy": map[string]any{"mode": "sanitize"},
	}
	repo := &memoryGatewayRepository{
		aiClients: map[string]domainaigateway.AIClient{
			"codex": {ID: "codex", Name: "Codex", Status: "active", Metadata: map[string]any{"registrationApprovalRequired": true}},
		},
		accessPolicies: []domainaigateway.AccessPolicy{
			{
				ID:           "policy-risk-open",
				Enabled:      true,
				SubjectType:  "role",
				SubjectID:    "developer",
				AIClientID:   "codex",
				Effect:       "allow",
				ToolPatterns: []string{"delivery.*"},
				Conditions:   governanceConditions,
			},
			{
				ID:             "policy-risk-safe",
				Enabled:        true,
				SubjectType:    "role",
				SubjectID:      "developer",
				Effect:         "allow",
				ToolPatterns:   []string{"delivery.*"},
				ApprovalPolicy: map[string]any{"strategy": "require_approval"},
			},
			{
				ID:           "policy-catalog-guarded",
				Enabled:      true,
				SubjectType:  "role",
				SubjectID:    "developer",
				Effect:       "allow",
				ToolPatterns: []string{"delivery.actions.trigger"},
			},
			{
				ID:           "policy-disabled",
				Enabled:      false,
				SubjectType:  "role",
				SubjectID:    "developer",
				Effect:       "allow",
				ToolPatterns: []string{"delivery.*"},
			},
		},
		toolGrants: []domainaigateway.ToolGrant{
			{ID: "grant-risk-open", SubjectType: "role", SubjectID: "developer", AIClientID: "codex", ToolName: "delivery.*", Effect: "allow"},
			{ID: "grant-risk-safe", SubjectType: "role", SubjectID: "developer", ToolName: "delivery.*", Effect: "allow", RequiresApproval: true},
			{ID: "grant-catalog-guarded", SubjectType: "role", SubjectID: "developer", ToolName: "delivery.actions.trigger", Effect: "allow"},
			{ID: "grant-expired", SubjectType: "role", SubjectID: "developer", ToolName: "delivery.*", Effect: "allow", ExpiresAt: &expiredAt},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"admin": {appaccess.PermAIGatewayManage},
		},
	}), nil, repo)

	status, err := service.GovernanceStatus(context.Background(), testPrincipal("admin"), domainaigateway.GovernanceStatusRequest{WindowHours: 24})
	if err != nil {
		t.Fatalf("GovernanceStatus returned error: %v", err)
	}

	assertHighRiskGovernanceFindings(t, status)
}

func TestGovernanceStatusUsesInjectedCapabilityProvider(t *testing.T) {
	repo := &memoryGatewayRepository{
		accessPolicies: []domainaigateway.AccessPolicy{
			{
				ID:          "policy-custom-open",
				Enabled:     true,
				SubjectType: "role",
				SubjectID:   "developer",
				Effect:      "allow",
				SkillIDs:    []string{"custom-skill"},
			},
		},
		toolGrants: []domainaigateway.ToolGrant{
			{ID: "grant-custom-open", SubjectType: "role", SubjectID: "developer", ToolName: "custom.*", Effect: "allow"},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"admin": {appaccess.PermAIGatewayManage},
		},
	}), nil, repo)
	service.SetCapabilityProviders(testCapabilityProvider{
		tools: []domainaigateway.ToolCapability{
			{Name: "custom.mutate", RiskLevel: domainaigateway.RiskLevelHigh, PermissionKeys: []string{appaccess.PermAIGatewayInvoke}},
		},
		skills: []domainaigateway.SkillCapability{
			{ID: "custom-skill", Name: "Custom Skill", CapabilityRefs: []string{"custom.mutate"}},
		},
	})

	status, err := service.GovernanceStatus(context.Background(), testPrincipal("admin"), domainaigateway.GovernanceStatusRequest{WindowHours: 24})
	if err != nil {
		t.Fatalf("GovernanceStatus returned error: %v", err)
	}
	if !slices.ContainsFunc(status.Anomalies, func(item domainaigateway.GovernanceFinding) bool {
		return item.Type == "high_risk_allow_without_approval" && item.PolicyID == "policy-custom-open" && item.RiskLevel == domainaigateway.RiskLevelHigh
	}) {
		t.Fatalf("expected injected skill policy high-risk finding, got %#v", status.Anomalies)
	}
	if !slices.ContainsFunc(status.Anomalies, func(item domainaigateway.GovernanceFinding) bool {
		return item.Type == "high_risk_grant_without_approval" && item.GrantID == "grant-custom-open" && item.RiskLevel == domainaigateway.RiskLevelHigh
	}) {
		t.Fatalf("expected injected tool grant high-risk finding, got %#v", status.Anomalies)
	}
}

func TestGatewayGovernanceStatusRuntimeTool(t *testing.T) {
	repo := governanceHealthyControlRepo()
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"admin": {appaccess.PermAIGatewayView, appaccess.PermAIGatewayInvoke, appaccess.PermAIGatewayManage},
		},
	}), nil, repo)
	principal := testPrincipal("admin")

	manifest, err := service.Capabilities(context.Background(), principal, domainaigateway.ManifestRequest{})
	if err != nil {
		t.Fatalf("Capabilities returned error: %v", err)
	}
	if !slices.ContainsFunc(manifest.Tools, func(item domainaigateway.ToolCapability) bool {
		return item.Name == "gateway.governance.status" &&
			item.RiskLevel == domainaigateway.RiskLevelRead &&
			slices.Contains(item.PermissionKeys, appaccess.PermAIGatewayInvoke) &&
			slices.Contains(item.PermissionKeys, appaccess.PermAIGatewayManage)
	}) {
		t.Fatalf("expected gateway.governance.status runtime tool in manifest, got %#v", manifest.Tools)
	}

	result, err := service.InvokeTool(context.Background(), principal, domainaigateway.ToolInvocationRequest{
		ToolName: "gateway.governance.status",
		Input:    map[string]any{"windowHours": 12},
	})
	if err != nil {
		t.Fatalf("InvokeTool returned error: %v", err)
	}
	status, ok := result.Output.(domainaigateway.GovernanceStatus)
	if !ok {
		t.Fatalf("expected GovernanceStatus output, got %T", result.Output)
	}
	if status.WindowHours != 12 {
		t.Fatalf("WindowHours = %d, want 12", status.WindowHours)
	}
	if result.RelatedIDs["windowHours"] != 12 || result.RelatedIDs["healthStatus"] != status.Health.Status {
		t.Fatalf("unexpected related ids: %#v", result.RelatedIDs)
	}
	if result.RiskLevel != domainaigateway.RiskLevelRead || result.RequiresApproval {
		t.Fatalf("unexpected invocation posture: %#v", result)
	}
}

func TestGatewayGovernanceListRuntimeTools(t *testing.T) {
	repo := newGatewayGovernanceListRepository(time.Now().UTC())
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"admin": {
				appaccess.PermAIGatewayView,
				appaccess.PermAIGatewayInvoke,
				appaccess.PermAIGatewayManage,
			},
		},
	}), &captureAuditRecorder{}, repo)
	principal := testPrincipal("admin")

	assertGatewayGovernanceListToolManifest(t, service, principal)

	tests := []struct {
		name     string
		toolName string
		input    map[string]any
		check    func(t *testing.T, result domainaigateway.ToolInvocationResult)
	}{
		{
			name:     "clients list",
			toolName: "gateway.clients.list",
			input:    map[string]any{"status": "active"},
			check:    assertGatewayClientsListResult,
		},
		{
			name:     "tokens list redacts hashes",
			toolName: "gateway.tokens.list",
			input:    map[string]any{"userId": "user-2"},
			check:    assertGatewayTokensListResult,
		},
		{
			name:     "service accounts list",
			toolName: "gateway.service_accounts.list",
			input:    map[string]any{"status": "active"},
			check:    assertGatewayServiceAccountsListResult,
		},
		{
			name:     "tool grants list",
			toolName: "gateway.tool_grants.list",
			input:    map[string]any{"subjectType": "role", "subjectId": "auditor"},
			check:    assertGatewayToolGrantsListResult,
		},
		{
			name:     "access policies list",
			toolName: "gateway.access_policies.list",
			input:    map[string]any{"subjectType": "role", "subjectId": "auditor"},
			check:    assertGatewayAccessPoliciesListResult,
		},
		{
			name:     "skill bindings list",
			toolName: "gateway.skill_bindings.list",
			input:    map[string]any{"skillId": "k8s-sre"},
			check:    assertGatewaySkillBindingsListResult,
		},
		{
			name:     "approvals list redacts input",
			toolName: "gateway.approvals.list",
			input:    map[string]any{"status": "pending"},
			check:    assertGatewayApprovalsListResult,
		},
		{
			name:     "audit logs list redacts metadata",
			toolName: "gateway.audit_logs.list",
			input:    map[string]any{"approvalRequestId": "approval-1", "limit": 10},
			check:    assertGatewayAuditLogsListResult,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := service.InvokeTool(context.Background(), principal, domainaigateway.ToolInvocationRequest{
				ToolName: tt.toolName,
				Input:    tt.input,
			})
			if err != nil {
				t.Fatalf("InvokeTool returned error: %v", err)
			}
			if result.RiskLevel != domainaigateway.RiskLevelRead || result.RequiresApproval {
				t.Fatalf("unexpected invocation posture: %#v", result)
			}
			tt.check(t, result)
		})
	}
}

func TestGatewayManifestAndRelayRuntimeTools(t *testing.T) {
	now := time.Now().UTC()
	repo := &memoryGatewayRepository{}
	relayRepo := &relayTestRepository{
		upstreams: []domainaigateway.LLMUpstream{
			{
				ID:               "upstream-openai",
				Name:             "OpenAI",
				ProviderKind:     "openai",
				BaseURL:          "https://user:password@example.com/v1?token=secret",
				APIKeyCiphertext: "ciphertext-should-not-leak",
				APIKeyPrefix:     "sk-live",
				Status:           "active",
				DefaultHeaders:   map[string]any{"Authorization": "Bearer secret", "X-Trace": "trace"},
				Metadata:         map[string]any{"credential": "secret", "region": "us"},
				CreatedAt:        now,
				UpdatedAt:        now,
			},
			{
				ID:           "upstream-disabled",
				Name:         "Disabled",
				ProviderKind: "openai",
				BaseURL:      "https://disabled.example/v1",
				Status:       "disabled",
				CreatedAt:    now,
				UpdatedAt:    now,
			},
		},
		routes: []domainaigateway.LLMModelRoute{
			{
				ID:              "route-gpt",
				PublicModel:     "gpt-4.1",
				ProviderKind:    "openai",
				UpstreamID:      "upstream-openai",
				UpstreamModel:   "gpt-4.1",
				RouteGroup:      "primary",
				Priority:        1,
				Weight:          100,
				Enabled:         true,
				TransformPolicy: map[string]any{"apiKey": "secret", "mode": "openai"},
				Metadata:        map[string]any{"token": "secret", "tier": "prod"},
				CreatedAt:       now,
				UpdatedAt:       now,
			},
			{
				ID:            "route-disabled",
				PublicModel:   "gpt-disabled",
				ProviderKind:  "openai",
				UpstreamID:    "upstream-disabled",
				UpstreamModel: "gpt-disabled",
				RouteGroup:    "backup",
				Enabled:       false,
				CreatedAt:     now,
				UpdatedAt:     now,
			},
		},
	}
	service := newTestServiceWithRelay(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"admin": {
				appaccess.PermAIGatewayView,
				appaccess.PermAIGatewayInvoke,
				appaccess.PermAIGatewayRelayView,
			},
		},
	}), &captureAuditRecorder{}, repo, relayRepo)
	principal := testPrincipal("admin")

	assertRelayRuntimeToolManifest(t, service, principal)
	assertManifestRuntimeTool(t, service, principal)
	assertRelayUpstreamsRuntimeTool(t, service, principal)
	assertRelayRoutesRuntimeTool(t, service, principal)
}

func TestGatewayApprovalRelayCallAndCacheRuntimeTools(t *testing.T) {
	now := time.Now().UTC()
	old := now.Add(-2 * time.Hour)
	repo := &memoryGatewayRepository{
		approvalRequests: []domainaigateway.ApprovalRequest{
			{
				ID:         "approval-reject",
				Status:     "pending",
				Strategy:   "require_approval",
				ActorType:  "user",
				ActorID:    "user-2",
				AIClientID: "codex",
				SkillID:    "delivery-tester",
				ToolName:   "delivery.actions.trigger",
				RiskLevel:  domainaigateway.RiskLevelExecute,
				ToolInput:  map[string]any{"token": "secret-token", "applicationId": "app-1"},
				RelatedIDs: map[string]any{"approvalRequestId": "approval-reject"},
				Summary:    "pending token=secret",
				CreatedAt:  now,
				UpdatedAt:  now,
			},
		},
	}
	relayRepo := &relayTestRepository{
		callLogs: []domainaigateway.LLMCallLog{
			{
				ID:           "call-1",
				ActorType:    "user",
				ActorID:      "user-2",
				TokenID:      "pat-1",
				TokenPrefix:  "soha_pat_secret",
				TokenKind:    "personal_access_token",
				AIClientID:   "codex",
				PublicModel:  "gpt-4.1",
				UpstreamID:   "upstream-openai",
				ProviderKind: "openai",
				Status:       "success",
				CacheStatus:  "hit",
				ErrorMessage: "token=secret",
				RouteTrace:   map[string]any{"Authorization": "Bearer secret", "route": "primary"},
				SourceIP:     "203.0.113.10",
				UserAgent:    "secret-agent",
				Metadata:     map[string]any{"apiKey": "secret", "region": "us"},
				TotalTokens:  42,
				CreatedAt:    now,
			},
			{
				ID:           "call-other",
				PublicModel:  "gpt-other",
				ProviderKind: "openai",
				Status:       "failure",
				CreatedAt:    now,
			},
		},
		caches: []domainaigateway.LLMCacheEntry{
			{ID: "cache-1", CacheKey: "cache-1", PublicModel: "gpt-4.1", UpstreamID: "upstream-openai", UpdatedAt: old},
			{ID: "cache-2", CacheKey: "cache-2", PublicModel: "gpt-other", UpstreamID: "upstream-openai", UpdatedAt: old},
		},
	}
	repo.approvalRequests = append(repo.approvalRequests, domainaigateway.ApprovalRequest{
		ID:         "approval-cache-purge",
		Status:     "pending",
		Strategy:   "require_approval",
		ActorType:  "user",
		ActorID:    "relay-user",
		ActorRoles: []string{"admin"},
		AIClientID: "codex",
		ToolName:   "gateway.relay.cache.purge",
		RiskLevel:  domainaigateway.RiskLevelExecute,
		ToolInput:  map[string]any{"publicModel": "gpt-4.1"},
		RelatedIDs: map[string]any{"approvalRequestId": "approval-cache-purge"},
		Summary:    "purge relay cache",
		CreatedAt:  now,
		UpdatedAt:  now,
	})
	service := newTestServiceWithRelay(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"admin": {
				appaccess.PermAIGatewayView,
				appaccess.PermAIGatewayInvoke,
				appaccess.PermAIGatewayManage,
				appaccess.PermAIGatewayRelayView,
				appaccess.PermAIGatewayRelayManage,
			},
			"relay-viewer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermAIGatewayRelayView,
			},
		},
	}), &captureAuditRecorder{}, repo, relayRepo)

	assertApprovalRelayRuntimeToolManifest(t, service)
	assertRelayModelCallsRuntimeTool(t, service)
	assertRelayCachePurgeRequiresApproval(t, service, relayRepo)
	assertApprovalDecisionRuntimeTool(t, service)
	assertApprovedRelayCachePurge(t, service, relayRepo)
}

func TestGovernanceStatusFlagsUnscopedHighRiskAllows(t *testing.T) {
	now := time.Now().UTC()
	expiredAt := now.Add(-time.Hour)
	repo := &memoryGatewayRepository{
		aiClients: map[string]domainaigateway.AIClient{
			"codex": {ID: "codex", Name: "Codex", Status: "active", Metadata: map[string]any{"registrationApprovalRequired": true}},
		},
		accessPolicies: []domainaigateway.AccessPolicy{
			{
				ID:             "policy-unscoped",
				Enabled:        true,
				SubjectType:    "role",
				SubjectID:      "developer",
				AIClientID:     "codex",
				Effect:         "allow",
				ToolPatterns:   []string{"delivery.*"},
				ApprovalPolicy: map[string]any{"strategy": "require_approval"},
			},
			{
				ID:             "policy-wildcard-scope",
				Enabled:        true,
				SubjectType:    "role",
				SubjectID:      "developer",
				Effect:         "allow",
				ToolPatterns:   []string{"delivery.*"},
				ResourceScopes: map[string]any{"applicationId": "*"},
				ApprovalPolicy: map[string]any{"strategy": "require_approval"},
			},
			{
				ID:             "policy-scoped",
				Enabled:        true,
				SubjectType:    "role",
				SubjectID:      "developer",
				Effect:         "allow",
				ToolPatterns:   []string{"delivery.*"},
				ResourceScopes: map[string]any{"applicationId": "app-1"},
				ApprovalPolicy: map[string]any{"strategy": "require_approval"},
			},
			{
				ID:             "policy-read",
				Enabled:        true,
				SubjectType:    "role",
				SubjectID:      "developer",
				Effect:         "allow",
				ToolPatterns:   []string{"k8s.pods.logs"},
				ApprovalPolicy: map[string]any{"strategy": "require_approval"},
			},
			{
				ID:             "policy-dry-run",
				Enabled:        true,
				SubjectType:    "role",
				SubjectID:      "developer",
				Effect:         "allow",
				ToolPatterns:   []string{"delivery.*"},
				ApprovalPolicy: map[string]any{"strategy": "dry_run_only"},
			},
		},
		toolGrants: []domainaigateway.ToolGrant{
			{ID: "grant-unscoped", SubjectType: "role", SubjectID: "developer", AIClientID: "codex", ToolName: "delivery.*", Effect: "allow", RequiresApproval: true},
			{ID: "grant-wildcard-scope", SubjectType: "role", SubjectID: "developer", ToolName: "delivery.*", Effect: "allow", ResourceScopes: map[string]any{"clusterId": "*"}, RequiresApproval: true},
			{ID: "grant-scoped", SubjectType: "role", SubjectID: "developer", ToolName: "delivery.*", Effect: "allow", ResourceScopes: map[string]any{"applicationId": "app-1"}, RequiresApproval: true},
			{ID: "grant-read", SubjectType: "role", SubjectID: "developer", ToolName: "k8s.pods.logs", Effect: "allow", RequiresApproval: true},
			{ID: "grant-expired", SubjectType: "role", SubjectID: "developer", ToolName: "delivery.*", Effect: "allow", ExpiresAt: &expiredAt, RequiresApproval: true},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"admin": {appaccess.PermAIGatewayManage},
		},
	}), nil, repo)

	status, err := service.GovernanceStatus(context.Background(), testPrincipal("admin"), domainaigateway.GovernanceStatusRequest{WindowHours: 24})
	if err != nil {
		t.Fatalf("GovernanceStatus returned error: %v", err)
	}

	assertResourceScopeGovernanceStatus(t, status)
}

func TestInvokeToolRoutesDeliveryListThroughApplicationService(t *testing.T) {
	audit := &captureAuditRecorder{}
	apps := &fakeApplicationService{}
	repo := &memoryGatewayRepository{}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsView,
			},
		},
	}), audit, repo)
	service.SetDeliveryServices(apps, &fakeDeliveryService{})

	result, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName:   "delivery.applications.list",
		Input:      map[string]any{"limit": 10, "applicationId": "app-1"},
		AIClientID: "codex",
		SkillID:    "delivery-developer",
	})
	if err != nil {
		t.Fatalf("InvokeTool returned error: %v", err)
	}
	if !apps.listed {
		t.Fatalf("expected application service list to be called")
	}
	if result.ToolName != "delivery.applications.list" || result.Result != "success" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if len(audit.entries) != 1 || audit.entries[0].Result != "success" {
		t.Fatalf("expected success audit entry, got %#v", audit.entries)
	}
	if len(repo.auditLogs) != 1 {
		t.Fatalf("expected dedicated AI Gateway audit log, got %#v", repo.auditLogs)
	}
	entry := repo.auditLogs[0]
	if entry.ToolName != "delivery.applications.list" || entry.AIClientID != "codex" || entry.SkillID != "delivery-developer" || entry.Result != "success" {
		t.Fatalf("unexpected dedicated AI Gateway audit log: %#v", entry)
	}
	if entry.ResourceScope["applicationId"] != "app-1" || entry.RiskLevel != domainaigateway.RiskLevelRead {
		t.Fatalf("expected resource scope and risk level in audit log, got %#v", entry)
	}
}

func TestInvokeDeliveryP1ReadToolsUseOwningServices(t *testing.T) {
	apps := &fakeApplicationService{}
	catalog := &fakeCatalogService{}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsView,
				appaccess.PermDeliveryApplicationServicesView,
				appaccess.PermDeliveryApplicationEnvView,
				appaccess.PermDeliveryWorkflowTemplatesView,
			},
		},
	}), nil, &memoryGatewayRepository{})
	service.SetDeliveryServices(apps, &fakeDeliveryService{})
	service.SetCatalogService(catalog)

	servicesResult, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName: "delivery.application_services.list",
		Input:    map[string]any{"applicationId": "app-1"},
	})
	if err != nil {
		t.Fatalf("application services tool returned error: %v", err)
	}
	if !apps.servicesListed || servicesResult.RelatedIDs["count"] != 1 {
		t.Fatalf("expected application service list to be called, result=%#v apps=%#v", servicesResult, apps)
	}
	services := mustValueAs[[]domainapp.Service](t, servicesResult.Output)
	if services[0].Metadata["token"] != "[REDACTED]" || services[0].Containers[0].EnvSchema["password"] != "[REDACTED]" {
		t.Fatalf("expected service and container config to be redacted, got %#v", services[0])
	}

	buildSourcesResult, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName: "delivery.build_sources.list",
		Input:    map[string]any{"applicationId": "app-1", "withBindings": true},
	})
	if err != nil {
		t.Fatalf("build sources tool returned error: %v", err)
	}
	buildSourcesOutput := mustValueAs[map[string]any](t, buildSourcesResult.Output)
	buildSources := mustMapFieldAs[[]domainapp.BuildSource](t, buildSourcesOutput, "buildSources")
	if len(buildSources) != 1 || buildSourcesResult.RelatedIDs["bindingCount"] != 1 {
		t.Fatalf("expected build source and binding usage output, got %#v", buildSourcesResult)
	}
	if buildSources[0].Config["token"] != "[REDACTED]" {
		t.Fatalf("expected build source config to be redacted, got %#v", buildSources[0].Config)
	}

	targetsResult, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName: "delivery.release_targets.list",
		Input:    map[string]any{"applicationId": "app-1"},
	})
	if err != nil {
		t.Fatalf("release targets tool returned error: %v", err)
	}
	if targetsResult.RelatedIDs["count"] != 1 {
		t.Fatalf("expected one release target, got %#v", targetsResult)
	}

	templatesResult, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName: "delivery.workflow_templates.list",
	})
	if err != nil {
		t.Fatalf("workflow templates tool returned error: %v", err)
	}
	if !catalog.listedWorkflowTemplates || templatesResult.RelatedIDs["count"] != 1 {
		t.Fatalf("expected catalog workflow template list, result=%#v catalog=%#v", templatesResult, catalog)
	}
}

func TestInvokeDeliveryExecutionLogsStandaloneToolRedacts(t *testing.T) {
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryExecutionTasksView,
			},
		},
	}), nil, &memoryGatewayRepository{})
	service.SetDeliveryServices(&fakeApplicationService{}, &fakeDeliveryService{})

	result, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName: "delivery.execution_logs.list",
		Input:    map[string]any{"taskId": "task-1", "limit": 10},
	})
	if err != nil {
		t.Fatalf("execution logs tool returned error: %v", err)
	}
	logs := mustValueAs[[]domaindelivery.ExecutionLog](t, result.Output)
	if logs[0].Message != "build failed token=[REDACTED]" || logs[0].Metadata["password"] != "[REDACTED]" {
		t.Fatalf("expected redacted execution logs, got %#v", logs[0])
	}
	if result.RelatedIDs["executionTaskId"] != "task-1" {
		t.Fatalf("expected task id in related ids, got %#v", result.RelatedIDs)
	}
}

func TestInvokeDeliveryReleaseAndRollbackContextAreReadOnly(t *testing.T) {
	delivery := &fakeDeliveryService{}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsView,
				appaccess.PermDeliveryReleaseBundlesView,
				appaccess.PermDeliveryExecutionTasksView,
			},
		},
	}), nil, &memoryGatewayRepository{})
	service.SetDeliveryServices(&fakeApplicationService{}, delivery)

	diffResult, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName: "delivery.release_context.diff",
		Input: map[string]any{
			"applicationId":            "app-1",
			"applicationEnvironmentId": "binding-1",
			"sourceBundleId":           "bundle-0",
			"targetBundleId":           "bundle-1",
		},
	})
	if err != nil {
		t.Fatalf("release context diff returned error: %v", err)
	}
	diffOutput := mustValueAs[map[string]any](t, diffResult.Output)
	comparison := mustMapFieldAs[map[string]any](t, diffOutput, "comparison")
	if comparison["sourceBundleId"] != "bundle-0" || comparison["targetBundleId"] != "bundle-1" {
		t.Fatalf("expected source/target comparison, got %#v", comparison)
	}

	rollbackResult, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName: "delivery.rollback.context",
		Input: map[string]any{
			"applicationId":            "app-1",
			"applicationEnvironmentId": "binding-1",
			"executionTaskId":          "task-1",
		},
	})
	if err != nil {
		t.Fatalf("rollback context returned error: %v", err)
	}
	if delivery.triggered {
		t.Fatalf("rollback context must not trigger delivery actions")
	}
	rollbackOutput := mustValueAs[map[string]any](t, rollbackResult.Output)
	logs := mustMapFieldAs[[]domaindelivery.ExecutionLog](t, rollbackOutput, "executionLogs")
	if logs[0].Message != "build failed token=[REDACTED]" {
		t.Fatalf("expected redacted rollback context logs, got %#v", logs)
	}
	suggestions := mustMapFieldAs[[]map[string]any](t, rollbackOutput, "suggestions")
	if len(suggestions) == 0 {
		t.Fatalf("expected rollback suggestions, got %#v", rollbackOutput)
	}
}

func TestInvokeDeliveryAICapabilitiesReturnDraftsWithoutExecution(t *testing.T) {
	delivery := &fakeDeliveryService{}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsView,
				appaccess.PermDeliveryApplicationEnvView,
			},
		},
	}), nil, &memoryGatewayRepository{})
	service.SetDeliveryServices(&fakeApplicationService{}, delivery)

	onboardingResult, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName: "delivery.onboarding.analyze_repo",
		Input: map[string]any{
			"repositoryPath": "platform/payments-api",
			"files":          []string{"go.mod", "cmd/api/main.go"},
			"environmentKey": "dev",
			"clusterId":      "cluster-a",
			"namespace":      "payments",
		},
	})
	if err != nil {
		t.Fatalf("onboarding analysis returned error: %v", err)
	}
	onboardingOutput := mustValueAs[map[string]any](t, onboardingResult.Output)
	draft := mustMapFieldAs[domaindelivery.DeliveryDraftInput](t, onboardingOutput, "deliveryDraftInput")
	if draft.Source != domaindelivery.DeliveryDraftSourceAI || draft.ApplicationDraft.Key != "payments-api" || len(draft.Files) == 0 {
		t.Fatalf("expected AI DeliveryDraft suggestion, got %#v", draft)
	}

	specResult, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName: "delivery.spec.render",
		Input: map[string]any{
			"source": "ai",
			"applicationDraft": map[string]any{
				"name":     "Payments API",
				"key":      "payments-api",
				"language": "go",
				"enabled":  true,
			},
			"services": []map[string]any{{"key": "api", "name": "API", "serviceKind": "kubernetes_workload", "enabled": true}},
		},
	})
	if err != nil {
		t.Fatalf("spec render returned error: %v", err)
	}
	specOutput := mustValueAs[map[string]any](t, specResult.Output)
	renderedDraft := mustMapFieldAs[domaindelivery.DeliveryDraftInput](t, specOutput, "deliveryDraftInput")
	if renderedDraft.ApplicationDraft.Key != "payments-api" || len(renderedDraft.Services) != 1 {
		t.Fatalf("expected rendered draft payload, got %#v", renderedDraft)
	}

	planResult, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName: "delivery.release.plan",
		Input: map[string]any{
			"applicationId":            "app-1",
			"applicationEnvironmentId": "binding-1",
			"action":                   "deploy",
			"releaseBundleId":          "bundle-1",
			"intent":                   "deploy candidate bundle after QA signoff",
		},
	})
	if err != nil {
		t.Fatalf("release plan returned error: %v", err)
	}
	planOutput := mustValueAs[map[string]any](t, planResult.Output)
	plan := mustMapFieldAs[domaindelivery.DeliveryPlanInput](t, planOutput, "deliveryPlanInput")
	if plan.Source != domaindelivery.DeliveryPlanSourceAI || plan.RiskLevel != "high" || !plan.RequiresApproval {
		t.Fatalf("expected high-risk AI plan draft, got %#v", plan)
	}
	if delivery.triggered {
		t.Fatalf("AI planning capability must not trigger delivery execution")
	}
}

func TestInvokeToolRejectsMissingBusinessPermission(t *testing.T) {
	audit := &captureAuditRecorder{}
	apps := &fakeApplicationService{}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
			},
		},
	}), audit)
	service.SetDeliveryServices(apps, &fakeDeliveryService{})

	_, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName: "delivery.applications.list",
	})
	if err == nil {
		t.Fatalf("expected missing delivery permission to be rejected")
	}
	if apps.listed {
		t.Fatalf("application service should not be called after authorization failure")
	}
	if len(audit.entries) != 1 || audit.entries[0].Result != "deny" {
		t.Fatalf("expected deny audit entry, got %#v", audit.entries)
	}
}

func TestInvokeToolRejectsToolGrantDeny(t *testing.T) {
	audit := &captureAuditRecorder{}
	apps := &fakeApplicationService{}
	repo := &memoryGatewayRepository{
		toolGrants: []domainaigateway.ToolGrant{
			{
				ID:          "grant-1",
				SubjectType: "user",
				SubjectID:   "user-1",
				AIClientID:  "codex",
				ToolName:    "delivery.applications.list",
				Effect:      "deny",
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsView,
			},
		},
	}), audit, repo)
	service.SetDeliveryServices(apps, &fakeDeliveryService{})

	_, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName:   "delivery.applications.list",
		AIClientID: "codex",
	})
	if err == nil {
		t.Fatalf("expected deny grant to reject invocation")
	}
	if apps.listed {
		t.Fatalf("application service should not be called after grant denial")
	}
	if len(audit.entries) != 1 || audit.entries[0].Result != "deny" {
		t.Fatalf("expected deny audit entry, got %#v", audit.entries)
	}
}

func TestInvokeToolRejectsToolGrantResourceScopeMismatch(t *testing.T) {
	apps := &fakeApplicationService{}
	repo := &memoryGatewayRepository{
		toolGrants: []domainaigateway.ToolGrant{
			{
				ID:             "grant-1",
				SubjectType:    "user",
				SubjectID:      "user-1",
				ToolName:       "delivery.applications.list",
				Effect:         "allow",
				ResourceScopes: map[string]any{"applicationId": "app-allowed"},
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsView,
			},
		},
	}), nil, repo)
	service.SetDeliveryServices(apps, &fakeDeliveryService{})

	_, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName: "delivery.applications.list",
		Input:    map[string]any{"applicationId": "app-denied"},
	})
	if err == nil {
		t.Fatalf("expected scoped grant mismatch to reject invocation")
	}
	if apps.listed {
		t.Fatalf("application service should not be called after scoped grant denial")
	}
	if len(repo.auditLogs) != 1 || repo.auditLogs[0].Result != "deny" || repo.auditLogs[0].ResourceScope["applicationId"] != "app-denied" {
		t.Fatalf("expected deny audit with requested resource scope, got %#v", repo.auditLogs)
	}
}

func TestInvokeToolRejectsAccessPolicyDeny(t *testing.T) {
	audit := &captureAuditRecorder{}
	apps := &fakeApplicationService{}
	repo := &memoryGatewayRepository{
		accessPolicies: []domainaigateway.AccessPolicy{
			{
				ID:           "policy-1",
				Enabled:      true,
				SubjectType:  "user",
				SubjectID:    "user-1",
				AIClientID:   "codex",
				Effect:       "deny",
				ToolPatterns: []string{"delivery.applications.list"},
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsView,
			},
		},
	}), audit, repo)
	service.SetDeliveryServices(apps, &fakeDeliveryService{})

	_, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName:   "delivery.applications.list",
		AIClientID: "codex",
	})
	if err == nil {
		t.Fatalf("expected deny access policy to reject invocation")
	}
	if apps.listed {
		t.Fatalf("application service should not be called after access policy denial")
	}
	if len(audit.entries) != 1 || audit.entries[0].Result != "deny" {
		t.Fatalf("expected deny audit entry, got %#v", audit.entries)
	}
}

func TestInvokeToolRejectsAccessPolicyResourceScopeMismatch(t *testing.T) {
	apps := &fakeApplicationService{}
	repo := &memoryGatewayRepository{
		accessPolicies: []domainaigateway.AccessPolicy{
			{
				ID:             "policy-1",
				Enabled:        true,
				SubjectType:    "role",
				SubjectID:      "developer",
				Effect:         "allow",
				ToolPatterns:   []string{"delivery.applications.list"},
				ResourceScopes: map[string]any{"businessLineId": []any{"bl-a"}},
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsView,
			},
		},
	}), nil, repo)
	service.SetDeliveryServices(apps, &fakeDeliveryService{})

	_, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName: "delivery.applications.list",
		Input:    map[string]any{"businessLineId": "bl-b"},
	})
	if err == nil {
		t.Fatalf("expected scoped access policy mismatch to reject invocation")
	}
	if apps.listed {
		t.Fatalf("application service should not be called after scoped access policy denial")
	}
}

func TestInvokeToolRejectsAccessPolicyRateLimitCondition(t *testing.T) {
	apps := &fakeApplicationService{}
	repo := &memoryGatewayRepository{
		accessPolicies: []domainaigateway.AccessPolicy{
			{
				ID:           "policy-rate",
				Enabled:      true,
				SubjectType:  "role",
				SubjectID:    "developer",
				Effect:       "allow",
				ToolPatterns: []string{"delivery.applications.list"},
				Conditions: map[string]any{
					"rateLimit": map[string]any{"maxCallsPerMinute": 1},
				},
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsView,
			},
		},
	}), nil, repo)
	service.SetDeliveryServices(apps, &fakeDeliveryService{})

	if _, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName:   "delivery.applications.list",
		AIClientID: "codex",
	}); err != nil {
		t.Fatalf("first InvokeTool returned error: %v", err)
	}
	apps.listed = false

	_, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName:   "delivery.applications.list",
		AIClientID: "codex",
	})
	if err == nil || !strings.Contains(err.Error(), "rate limit") {
		t.Fatalf("expected rate limit rejection, got %v", err)
	}
	if apps.listed {
		t.Fatalf("application service should not be called after rate limit rejection")
	}
	if len(repo.rateLimitCounters) != 1 {
		t.Fatalf("expected one rate-limit counter bucket, got %#v", repo.rateLimitCounters)
	}
	for _, counter := range repo.rateLimitCounters {
		if counter.Count != 2 || counter.Limit != 1 || counter.PolicyID != "policy-rate" {
			t.Fatalf("expected counter to record rejected overage, got %#v", counter)
		}
	}
	if len(repo.auditLogs) != 2 || repo.auditLogs[1].Result != "deny" {
		t.Fatalf("expected deny audit log for rate limit rejection, got %#v", repo.auditLogs)
	}
}

func TestInvokeToolKeepsFixedWindowRateLimitBucketsSeparate(t *testing.T) {
	apps := &fakeApplicationService{}
	repo := &memoryGatewayRepository{
		accessPolicies: []domainaigateway.AccessPolicy{
			{
				ID:           "policy-rate-multi-window",
				Enabled:      true,
				SubjectType:  "role",
				SubjectID:    "developer",
				Effect:       "allow",
				ToolPatterns: []string{"delivery.applications.list"},
				Conditions: map[string]any{
					"rateLimit": map[string]any{
						"maxCallsPerMinute": 1,
						"maxCallsPerHour":   10,
					},
				},
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsView,
			},
		},
	}), nil, repo)
	service.SetDeliveryServices(apps, &fakeDeliveryService{})

	if _, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName:   "delivery.applications.list",
		AIClientID: "codex",
	}); err != nil {
		t.Fatalf("first InvokeTool returned error: %v", err)
	}
	apps.listed = false

	_, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName:   "delivery.applications.list",
		AIClientID: "codex",
	})
	if err == nil || !strings.Contains(err.Error(), "rate limit") {
		t.Fatalf("expected minute rate limit rejection, got %v", err)
	}
	if apps.listed {
		t.Fatalf("application service should not be called after rate limit rejection")
	}
	if len(repo.rateLimitCounters) != 2 {
		t.Fatalf("expected minute and hour buckets to be distinct, got %#v", repo.rateLimitCounters)
	}
	var minuteFound, hourFound bool
	for _, counter := range repo.rateLimitCounters {
		switch counter.Limit {
		case 1:
			minuteFound = true
			if counter.Count != 2 || counter.WindowEnd.Sub(counter.WindowStart) != time.Minute {
				t.Fatalf("expected minute bucket to record rejected overage, got %#v", counter)
			}
		case 10:
			hourFound = true
			if counter.Count != 1 || counter.WindowEnd.Sub(counter.WindowStart) != time.Hour {
				t.Fatalf("expected hour bucket to stay independent, got %#v", counter)
			}
		default:
			t.Fatalf("unexpected rate limit bucket: %#v", counter)
		}
	}
	if !minuteFound || !hourFound {
		t.Fatalf("expected both minute and hour buckets, got %#v", repo.rateLimitCounters)
	}
}

func TestInvokeToolUsesExternalRateLimitCounterBackend(t *testing.T) {
	apps := &fakeApplicationService{}
	repo := &memoryGatewayRepository{
		accessPolicies: []domainaigateway.AccessPolicy{
			{
				ID:           "policy-redis-rate",
				Enabled:      true,
				SubjectType:  "role",
				SubjectID:    "developer",
				Effect:       "allow",
				ToolPatterns: []string{"delivery.applications.list"},
				Conditions: map[string]any{
					"rateLimit": map[string]any{"maxCallsPerMinute": 1},
				},
			},
		},
	}
	backend := &fakeRateLimitBackend{}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsView,
			},
		},
	}), nil, repo)
	service.SetDeliveryServices(apps, &fakeDeliveryService{})
	service.SetRateLimitBackend(backend)

	if _, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName:   "delivery.applications.list",
		AIClientID: "codex",
	}); err != nil {
		t.Fatalf("first InvokeTool returned error: %v", err)
	}
	apps.listed = false

	_, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName:   "delivery.applications.list",
		AIClientID: "codex",
	})
	if err == nil || !strings.Contains(err.Error(), "rate limit") {
		t.Fatalf("expected external rate limit rejection, got %v", err)
	}
	if apps.listed {
		t.Fatalf("application service should not be called after external rate limit rejection")
	}
	if backend.counterCalls != 2 || len(backend.counters) != 1 {
		t.Fatalf("expected external counter backend to be used, got calls=%d counters=%#v", backend.counterCalls, backend.counters)
	}
	if len(repo.rateLimitCounters) != 0 {
		t.Fatalf("PostgreSQL counters should not be used when external backend succeeds, got %#v", repo.rateLimitCounters)
	}
}

func TestInvokeToolFallsBackWhenExternalRateLimitCounterBackendUnavailable(t *testing.T) {
	apps := &fakeApplicationService{}
	repo := &memoryGatewayRepository{
		accessPolicies: []domainaigateway.AccessPolicy{
			{
				ID:           "policy-redis-rate-fallback",
				Enabled:      true,
				SubjectType:  "role",
				SubjectID:    "developer",
				Effect:       "allow",
				ToolPatterns: []string{"delivery.applications.list"},
				Conditions: map[string]any{
					"rateLimit": map[string]any{"maxCallsPerMinute": 1},
				},
			},
		},
	}
	backend := &fakeRateLimitBackend{counterErr: fmt.Errorf("redis unavailable")}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsView,
			},
		},
	}), nil, repo)
	service.SetDeliveryServices(apps, &fakeDeliveryService{})
	service.SetRateLimitBackend(backend)

	if _, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName:   "delivery.applications.list",
		AIClientID: "codex",
	}); err != nil {
		t.Fatalf("expected PostgreSQL fallback to allow first invocation, got %v", err)
	}
	if backend.counterCalls != 1 {
		t.Fatalf("expected external counter backend to be attempted, got %d calls", backend.counterCalls)
	}
	if len(repo.rateLimitCounters) != 1 {
		t.Fatalf("expected fallback PostgreSQL counter to be used, got %#v", repo.rateLimitCounters)
	}
}

func TestInvokeToolFallsBackToAuditWindowWhenRateLimitCounterUnavailable(t *testing.T) {
	now := time.Now().UTC()
	apps := &fakeApplicationService{}
	repo := &memoryGatewayRepository{
		rateLimitCounterErr: fmt.Errorf(`ERROR: relation "ai_gateway_rate_limit_counters" does not exist`),
		accessPolicies: []domainaigateway.AccessPolicy{
			{
				ID:           "policy-rate-fallback",
				Enabled:      true,
				SubjectType:  "role",
				SubjectID:    "developer",
				Effect:       "allow",
				ToolPatterns: []string{"delivery.applications.list"},
				Conditions: map[string]any{
					"rateLimit": map[string]any{"maxCallsPerMinute": 1},
				},
			},
		},
		auditLogs: []domainaigateway.AuditLog{
			{ID: "audit-1", ActorType: "user", ActorID: "user-1", AIClientID: "codex", ToolName: "delivery.applications.list", Action: "ai_gateway.tool.invoke", Result: "success", CreatedAt: now.Add(-10 * time.Second)},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsView,
			},
		},
	}), nil, repo)
	service.SetDeliveryServices(apps, &fakeDeliveryService{})

	_, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName:   "delivery.applications.list",
		AIClientID: "codex",
	})
	if err == nil || !strings.Contains(err.Error(), "rate limit") {
		t.Fatalf("expected audit-window rate limit rejection, got %v", err)
	}
	if apps.listed {
		t.Fatalf("application service should not be called after fallback rate limit rejection")
	}
	if len(repo.auditLogs) != 2 || repo.auditLogs[1].Result != "deny" {
		t.Fatalf("expected deny audit log for fallback rate limit rejection, got %#v", repo.auditLogs)
	}
}

func TestInvokeToolRejectsSlidingWindowRateLimitCondition(t *testing.T) {
	now := time.Now().UTC()
	apps := &fakeApplicationService{}
	repo := &memoryGatewayRepository{
		accessPolicies: []domainaigateway.AccessPolicy{
			{
				ID:           "policy-sliding-rate",
				Enabled:      true,
				SubjectType:  "role",
				SubjectID:    "developer",
				Effect:       "allow",
				ToolPatterns: []string{"delivery.applications.list"},
				Conditions: map[string]any{
					"rateLimit": map[string]any{
						"maxCallsPerMinute": 1,
						"mode":              "sliding_window",
					},
				},
			},
		},
		auditLogs: []domainaigateway.AuditLog{
			{ID: "audit-1", ActorType: "user", ActorID: "user-1", AIClientID: "codex", ToolName: "delivery.applications.list", Action: "ai_gateway.tool.invoke", Result: "success", CreatedAt: now.Add(-10 * time.Second)},
		},
	}
	backend := &fakeRateLimitBackend{}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsView,
			},
		},
	}), nil, repo)
	service.SetDeliveryServices(apps, &fakeDeliveryService{})
	service.SetRateLimitBackend(backend)

	_, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName:   "delivery.applications.list",
		AIClientID: "codex",
	})
	if err == nil || !strings.Contains(err.Error(), "rate limit") {
		t.Fatalf("expected sliding-window rate limit rejection, got %v", err)
	}
	if apps.listed {
		t.Fatalf("application service should not be called after sliding-window rate limit rejection")
	}
	if backend.counterCalls != 0 || backend.stateCalls != 0 || len(repo.rateLimitCounters) != 0 || len(repo.rateLimitStates) != 0 {
		t.Fatalf("sliding-window mode should use audit window directly, got backend=%#v counters=%#v states=%#v", backend, repo.rateLimitCounters, repo.rateLimitStates)
	}
	if len(repo.auditLogs) != 2 || repo.auditLogs[1].Result != "deny" {
		t.Fatalf("expected deny audit log for sliding-window rejection, got %#v", repo.auditLogs)
	}
}

func TestInvokeToolRejectsGCRARateLimitCondition(t *testing.T) {
	apps := &fakeApplicationService{}
	repo := &memoryGatewayRepository{
		accessPolicies: []domainaigateway.AccessPolicy{
			{
				ID:           "policy-gcra-rate",
				Enabled:      true,
				SubjectType:  "role",
				SubjectID:    "developer",
				Effect:       "allow",
				ToolPatterns: []string{"delivery.applications.list"},
				Conditions: map[string]any{
					"rateLimit": map[string]any{
						"maxCallsPerMinute": 1,
						"mode":              "gcra",
						"burst":             2,
					},
				},
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsView,
			},
		},
	}), nil, repo)
	service.SetDeliveryServices(apps, &fakeDeliveryService{})

	for index := 0; index < 2; index++ {
		if _, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
			ToolName:   "delivery.applications.list",
			AIClientID: "codex",
		}); err != nil {
			t.Fatalf("allowed burst invocation %d returned error: %v", index+1, err)
		}
	}
	apps.listed = false
	_, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName:   "delivery.applications.list",
		AIClientID: "codex",
	})
	if err == nil || !strings.Contains(err.Error(), "retry after") {
		t.Fatalf("expected gcra rate limit rejection, got %v", err)
	}
	if apps.listed {
		t.Fatalf("application service should not be called after gcra rate limit rejection")
	}
	if len(repo.rateLimitStates) != 1 {
		t.Fatalf("expected one gcra rate-limit state, got %#v", repo.rateLimitStates)
	}
	for _, state := range repo.rateLimitStates {
		if state.Limit != 1 || state.Burst != 2 || state.PolicyID != "policy-gcra-rate" || state.Allowed {
			t.Fatalf("expected rejected gcra state to be recorded, got %#v", state)
		}
		if state.IntervalSeconds != 60 {
			t.Fatalf("expected 60s gcra interval, got %#v", state)
		}
	}
	if len(repo.rateLimitCounters) != 0 {
		t.Fatalf("gcra mode should not use fixed-window counters, got %#v", repo.rateLimitCounters)
	}
	if len(repo.auditLogs) != 3 || repo.auditLogs[2].Result != "deny" {
		t.Fatalf("expected deny audit log for gcra rate limit rejection, got %#v", repo.auditLogs)
	}
}

func TestInvokeToolKeepsGCRARateLimitStatesSeparate(t *testing.T) {
	apps := &fakeApplicationService{}
	repo := &memoryGatewayRepository{
		accessPolicies: []domainaigateway.AccessPolicy{
			{
				ID:           "policy-gcra-multi-window",
				Enabled:      true,
				SubjectType:  "role",
				SubjectID:    "developer",
				Effect:       "allow",
				ToolPatterns: []string{"delivery.applications.list"},
				Conditions: map[string]any{
					"rateLimit": map[string]any{
						"maxCallsPerMinute": 1,
						"maxCallsPerHour":   10,
						"mode":              "gcra",
						"burst":             1,
					},
				},
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsView,
			},
		},
	}), nil, repo)
	service.SetDeliveryServices(apps, &fakeDeliveryService{})

	if _, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName:   "delivery.applications.list",
		AIClientID: "codex",
	}); err != nil {
		t.Fatalf("first InvokeTool returned error: %v", err)
	}
	apps.listed = false

	_, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName:   "delivery.applications.list",
		AIClientID: "codex",
	})
	if err == nil || !strings.Contains(err.Error(), "retry after") {
		t.Fatalf("expected minute gcra rate limit rejection, got %v", err)
	}
	if apps.listed {
		t.Fatalf("application service should not be called after gcra rate limit rejection")
	}
	if len(repo.rateLimitStates) != 2 {
		t.Fatalf("expected minute and hour gcra states to be distinct, got %#v", repo.rateLimitStates)
	}
	var minuteFound, hourFound bool
	for _, state := range repo.rateLimitStates {
		switch state.Limit {
		case 1:
			minuteFound = true
			if state.Allowed || state.IntervalSeconds != 60 {
				t.Fatalf("expected minute gcra state to reject the second call, got %#v", state)
			}
		case 10:
			hourFound = true
			if !state.Allowed || state.IntervalSeconds != 360 {
				t.Fatalf("expected hour gcra state to remain independent, got %#v", state)
			}
		default:
			t.Fatalf("unexpected gcra rate limit state: %#v", state)
		}
	}
	if !minuteFound || !hourFound {
		t.Fatalf("expected both minute and hour gcra states, got %#v", repo.rateLimitStates)
	}
}

func TestInvokeToolUsesExternalGCRARateLimitBackend(t *testing.T) {
	apps := &fakeApplicationService{}
	repo := &memoryGatewayRepository{
		accessPolicies: []domainaigateway.AccessPolicy{
			{
				ID:           "policy-redis-gcra",
				Enabled:      true,
				SubjectType:  "role",
				SubjectID:    "developer",
				Effect:       "allow",
				ToolPatterns: []string{"delivery.applications.list"},
				Conditions: map[string]any{
					"rateLimit": map[string]any{
						"maxCallsPerMinute": 1,
						"mode":              "gcra",
					},
				},
			},
		},
	}
	backend := &fakeRateLimitBackend{}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsView,
			},
		},
	}), nil, repo)
	service.SetDeliveryServices(apps, &fakeDeliveryService{})
	service.SetRateLimitBackend(backend)

	if _, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName:   "delivery.applications.list",
		AIClientID: "codex",
	}); err != nil {
		t.Fatalf("first InvokeTool returned error: %v", err)
	}
	apps.listed = false
	_, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName:   "delivery.applications.list",
		AIClientID: "codex",
	})
	if err == nil || !strings.Contains(err.Error(), "retry after") {
		t.Fatalf("expected external gcra rejection, got %v", err)
	}
	if apps.listed {
		t.Fatalf("application service should not be called after external gcra rejection")
	}
	if backend.stateCalls != 2 || len(backend.states) != 1 {
		t.Fatalf("expected external gcra backend to be used, got calls=%d states=%#v", backend.stateCalls, backend.states)
	}
	if len(repo.rateLimitStates) != 0 {
		t.Fatalf("PostgreSQL GCRA state should not be used when external backend succeeds, got %#v", repo.rateLimitStates)
	}
}

func TestInvokeToolFallsBackToAuditWindowWhenGCRARateLimitStateUnavailable(t *testing.T) {
	now := time.Now().UTC()
	apps := &fakeApplicationService{}
	repo := &memoryGatewayRepository{
		rateLimitStateErr: fmt.Errorf(`ERROR: relation "ai_gateway_rate_limit_states" does not exist`),
		accessPolicies: []domainaigateway.AccessPolicy{
			{
				ID:           "policy-gcra-fallback",
				Enabled:      true,
				SubjectType:  "role",
				SubjectID:    "developer",
				Effect:       "allow",
				ToolPatterns: []string{"delivery.applications.list"},
				Conditions: map[string]any{
					"rateLimit": map[string]any{"maxCallsPerMinute": 1, "mode": "token_bucket"},
				},
			},
		},
		auditLogs: []domainaigateway.AuditLog{
			{ID: "audit-1", ActorType: "user", ActorID: "user-1", AIClientID: "codex", ToolName: "delivery.applications.list", Action: "ai_gateway.tool.invoke", Result: "success", CreatedAt: now.Add(-10 * time.Second)},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsView,
			},
		},
	}), nil, repo)
	service.SetDeliveryServices(apps, &fakeDeliveryService{})

	_, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName:   "delivery.applications.list",
		AIClientID: "codex",
	})
	if err == nil || !strings.Contains(err.Error(), "rate limit") {
		t.Fatalf("expected gcra fallback rate limit rejection, got %v", err)
	}
	if apps.listed {
		t.Fatalf("application service should not be called after gcra fallback rate limit rejection")
	}
	if len(repo.auditLogs) != 2 || repo.auditLogs[1].Result != "deny" {
		t.Fatalf("expected deny audit log for gcra fallback rejection, got %#v", repo.auditLogs)
	}
}

func TestInvokeToolDoesNotIncrementRateLimitCounterAfterRedactionRejection(t *testing.T) {
	apps := &fakeApplicationService{}
	repo := &memoryGatewayRepository{
		accessPolicies: []domainaigateway.AccessPolicy{
			{
				ID:           "policy-rate-and-redaction",
				Enabled:      true,
				SubjectType:  "role",
				SubjectID:    "developer",
				Effect:       "allow",
				ToolPatterns: []string{"delivery.applications.list"},
				Conditions: map[string]any{
					"rateLimit":       map[string]any{"maxCallsPerMinute": 1},
					"redactionPolicy": map[string]any{"mode": "strict"},
				},
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsView,
			},
		},
	}), nil, repo)
	service.SetDeliveryServices(apps, &fakeDeliveryService{})

	_, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName:   "delivery.applications.list",
		AIClientID: "codex",
		Input:      map[string]any{"password": "secret"},
	})
	if err == nil || !strings.Contains(err.Error(), "redaction policy") {
		t.Fatalf("expected redaction rejection, got %v", err)
	}
	if len(repo.rateLimitCounters) != 0 {
		t.Fatalf("redaction rejection should not increment rate limit counters, got %#v", repo.rateLimitCounters)
	}
}

func TestInvokeToolRejectsAccessPolicyBudgetCondition(t *testing.T) {
	now := time.Now().UTC()
	apps := &fakeApplicationService{}
	repo := &memoryGatewayRepository{
		accessPolicies: []domainaigateway.AccessPolicy{
			{
				ID:           "policy-budget",
				Enabled:      true,
				SubjectType:  "role",
				SubjectID:    "developer",
				Effect:       "allow",
				ToolPatterns: []string{"delivery.applications.list"},
				Conditions: map[string]any{
					"budget": map[string]any{"maxCallsPerHour": 1},
				},
			},
		},
		auditLogs: []domainaigateway.AuditLog{
			{ID: "audit-1", ActorType: "user", ActorID: "user-1", AIClientID: "codex", ToolName: "delivery.applications.list", Action: "ai_gateway.tool.invoke", Result: "success", CreatedAt: now.Add(-10 * time.Minute)},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsView,
			},
		},
	}), nil, repo)
	service.SetDeliveryServices(apps, &fakeDeliveryService{})

	_, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName:   "delivery.applications.list",
		AIClientID: "codex",
	})
	if err == nil || !strings.Contains(err.Error(), "budget") {
		t.Fatalf("expected budget rejection, got %v", err)
	}
	if apps.listed {
		t.Fatalf("application service should not be called after budget rejection")
	}
	if len(repo.auditLogs) != 2 || repo.auditLogs[1].Result != "deny" {
		t.Fatalf("expected deny audit log for budget rejection, got %#v", repo.auditLogs)
	}
}

func TestInvokeToolRejectsAccessPolicyTokenBudgetCondition(t *testing.T) {
	now := time.Now().UTC()
	apps := &fakeApplicationService{}
	repo := &memoryGatewayRepository{
		accessPolicies: []domainaigateway.AccessPolicy{
			{
				ID:           "policy-token-budget",
				Enabled:      true,
				SubjectType:  "role",
				SubjectID:    "developer",
				Effect:       "allow",
				ToolPatterns: []string{"delivery.applications.list"},
				Conditions: map[string]any{
					"budget": map[string]any{"maxTokensPerDay": 1000, "scope": "actor_client_tool"},
				},
			},
		},
		auditLogs: []domainaigateway.AuditLog{
			{
				ID:         "audit-1",
				ActorType:  "user",
				ActorID:    "user-1",
				AIClientID: "codex",
				ToolName:   "delivery.applications.list",
				Action:     "ai_gateway.tool.invoke",
				Result:     "success",
				Metadata: map[string]any{
					"usage": map[string]any{"inputTokens": 400, "outputTokens": 700},
				},
				CreatedAt: now.Add(-2 * time.Hour),
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsView,
			},
		},
	}), nil, repo)
	service.SetDeliveryServices(apps, &fakeDeliveryService{})

	_, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName:   "delivery.applications.list",
		AIClientID: "codex",
	})
	if err == nil || !strings.Contains(err.Error(), "token budget") {
		t.Fatalf("expected token budget rejection, got %v", err)
	}
	if apps.listed {
		t.Fatalf("application service should not be called after token budget rejection")
	}
	if len(repo.auditLogs) != 2 || repo.auditLogs[1].Result != "deny" {
		t.Fatalf("expected deny audit log for token budget rejection, got %#v", repo.auditLogs)
	}
}

func TestInvokeToolRejectsAccessPolicyCostBudgetCondition(t *testing.T) {
	now := time.Now().UTC()
	apps := &fakeApplicationService{}
	repo := &memoryGatewayRepository{
		accessPolicies: []domainaigateway.AccessPolicy{
			{
				ID:           "policy-cost-budget",
				Enabled:      true,
				SubjectType:  "role",
				SubjectID:    "developer",
				Effect:       "allow",
				ToolPatterns: []string{"delivery.applications.list"},
				Conditions: map[string]any{
					"budget": map[string]any{"maxCost": 1.25, "windowHours": 24, "scope": "actor_client"},
				},
			},
		},
		auditLogs: []domainaigateway.AuditLog{
			{
				ID:         "audit-1",
				ActorType:  "user",
				ActorID:    "user-1",
				AIClientID: "codex",
				ToolName:   "delivery.applications.detail",
				Action:     "ai_gateway.tool.invoke",
				Result:     "success",
				Metadata: map[string]any{
					"providerUsage": map[string]any{"totalCost": 1.30, "totalTokens": 900},
				},
				CreatedAt: now.Add(-30 * time.Minute),
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsView,
			},
		},
	}), nil, repo)
	service.SetDeliveryServices(apps, &fakeDeliveryService{})

	_, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName:   "delivery.applications.list",
		AIClientID: "codex",
	})
	if err == nil || !strings.Contains(err.Error(), "cost budget") {
		t.Fatalf("expected cost budget rejection, got %v", err)
	}
	if apps.listed {
		t.Fatalf("application service should not be called after cost budget rejection")
	}
	if len(repo.auditLogs) != 2 || repo.auditLogs[1].Result != "deny" {
		t.Fatalf("expected deny audit log for cost budget rejection, got %#v", repo.auditLogs)
	}
}

func TestInvokeToolRejectsAccessPolicyStrictRedactionCondition(t *testing.T) {
	apps := &fakeApplicationService{}
	repo := &memoryGatewayRepository{
		accessPolicies: []domainaigateway.AccessPolicy{
			{
				ID:           "policy-redaction",
				Enabled:      true,
				SubjectType:  "role",
				SubjectID:    "developer",
				Effect:       "allow",
				ToolPatterns: []string{"delivery.applications.list"},
				Conditions: map[string]any{
					"redactionPolicy": map[string]any{"mode": "strict"},
				},
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsView,
			},
		},
	}), nil, repo)
	service.SetDeliveryServices(apps, &fakeDeliveryService{})

	_, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName:   "delivery.applications.list",
		AIClientID: "codex",
		Input:      map[string]any{"password": "secret"},
	})
	if err == nil || !strings.Contains(err.Error(), "redaction policy") {
		t.Fatalf("expected redaction policy rejection, got %v", err)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("redaction policy error leaked sensitive input: %v", err)
	}
	if apps.listed {
		t.Fatalf("application service should not be called after redaction policy rejection")
	}
	if len(repo.auditLogs) != 1 || repo.auditLogs[0].Result != "deny" {
		t.Fatalf("expected deny audit log for redaction rejection, got %#v", repo.auditLogs)
	}
}

func TestInvokeToolSanitizesAccessPolicyRedactionCondition(t *testing.T) {
	apps := &fakeApplicationService{}
	repo := &memoryGatewayRepository{
		accessPolicies: []domainaigateway.AccessPolicy{
			{
				ID:           "policy-redact",
				Enabled:      true,
				SubjectType:  "role",
				SubjectID:    "developer",
				Effect:       "allow",
				ToolPatterns: []string{"delivery.applications.list"},
				Conditions: map[string]any{
					"redactionPolicy": map[string]any{"mode": "sanitize"},
				},
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsView,
			},
		},
	}), nil, repo)
	service.SetDeliveryServices(apps, &fakeDeliveryService{})

	_, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName:   "delivery.applications.list",
		AIClientID: "codex",
		Input:      map[string]any{"search": "token=secret-token"},
	})
	if err != nil {
		t.Fatalf("InvokeTool returned error: %v", err)
	}
	if !apps.listed {
		t.Fatalf("expected application service to be called after sanitize redaction")
	}
	if apps.lastFilter.Search != "token=[REDACTED]" {
		t.Fatalf("expected sanitized search input, got %q", apps.lastFilter.Search)
	}
	if len(repo.auditLogs) != 1 || repo.auditLogs[0].Result != "success" {
		t.Fatalf("expected success audit log after sanitize redaction, got %#v", repo.auditLogs)
	}
}

func TestInvokeToolSanitizesAccessPolicyValuePattern(t *testing.T) {
	apps := &fakeApplicationService{}
	repo := &memoryGatewayRepository{
		accessPolicies: []domainaigateway.AccessPolicy{
			{
				ID:           "policy-pattern-redact",
				Enabled:      true,
				SubjectType:  "role",
				SubjectID:    "developer",
				Effect:       "allow",
				ToolPatterns: []string{"delivery.applications.list"},
				Conditions: map[string]any{
					"redactionPolicy": map[string]any{
						"mode":          "sanitize",
						"valuePatterns": []any{`APP-[0-9]{4}`},
						"replacement":   "[APP-ID]",
					},
				},
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsView,
			},
		},
	}), nil, repo)
	service.SetDeliveryServices(apps, &fakeDeliveryService{})

	_, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName:   "delivery.applications.list",
		AIClientID: "codex",
		Input:      map[string]any{"search": "incident APP-1234"},
	})
	if err != nil {
		t.Fatalf("InvokeTool returned error: %v", err)
	}
	if apps.lastFilter.Search != "incident [APP-ID]" {
		t.Fatalf("expected value pattern redaction, got %q", apps.lastFilter.Search)
	}
	if len(repo.auditLogs) != 1 {
		t.Fatalf("expected success audit log, got %#v", repo.auditLogs)
	}
	redaction := mapValue(repo.auditLogs[0].Metadata["redaction"])
	if redaction["valuePatternMatches"] != 1 {
		t.Fatalf("expected value pattern redaction summary, got %#v", redaction)
	}
	if text := fmt.Sprint(repo.auditLogs[0].Metadata); strings.Contains(text, "APP-1234") {
		t.Fatalf("redaction audit summary leaked matched value: %s", text)
	}
}

func TestInvokeToolRejectsAccessPolicySecretClassifier(t *testing.T) {
	apps := &fakeApplicationService{}
	repo := &memoryGatewayRepository{
		accessPolicies: []domainaigateway.AccessPolicy{
			{
				ID:           "policy-secret-classifier",
				Enabled:      true,
				SubjectType:  "role",
				SubjectID:    "developer",
				Effect:       "allow",
				ToolPatterns: []string{"delivery.applications.list"},
				Conditions: map[string]any{
					"redactionPolicy": map[string]any{
						"mode":        "strict",
						"secretTypes": []any{"github"},
					},
				},
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsView,
			},
		},
	}), nil, repo)
	service.SetDeliveryServices(apps, &fakeDeliveryService{})

	_, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName:   "delivery.applications.list",
		AIClientID: "codex",
		Input:      map[string]any{"search": fakeGitHubPATForTest()},
	})
	if err == nil || !strings.Contains(err.Error(), "redaction policy") {
		t.Fatalf("expected secret classifier rejection, got %v", err)
	}
	if apps.listed {
		t.Fatalf("application service should not be called after classifier rejection")
	}
	if len(repo.auditLogs) != 1 || repo.auditLogs[0].Result != "deny" {
		t.Fatalf("expected deny audit log for classifier rejection, got %#v", repo.auditLogs)
	}
	redaction := mapValue(repo.auditLogs[0].Metadata["redaction"])
	if redaction["secretClassifierMatches"] != 1 {
		t.Fatalf("expected classifier redaction summary, got %#v", redaction)
	}
	if text := fmt.Sprint(repo.auditLogs[0].Metadata); strings.Contains(text, fakeGitHubPATForTest()) {
		t.Fatalf("redaction audit summary leaked classified secret: %s", text)
	}
}

func TestInvokeToolRejectsAdditionalProviderSecretClassifiers(t *testing.T) {
	apps := &fakeApplicationService{}
	repo := &memoryGatewayRepository{
		accessPolicies: []domainaigateway.AccessPolicy{
			{
				ID:           "policy-provider-classifiers",
				Enabled:      true,
				SubjectType:  "role",
				SubjectID:    "developer",
				Effect:       "allow",
				ToolPatterns: []string{"delivery.applications.list"},
				Conditions: map[string]any{
					"redactionPolicy": map[string]any{
						"mode":        "strict",
						"secretTypes": []any{"anthropic", "google_api_key", "huggingface", "npm", "stripe"},
					},
				},
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsView,
			},
		},
	}), nil, repo)
	service.SetDeliveryServices(apps, &fakeDeliveryService{})

	secret := "sk-ant-" + strings.Repeat("a", 24)
	_, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName:   "delivery.applications.list",
		AIClientID: "codex",
		Input:      map[string]any{"search": secret},
	})
	if err == nil || !strings.Contains(err.Error(), "redaction policy") {
		t.Fatalf("expected provider classifier rejection, got %v", err)
	}
	if apps.listed {
		t.Fatalf("application service should not be called after provider classifier rejection")
	}
	redaction := mapValue(repo.auditLogs[0].Metadata["redaction"])
	if redaction["secretClassifierMatches"] != 1 {
		t.Fatalf("expected provider classifier redaction summary, got %#v", redaction)
	}
	classifiers := fmt.Sprint(redaction["classifiers"])
	if !strings.Contains(classifiers, "anthropic") {
		t.Fatalf("expected anthropic classifier in redaction summary, got %#v", redaction)
	}
	if text := fmt.Sprint(repo.auditLogs[0].Metadata); strings.Contains(text, secret) {
		t.Fatalf("redaction audit summary leaked classified provider secret: %s", text)
	}
}

func TestInvokeToolRejectsAdditionalAIToolSecretClassifiers(t *testing.T) {
	secretTypes := []any{"cohere", "mistral", "deepseek", "groq", "together", "replicate", "langsmith", "pinecone"}
	apps, repo, service := newSecretClassifierTestService("policy-ai-tool-classifiers", secretTypes)

	_, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName:   "delivery.applications.list",
		AIClientID: "codex",
		Input: map[string]any{
			"cohere":    "cohere_api_key_12345678901234567890",
			"mistral":   "mistral_12345678901234567890",
			"deepseek":  "sk-deepseek-12345678901234567890",
			"groq":      "gsk_12345678901234567890",
			"together":  "tgp_v1_12345678901234567890",
			"replicate": "r8_12345678901234567890",
			"langsmith": "lsv2_12345678901234567890",
			"pinecone":  "pcsk_12345678901234567890",
		},
	})
	assertSecretClassifierRejected(t, apps, repo, err, []string{"cohere", "mistral", "deepseek", "groq", "together", "replicate", "langsmith", "pinecone"})
}

func TestInvokeToolRejectsNewProviderSecretClassifiers(t *testing.T) {
	apps := &fakeApplicationService{}
	repo := &memoryGatewayRepository{
		accessPolicies: []domainaigateway.AccessPolicy{
			{
				ID:           "policy-new-provider-classifiers",
				Enabled:      true,
				SubjectType:  "role",
				SubjectID:    "developer",
				Effect:       "allow",
				ToolPatterns: []string{"delivery.applications.list"},
				Conditions: map[string]any{
					"redactionPolicy": map[string]any{
						"mode":        "strict",
						"secretTypes": []any{"xai", "perplexity", "tavily", "langfuse", "qdrant", "wandb", "linear", "openrouter", "fireworks", "voyage"},
					},
				},
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsView,
			},
		},
	}), nil, repo)
	service.SetDeliveryServices(apps, &fakeDeliveryService{})

	_, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName:   "delivery.applications.list",
		AIClientID: "codex",
		Input: map[string]any{
			"xai":        "xai-12345678901234567890",
			"perplexity": "pplx-12345678901234567890",
			"tavily":     "tvly-12345678901234567890",
			"langfuse":   "sk-lf-12345678901234567890",
			"qdrant":     "qdrant_12345678901234567890",
			"wandb":      "wandb_12345678901234567890",
			"linear":     "lin_api_12345678901234567890",
			"openrouter": "sk-or-v1-12345678901234567890",
			"fireworks":  "fw_12345678901234567890",
			"voyage":     "pa-12345678901234567890",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "redaction policy") {
		t.Fatalf("expected new provider classifier rejection, got %v", err)
	}
	if apps.listed {
		t.Fatalf("application service should not be called after new provider classifier rejection")
	}
	redaction := mapValue(repo.auditLogs[0].Metadata["redaction"])
	classifiers := fmt.Sprint(redaction["classifiers"])
	for _, classifier := range []string{"xai", "perplexity", "tavily", "langfuse", "qdrant", "wandb", "linear", "openrouter", "fireworks", "voyage"} {
		if !strings.Contains(classifiers, classifier) {
			t.Fatalf("expected classifier %s in redaction summary, got %#v", classifier, redaction)
		}
	}
	if text := fmt.Sprint(repo.auditLogs[0].Metadata); strings.Contains(text, "12345678901234567890") {
		t.Fatalf("new provider classifier audit leaked raw secret: %s", text)
	}
}

func TestInvokeToolRejectsAgentToolingSecretClassifiers(t *testing.T) {
	secretTypes := []any{"brave_search", "serpapi", "browserbase", "exa", "jina", "unstructured", "llama_cloud", "helicone"}
	apps, repo, service := newSecretClassifierTestService("policy-agent-tooling-classifiers", secretTypes)

	_, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName:   "delivery.applications.list",
		AIClientID: "codex",
		Input: map[string]any{
			"brave":        "BSA12345678901234567890",
			"serpapi":      "serpapi_12345678901234567890",
			"browserbase":  "bb_12345678901234567890",
			"exa":          "exa_12345678901234567890",
			"jina":         "jina_12345678901234567890",
			"unstructured": "unstructured_12345678901234567890",
			"llamaCloud":   "llx-12345678901234567890",
			"helicone":     "sk-helicone-12345678901234567890",
		},
	})
	assertSecretClassifierRejected(t, apps, repo, err, []string{"brave_search", "serpapi", "browserbase", "exa", "jina", "unstructured", "llama_cloud", "helicone"})
}

func TestInvokeToolRejectsChinaCloudSecretClassifiers(t *testing.T) {
	apps := &fakeApplicationService{}
	repo := &memoryGatewayRepository{
		accessPolicies: []domainaigateway.AccessPolicy{
			{
				ID:           "policy-china-cloud-classifiers",
				Enabled:      true,
				SubjectType:  "role",
				SubjectID:    "developer",
				Effect:       "allow",
				ToolPatterns: []string{"delivery.applications.list"},
				Conditions: map[string]any{
					"redactionPolicy": map[string]any{
						"mode":        "strict",
						"secretTypes": []any{"dashscope", "moonshot", "zhipu", "siliconflow", "hunyuan", "qianfan", "volcengine"},
					},
				},
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsView,
			},
		},
	}), nil, repo)
	service.SetDeliveryServices(apps, &fakeDeliveryService{})

	_, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName:   "delivery.applications.list",
		AIClientID: "codex",
		Input: map[string]any{
			"dashscope":   "sk-123456789012345678901234",
			"moonshot":    fakeProviderKeyForTest("1"),
			"zhipu":       "appid_123456.abcd123456789012345678901234",
			"siliconflow": fakeProviderKeyForTest("a"),
			"hunyuan":     "AKID12345678901234567890",
			"qianfan":     "bce-v3/abcdefghijklmnopqrstuvwxyz123456",
			"volcengine":  "aklt12345678901234567890",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "redaction policy") {
		t.Fatalf("expected China cloud classifier rejection, got %v", err)
	}
	if apps.listed {
		t.Fatalf("application service should not be called after China cloud classifier rejection")
	}
	redaction := mapValue(repo.auditLogs[0].Metadata["redaction"])
	classifiers := fmt.Sprint(redaction["classifiers"])
	for _, classifier := range []string{"dashscope", "moonshot", "zhipu", "siliconflow", "hunyuan", "qianfan", "volcengine"} {
		if !strings.Contains(classifiers, classifier) {
			t.Fatalf("expected classifier %s in redaction summary, got %#v", classifier, redaction)
		}
	}
	if text := fmt.Sprint(repo.auditLogs[0].Metadata); strings.Contains(text, "12345678901234567890") || strings.Contains(text, "abcdefghijklmnopqrstuvwxyz") || strings.Contains(text, "appid_123456") {
		t.Fatalf("China cloud classifier audit leaked raw secret: %s", text)
	}
}

func TestInvokeToolRejectsObservabilitySecretClassifiers(t *testing.T) {
	apps := &fakeApplicationService{}
	repo := &memoryGatewayRepository{
		accessPolicies: []domainaigateway.AccessPolicy{
			{
				ID:           "policy-observability-classifiers",
				Enabled:      true,
				SubjectType:  "role",
				SubjectID:    "developer",
				Effect:       "allow",
				ToolPatterns: []string{"delivery.applications.list"},
				Conditions: map[string]any{
					"redactionPolicy": map[string]any{
						"mode":        "strict",
						"secretTypes": []any{"grafana", "sentry", "newrelic", "azure_openai", "azure_devops", "datadog", "pagerduty", "posthog", "splunk", "elastic", "terraform"},
					},
				},
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsView,
			},
		},
	}), nil, repo)
	service.SetDeliveryServices(apps, &fakeDeliveryService{})

	_, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName:   "delivery.applications.list",
		AIClientID: "codex",
		Input: map[string]any{
			"grafana":   "glsa_12345678901234567890_abcdef12",
			"sentry":    "sntrys_12345678901234567890",
			"newrelic":  "NRAK-12345678901234567890",
			"azure":     "AZURE_OPENAI_API_KEY=1234567890abcdef1234567890abcdef",
			"azdo":      "abcdefghijklmnopqrstuvwxyz012345679ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnoAZDOabcd",
			"datadog":   "datadog_api_key_1234567890abcdef1234567890abcdef",
			"pagerduty": "pdus+12345678901234567890",
			"posthog":   "phc_12345678901234567890",
			"splunk":    "Splunk 12345678901234567890",
			"elastic":   "ApiKey 12345678901234567890",
			"terraform": "atlasv1.12345678901234567890",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "redaction policy") {
		t.Fatalf("expected observability classifier rejection, got %v", err)
	}
	if apps.listed {
		t.Fatalf("application service should not be called after observability classifier rejection")
	}
	redaction := mapValue(repo.auditLogs[0].Metadata["redaction"])
	classifiers := fmt.Sprint(redaction["classifiers"])
	for _, classifier := range []string{"grafana", "sentry", "newrelic", "azure_openai", "azure_devops", "datadog", "pagerduty", "posthog", "splunk", "elastic", "terraform"} {
		if !strings.Contains(classifiers, classifier) {
			t.Fatalf("expected classifier %s in redaction summary, got %#v", classifier, redaction)
		}
	}
	if text := fmt.Sprint(repo.auditLogs[0].Metadata); strings.Contains(text, "12345678901234567890") || strings.Contains(text, "abcdef12") || strings.Contains(text, "AZDO") {
		t.Fatalf("observability classifier audit leaked raw secret: %s", text)
	}
}

func TestGatewayRedactionClassifiesStructuredSecrets(t *testing.T) {
	rule := gatewayBuildRedactionRule(map[string]any{
		"mode":        "sanitize",
		"secretTypes": []any{"kubeconfig", "docker_config", "gcp_service_account", "aws"},
		"replacement": "[CLASSIFIED]",
	}, gatewayRedactionRule{Target: "input"})
	value := map[string]any{
		"kubeconfig": map[string]any{
			"clusters":        []any{map[string]any{"name": "prod"}},
			"contexts":        []any{map[string]any{"name": "prod"}},
			"users":           []any{map[string]any{"name": "admin"}},
			"current-context": "prod",
		},
		"docker": map[string]any{
			"auths": map[string]any{"registry.example.com": map[string]any{"auth": "raw-docker-auth"}},
		},
		"gcp": map[string]any{
			"type":         "service_account",
			"private_key":  "raw-gcp-private-key",
			"client_email": "ci@example.iam.gserviceaccount.com",
		},
		"aws": map[string]any{
			"aws_access_key_id":     fakeAWSAccessKeyIDForTest(),
			"aws_secret_access_key": "raw-aws-secret",
		},
	}

	summary := gatewayRedactionAuditSummaryForValue(value, rule, "input")
	if summary.StructuredSecretMatches != 4 {
		t.Fatalf("expected structured secret matches, got %#v", summary)
	}
	for _, classifier := range []string{"kubeconfig", "docker_config", "gcp_service_account", "aws"} {
		if !strings.Contains(fmt.Sprint(summary.Classifiers), classifier) {
			t.Fatalf("expected classifier %s, got %#v", classifier, summary.Classifiers)
		}
	}
	redacted := mustValueAs[map[string]any](t, applyGatewayRedactionValue(value, rule, ""))
	text := fmt.Sprint(redacted)
	for _, raw := range []string{"prod", "raw-docker-auth", "raw-gcp-private-key", "ci@example.iam.gserviceaccount.com", fakeAWSAccessKeyIDForTest(), "raw-aws-secret"} {
		if strings.Contains(text, raw) {
			t.Fatalf("structured secret redaction leaked %q in %#v", raw, redacted)
		}
	}
}

func TestInvokeToolAppliesFieldLevelRedactionPolicy(t *testing.T) {
	apps := &fakeApplicationService{}
	repo := &memoryGatewayRepository{
		accessPolicies: []domainaigateway.AccessPolicy{
			{
				ID:           "policy-field-redact",
				Enabled:      true,
				SubjectType:  "role",
				SubjectID:    "developer",
				Effect:       "allow",
				ToolPatterns: []string{"delivery.applications.list"},
				Conditions: map[string]any{
					"redactionPolicy": map[string]any{
						"mode":        "sanitize",
						"fields":      []any{"search"},
						"replacement": "[MASKED]",
					},
				},
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsView,
			},
		},
	}), nil, repo)
	service.SetDeliveryServices(apps, &fakeDeliveryService{})

	_, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName:   "delivery.applications.list",
		AIClientID: "codex",
		Input:      map[string]any{"search": "billing", "limit": 20},
	})
	if err != nil {
		t.Fatalf("InvokeTool returned error: %v", err)
	}
	if apps.lastFilter.Search != "[MASKED]" || apps.lastFilter.Limit != 20 {
		t.Fatalf("expected field-level redaction to preserve non-target fields, got filter %#v", apps.lastFilter)
	}
}

func TestInvokeToolPreservesAllowedRedactionFields(t *testing.T) {
	apps := &fakeApplicationService{}
	repo := &memoryGatewayRepository{
		accessPolicies: []domainaigateway.AccessPolicy{
			{
				ID:           "policy-allow-fields",
				Enabled:      true,
				SubjectType:  "role",
				SubjectID:    "developer",
				Effect:       "allow",
				ToolPatterns: []string{"delivery.applications.list"},
				Conditions: map[string]any{
					"redactionPolicy": map[string]any{
						"mode":        "strict",
						"allowFields": []any{"search"},
					},
				},
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsView,
			},
		},
	}), nil, repo)
	service.SetDeliveryServices(apps, &fakeDeliveryService{})

	_, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName:   "delivery.applications.list",
		AIClientID: "codex",
		Input:      map[string]any{"search": "token=allowed-for-search"},
	})
	if err != nil {
		t.Fatalf("InvokeTool returned error for allowed field: %v", err)
	}
	if apps.lastFilter.Search != "token=allowed-for-search" {
		t.Fatalf("expected allowed field to remain unchanged, got %q", apps.lastFilter.Search)
	}
}

func TestInvokeToolAppliesToolSpecificRedactionRule(t *testing.T) {
	apps := &fakeApplicationService{}
	repo := &memoryGatewayRepository{
		accessPolicies: []domainaigateway.AccessPolicy{
			{
				ID:           "policy-tool-rule",
				Enabled:      true,
				SubjectType:  "role",
				SubjectID:    "developer",
				Effect:       "allow",
				ToolPatterns: []string{"delivery.*"},
				Conditions: map[string]any{
					"redactionPolicy": map[string]any{
						"rules": []any{
							map[string]any{
								"toolPatterns":    []any{"delivery.applications.create"},
								"fields":          []any{"metadata.apiToken"},
								"mode":            "mask",
								"preserveFormat":  true,
								"replacementText": "[MASKED]",
							},
							map[string]any{
								"toolPatterns": []any{"k8s.*"},
								"fields":       []any{"search"},
								"mode":         "mask",
							},
						},
					},
				},
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsCreate,
			},
		},
	}), nil, repo)
	service.SetDeliveryServices(apps, &fakeDeliveryService{})

	_, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName:   "delivery.applications.create",
		AIClientID: "codex",
		Input: map[string]any{
			"name":    "Billing",
			"key":     "billing",
			"enabled": true,
			"metadata": map[string]any{
				"apiToken": "abcd12345678",
				"team":     "payments",
			},
		},
	})
	if err != nil {
		t.Fatalf("InvokeTool returned error: %v", err)
	}
	if !apps.created {
		t.Fatalf("expected application create service to be called")
	}
	if apps.lastCreate.Metadata["apiToken"] != "********5678" {
		t.Fatalf("expected tool-specific preserve-format mask, got %#v", apps.lastCreate.Metadata)
	}
	if apps.lastCreate.Metadata["team"] != "payments" {
		t.Fatalf("expected non-target metadata to be preserved, got %#v", apps.lastCreate.Metadata)
	}
}

func TestInvokeToolAppliesOutputRedactionPolicy(t *testing.T) {
	delivery := &fakeDeliveryService{}
	repo := &memoryGatewayRepository{
		accessPolicies: []domainaigateway.AccessPolicy{
			{
				ID:           "policy-output-redact",
				Enabled:      true,
				SubjectType:  "role",
				SubjectID:    "developer",
				Effect:       "allow",
				ToolPatterns: []string{"delivery.applications.detail"},
				Conditions: map[string]any{
					"outputRedactionPolicy": map[string]any{
						"mode":   "sanitize",
						"fields": []any{"application.buildSources.*.config.token"},
					},
				},
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsView,
			},
		},
	}), nil, repo)
	service.SetDeliveryServices(&fakeApplicationService{}, delivery)

	result, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName:   "delivery.applications.detail",
		AIClientID: "codex",
		Input:      map[string]any{"applicationId": "app-1"},
	})
	if err != nil {
		t.Fatalf("InvokeTool returned error: %v", err)
	}
	output, ok := result.Output.(map[string]any)
	if !ok {
		t.Fatalf("expected redacted output map, got %#v", result.Output)
	}
	application := mustMapFieldAs[map[string]any](t, output, "application")
	buildSources := mustMapFieldAs[[]any](t, application, "buildSources")
	buildSource := mustValueAs[map[string]any](t, buildSources[0])
	config := mustMapFieldAs[map[string]any](t, buildSource, "config")
	if config["token"] != "[REDACTED]" {
		t.Fatalf("expected output redaction to sanitize build source config, got %#v", config)
	}
}

func TestInvokeToolWritesProviderUsageSummaryToAudit(t *testing.T) {
	delivery := &fakeDeliveryService{}
	repo := &memoryGatewayRepository{}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsView,
			},
		},
	}), nil, repo)
	service.SetDeliveryServices(&fakeApplicationService{}, delivery)

	result, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName:   "delivery.applications.detail",
		AIClientID: "codex",
		Input:      map[string]any{"applicationId": "app-1"},
	})
	if err != nil {
		t.Fatalf("InvokeTool returned error: %v", err)
	}
	usage := mapValue(result.Audit["providerUsage"])
	if usage["totalTokens"] != float64(50) || usage["totalCost"] != 0.08 {
		t.Fatalf("expected result audit usage summary, got %#v", usage)
	}
	if len(repo.auditLogs) != 1 {
		t.Fatalf("expected one gateway audit log, got %#v", repo.auditLogs)
	}
	auditUsage := mapValue(repo.auditLogs[0].Metadata["providerUsage"])
	if auditUsage["totalTokens"] != float64(50) || auditUsage["inputTokens"] != float64(20) || auditUsage["outputTokens"] != float64(30) || auditUsage["totalCost"] != 0.08 {
		t.Fatalf("expected gateway audit usage summary, got %#v", auditUsage)
	}
	if text := fmt.Sprint(repo.auditLogs[0].Metadata); strings.Contains(text, "do-not-store") {
		t.Fatalf("provider usage summary leaked raw provider payload: %s", text)
	}
}

func TestGatewayProviderUsageSummaryExtractsRelatedIDUsage(t *testing.T) {
	summary := gatewayProviderUsageSummary(map[string]any{
		"message": "ok",
	}, map[string]any{
		"providerUsage": map[string]any{
			"prompt_tokens":      12,
			"completion_tokens":  8,
			"estimatedCostUsd":   0.03,
			"model":              "do-not-store",
			"rawProviderPayload": map[string]any{"secret": "do-not-store"},
		},
	})

	if summary["totalTokens"] != float64(20) || summary["inputTokens"] != float64(12) || summary["outputTokens"] != float64(8) || summary["totalCost"] != 0.03 {
		t.Fatalf("expected relatedIds usage summary, got %#v", summary)
	}
	if text := fmt.Sprint(summary); strings.Contains(text, "do-not-store") {
		t.Fatalf("usage summary leaked raw provider data: %s", text)
	}
}

func TestGatewayProviderUsageSummaryMapsNativeProviderUsageFields(t *testing.T) {
	summary := gatewayProviderUsageSummary(map[string]any{
		"usageMetadata": map[string]any{
			"promptTokenCount":     11,
			"candidatesTokenCount": 17,
			"totalTokenCount":      28,
			"estimatedCostUsd":     0.05,
			"model":                "gemini-do-not-store",
		},
		"ollama": map[string]any{
			"prompt_eval_count": 3,
			"eval_count":        7,
			"raw":               "ollama-do-not-store",
		},
		"anthropic": map[string]any{
			"usage": map[string]any{
				"input_tokens":                5,
				"output_tokens":               13,
				"cache_creation_input_tokens": 2,
				"cache_read_input_tokens":     4,
				"response_cost":               0.02,
				"model":                       "claude-do-not-store",
			},
		},
	}, nil)

	if summary["totalTokens"] != float64(62) || summary["inputTokens"] != float64(25) || summary["outputTokens"] != float64(37) || summary["totalCost"] != 0.07 {
		t.Fatalf("expected native provider usage summary, got %#v", summary)
	}
	if text := fmt.Sprint(summary); strings.Contains(text, "do-not-store") {
		t.Fatalf("native provider usage summary leaked raw provider data: %s", text)
	}
}

func TestGatewayProviderUsageSummaryMapsAdditionalProviderAliases(t *testing.T) {
	summary := gatewayProviderUsageSummary(map[string]any{
		"openai": map[string]any{
			"usage": map[string]any{
				"prompt_tokens":     10,
				"completion_tokens": 15,
				"prompt_tokens_details": map[string]any{
					"cached_tokens": 4,
				},
				"completion_tokens_details": map[string]any{
					"reasoning_tokens": 3,
				},
				"billed_amount": 0.04,
				"model":         "openai-do-not-store",
			},
		},
		"bedrock": map[string]any{
			"inputTextTokens":  8,
			"outputTextTokens": 12,
			"inputImageTokens": 5,
			"estimatedCostUsd": 0.03,
			"trace":            "bedrock-do-not-store",
		},
		"cohere": map[string]any{
			"meta": map[string]any{
				"billed_units": map[string]any{
					"read_units":   7,
					"write_units":  11,
					"credits_used": 0.02,
					"raw_response": "cohere-do-not-store",
				},
			},
		},
	}, nil)

	if summary["totalTokens"] != float64(75) || summary["inputTokens"] != float64(34) || summary["outputTokens"] != float64(41) || !floatNear(summary["totalCost"], 0.09) {
		t.Fatalf("expected additional provider usage aliases, got %#v", summary)
	}
	if text := fmt.Sprint(summary); strings.Contains(text, "do-not-store") {
		t.Fatalf("additional provider usage summary leaked raw provider data: %s", text)
	}
}

func TestGatewayProviderUsageSummaryMapsExpandedProviderAliases(t *testing.T) {
	summary := gatewayProviderUsageSummary(map[string]any{
		"providerUsage": map[string]any{
			"billable_tokens": 90,
			"totalCostMicros": 120000,
			"inputCostMicros": 50000,
			"raw":             "provider-do-not-store",
		},
		"multimodal": map[string]any{
			"usage": map[string]any{
				"textInputTokens":             6,
				"image_input_tokens":          4,
				"audioInputTokens":            3,
				"textOutputTokens":            8,
				"image_output_tokens":         5,
				"audioOutputTokens":           2,
				"completion_reasoning_tokens": 7,
				"outputCostMicros":            70000,
				"trace":                       "multimodal-do-not-store",
			},
		},
		"anthropic_variant": map[string]any{
			"usage": map[string]any{
				"prompt_tokens":             9,
				"prompt_cache_read_tokens":  2,
				"prompt_cache_write_tokens": 3,
				"response_cost":             0.01,
				"model":                     "claude-do-not-store",
			},
		},
		"generic_cost_adapter": map[string]any{
			"promptCost":     0.011,
			"completionCost": 0.019,
			"raw":            "generic-cost-do-not-store",
		},
	}, nil)

	if summary["totalTokens"] != float64(132) || summary["inputTokens"] != float64(27) || summary["outputTokens"] != float64(15) || !floatNear(summary["totalCost"], 0.23) || !floatNear(summary["inputCost"], 0.061) || !floatNear(summary["outputCost"], 0.089) {
		t.Fatalf("expected expanded provider usage aliases, got %#v", summary)
	}
	if text := fmt.Sprint(summary); strings.Contains(text, "do-not-store") {
		t.Fatalf("expanded provider usage summary leaked raw provider data: %s", text)
	}
}

func TestGatewayProviderUsageSummaryMapsEmergingProviderAliases(t *testing.T) {
	summary := gatewayProviderUsageSummary(map[string]any{
		"gemini": map[string]any{
			"usageMetadata": map[string]any{
				"promptTokenCount":        40,
				"cachedContentTokenCount": 12,
				"toolUsePromptTokenCount": 5,
				"candidatesTokenCount":    24,
				"thoughtsTokenCount":      6,
				"totalCostCents":          9,
				"model":                   "gemini-do-not-store",
			},
		},
		"openai": map[string]any{
			"usage": map[string]any{
				"prompt_tokens": 20,
				"prompt_tokens_details": map[string]any{
					"cached_tokens": 3,
					"audio_tokens":  2,
				},
				"completion_tokens": 10,
				"completion_tokens_details": map[string]any{
					"reasoning_tokens":              4,
					"accepted_prediction_tokens":    3,
					"rejected_prediction_tokens":    1,
					"provider_payload_do_not_store": "raw",
				},
				"inputCostCents":  2,
				"outputCostCents": 3,
				"model":           "openai-do-not-store",
			},
		},
	}, nil)

	if summary["totalTokens"] != float64(130) || summary["inputTokens"] != float64(82) || summary["outputTokens"] != float64(48) || !floatNear(summary["totalCost"], 0.14) || !floatNear(summary["inputCost"], 0.02) || !floatNear(summary["outputCost"], 0.03) {
		t.Fatalf("expected emerging provider usage aliases, got %#v", summary)
	}
	if text := fmt.Sprint(summary); strings.Contains(text, "do-not-store") || strings.Contains(text, "provider_payload") {
		t.Fatalf("emerging provider usage summary leaked raw provider data: %s", text)
	}
}

func TestGatewayProviderUsageSummaryMapsChinaCloudProviderAliases(t *testing.T) {
	summary := gatewayProviderUsageSummary(map[string]any{
		"dashscope": map[string]any{
			"usage": map[string]any{
				"input_tokens_count":      10,
				"output_tokens_count":     20,
				"prompt_cache_hit_tokens": 3,
				"raw":                     "dashscope-do-not-store",
			},
		},
		"dashscope_multimodal": map[string]any{
			"usage": map[string]any{
				"image_tokens": 4,
				"video_tokens": 5,
				"audio_tokens": 6,
				"raw":          "dashscope-multimodal-do-not-store",
			},
		},
		"moonshot": map[string]any{
			"usage": map[string]any{
				"prompt_token_usage":     11,
				"completion_token_usage": 13,
				"total_cost_usd":         0.04,
				"model":                  "moonshot-do-not-store",
			},
		},
		"zhipu": map[string]any{
			"usage": map[string]any{
				"promptTokensCount":     7,
				"completionTokensCount": 9,
				"estimatedCostCents":    5,
				"trace":                 "zhipu-do-not-store",
			},
		},
		"qianfan": map[string]any{
			"token_usage": map[string]any{
				"input_token_usage":  8,
				"output_token_usage": 12,
				"total_cost_micros":  60000,
				"raw_response":       "qianfan-do-not-store",
			},
		},
	}, nil)

	if summary["totalTokens"] != float64(108) || summary["inputTokens"] != float64(54) || summary["outputTokens"] != float64(54) || !floatNear(summary["totalCost"], 0.15) {
		t.Fatalf("expected China cloud provider usage aliases, got %#v", summary)
	}
	if text := fmt.Sprint(summary); strings.Contains(text, "do-not-store") {
		t.Fatalf("China cloud provider usage summary leaked raw provider data: %s", text)
	}
}

func TestGatewayProviderUsageSummaryPrefersBilledUsageUnits(t *testing.T) {
	summary := gatewayProviderUsageSummary(map[string]any{
		"cohere_chat": map[string]any{
			"usage": map[string]any{
				"billed_units": map[string]any{
					"input_tokens":  5,
					"output_tokens": 26,
					"raw":           "billed-do-not-store",
				},
				"tokens": map[string]any{
					"input_tokens":  71,
					"output_tokens": 26,
					"raw":           "tokens-do-not-store",
				},
				"cost": 0.012,
			},
		},
		"cohere_rerank": map[string]any{
			"meta": map[string]any{
				"billed_units": map[string]any{
					"search_units": 2,
					"raw":          "search-do-not-store",
				},
			},
		},
		"voyage_embedding": map[string]any{
			"usage": map[string]any{
				"embedding_tokens": 7,
				"raw":              "embedding-do-not-store",
			},
		},
		"custom_gateway": map[string]any{
			"metering": map[string]any{
				"request_units":  3,
				"response_units": 4,
				"raw":            "unit-do-not-store",
			},
		},
	}, nil)

	if summary["totalTokens"] != float64(47) || summary["inputTokens"] != float64(8) || summary["outputTokens"] != float64(30) || !floatNear(summary["totalCost"], 0.012) {
		t.Fatalf("expected billed usage units without double counting generic tokens, got %#v", summary)
	}
	if text := fmt.Sprint(summary); strings.Contains(text, "do-not-store") {
		t.Fatalf("billed usage summary leaked raw provider data: %s", text)
	}
}

func TestGatewayProviderUsageSummaryMapsAgentToolingAliases(t *testing.T) {
	summary := gatewayProviderUsageSummary(map[string]any{
		"brave_search": map[string]any{
			"usage": map[string]any{
				"queryUnits":       2,
				"braveSearchUnits": 1,
				"raw":              "brave-do-not-store",
			},
		},
		"serpapi": map[string]any{
			"metering": map[string]any{
				"searchCredits":   3,
				"serpapiSearches": 4,
				"trace":           "serpapi-do-not-store",
			},
		},
		"browserbase": map[string]any{
			"usage": map[string]any{
				"browserMinutes":  5,
				"browserSessions": 6,
				"pageLoads":       7,
				"session":         "browserbase-do-not-store",
			},
		},
		"rag_tools": map[string]any{
			"providerUsage": map[string]any{
				"documentPages":   8,
				"parsePages":      9,
				"llamaParsePages": 10,
				"characters":      11,
				"chunks":          12,
				"source":          "rag-do-not-store",
			},
		},
		"helicone": map[string]any{
			"billing": map[string]any{
				"requestCount":     13,
				"providerRequests": 14,
				"totalCostMicros":  90000,
				"raw":              "helicone-do-not-store",
			},
		},
	}, nil)

	if summary["totalTokens"] != float64(39) || !floatNear(summary["totalCost"], 0.09) {
		t.Fatalf("expected agent tooling usage aliases, got %#v", summary)
	}
	if text := fmt.Sprint(summary); strings.Contains(text, "do-not-store") {
		t.Fatalf("agent tooling usage summary leaked raw provider data: %s", text)
	}
}

func TestGatewayProviderUsageSummaryIgnoresGenericCountsOutsideUsageContainers(t *testing.T) {
	summary := gatewayProviderUsageSummary(map[string]any{
		"observability": map[string]any{
			"requests":   200,
			"documents":  30,
			"chunks":     40,
			"characters": 5000,
			"raw":        "do-not-store",
		},
	}, nil)

	if summary != nil {
		t.Fatalf("expected generic non-usage counters to be ignored, got %#v", summary)
	}
}

func TestGatewayUsageWithDerivedTotalsPrefersLargestCanonicalAlias(t *testing.T) {
	values := gatewayUsageWithDerivedTotals(map[string]any{
		"queryUnits":       2,
		"requestCount":     13,
		"providerRequests": 14,
		"total_tokens":     "do-not-store",
		"totalCostMicros":  90000,
		"costCents":        12,
		"cost":             "do-not-store",
	})

	if values["totalTokens"] != float64(14) || !floatNear(values["totalCost"], 0.12) {
		t.Fatalf("expected largest canonical usage aliases, got %#v", values)
	}
	if text := fmt.Sprint(values); strings.Contains(text, "do-not-store") {
		t.Fatalf("derived usage totals leaked non-numeric alias payload: %s", text)
	}
}

func TestApproveApprovalRequestAppliesOutputRedactionPolicy(t *testing.T) {
	delivery := &fakeDeliveryService{}
	repo := &memoryGatewayRepository{
		accessPolicies: []domainaigateway.AccessPolicy{
			{
				ID:           "policy-approval-output-redact",
				Enabled:      true,
				SubjectType:  "role",
				SubjectID:    "developer",
				Effect:       "allow",
				ToolPatterns: []string{"delivery.applications.create"},
				ApprovalPolicy: map[string]any{
					"strategy": "require_approval",
				},
				Conditions: map[string]any{
					"outputRedactionPolicy": map[string]any{
						"mode":   "sanitize",
						"fields": []any{"metadata.token"},
					},
				},
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"admin": {
				appaccess.PermAIGatewayManage,
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsCreate,
			},
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsCreate,
			},
		},
	}), nil, repo)
	service.SetDeliveryServices(&fakeApplicationService{}, delivery)

	held, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName: "delivery.applications.create",
		Input: map[string]any{
			"name":    "Billing",
			"key":     "billing",
			"enabled": true,
			"metadata": map[string]any{
				"token": "approval-token",
				"team":  "payments",
			},
		},
	})
	if err != nil {
		t.Fatalf("InvokeTool returned error: %v", err)
	}
	approvalRequestID := mustMapFieldAs[string](t, held.RelatedIDs, "approvalRequestId")
	decision, err := service.ApproveApprovalRequest(context.Background(), testPrincipal("admin"), approvalRequestID, domainaigateway.ApprovalDecisionInput{Comment: "ok"})
	if err != nil {
		t.Fatalf("ApproveApprovalRequest returned error: %v", err)
	}
	if decision.Invocation == nil {
		t.Fatalf("expected replay invocation")
	}
	output := mustValueAs[map[string]any](t, decision.Invocation.Output)
	metadata := mustMapFieldAs[map[string]any](t, output, "metadata")
	if metadata["token"] != "[REDACTED]" || metadata["team"] != "payments" {
		t.Fatalf("expected approved replay output redaction, got %#v", metadata)
	}
	requestOutput := mustValueAs[map[string]any](t, decision.Request.Output)
	requestMetadata := mustMapFieldAs[map[string]any](t, requestOutput, "metadata")
	if requestMetadata["token"] != "[REDACTED]" {
		t.Fatalf("expected persisted approval output to be redacted, got %#v", requestMetadata)
	}
}

func TestApproveApprovalRequestWritesProviderUsageSummaryToAudit(t *testing.T) {
	delivery := &fakeDeliveryService{}
	repo := &memoryGatewayRepository{
		accessPolicies: []domainaigateway.AccessPolicy{
			{
				ID:           "policy-approval-usage",
				Enabled:      true,
				SubjectType:  "role",
				SubjectID:    "developer",
				Effect:       "allow",
				ToolPatterns: []string{"delivery.applications.detail"},
				ApprovalPolicy: map[string]any{
					"strategy": "require_approval",
				},
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"admin": {
				appaccess.PermAIGatewayManage,
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsView,
			},
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsView,
			},
		},
	}), nil, repo)
	service.SetDeliveryServices(&fakeApplicationService{}, delivery)

	held, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName: "delivery.applications.detail",
		Input:    map[string]any{"applicationId": "app-1"},
	})
	if err != nil {
		t.Fatalf("InvokeTool returned error: %v", err)
	}
	approvalRequestID := mustMapFieldAs[string](t, held.RelatedIDs, "approvalRequestId")
	decision, err := service.ApproveApprovalRequest(context.Background(), testPrincipal("admin"), approvalRequestID, domainaigateway.ApprovalDecisionInput{Comment: "ok"})
	if err != nil {
		t.Fatalf("ApproveApprovalRequest returned error: %v", err)
	}
	if decision.Invocation == nil {
		t.Fatalf("expected replay invocation")
	}
	usage := mapValue(decision.Invocation.Audit["providerUsage"])
	if usage["totalTokens"] != float64(50) || usage["totalCost"] != 0.08 {
		t.Fatalf("expected approved replay audit usage, got %#v", usage)
	}
	var executedAudit *domainaigateway.AuditLog
	for index := range repo.auditLogs {
		if repo.auditLogs[index].Action == "ai_gateway.tool.invoke" && repo.auditLogs[index].Result == "success" {
			executedAudit = &repo.auditLogs[index]
		}
	}
	if executedAudit == nil {
		t.Fatalf("expected executed tool audit log, got %#v", repo.auditLogs)
	}
	auditUsage := mapValue(executedAudit.Metadata["providerUsage"])
	if auditUsage["totalTokens"] != float64(50) || auditUsage["totalCost"] != 0.08 {
		t.Fatalf("expected executed gateway audit usage summary, got %#v", auditUsage)
	}
	if text := fmt.Sprint(executedAudit.Metadata); strings.Contains(text, "do-not-store") {
		t.Fatalf("approved replay usage summary leaked raw provider payload: %s", text)
	}
}

func TestInvokeToolRejectsSkillBindingCapabilityMismatch(t *testing.T) {
	apps := &fakeApplicationService{}
	repo := &memoryGatewayRepository{
		skillBindings: []domainaigateway.SkillBinding{
			{
				ID:             "binding-1",
				SubjectType:    "user",
				SubjectID:      "user-1",
				SkillID:        "delivery-developer",
				CapabilityRefs: []string{"delivery.applications.create"},
				Enabled:        true,
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsView,
			},
		},
	}), nil, repo)
	service.SetDeliveryServices(apps, &fakeDeliveryService{})

	_, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName: "delivery.applications.list",
		SkillID:  "delivery-developer",
	})
	if err == nil {
		t.Fatalf("expected skill binding capability mismatch to reject invocation")
	}
	if apps.listed {
		t.Fatalf("application service should not be called after skill binding denial")
	}
}

func TestInvokeToolHoldsDeliveryActionWhenApprovalRequired(t *testing.T) {
	delivery := &fakeDeliveryService{}
	operations := &captureOperationRecorder{}
	repo := &memoryGatewayRepository{}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryBuildsTrigger,
				appaccess.PermDeliveryReleasesTrigger,
			},
		},
	}), nil, repo)
	service.SetDeliveryServices(&fakeApplicationService{}, delivery)
	service.SetOperationRecorder(operations)

	result, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName: "delivery.actions.trigger",
		Input: map[string]any{
			"applicationId":            "app-1",
			"action":                   "build",
			"applicationEnvironmentId": "binding-1",
		},
	})
	if err != nil {
		t.Fatalf("InvokeTool returned error: %v", err)
	}
	assertHeldDeliveryAction(t, result, delivery, repo)
	assertHeldDeliveryAudit(t, repo, operations)
}

func TestApproveApprovalRequestExecutesThroughOwningService(t *testing.T) {
	delivery := &fakeDeliveryService{workflowRunID: "workflow-1"}
	operations := &captureOperationRecorder{}
	repo := &memoryGatewayRepository{}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"admin": {
				appaccess.PermAIGatewayManage,
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryBuildsTrigger,
				appaccess.PermDeliveryReleasesTrigger,
			},
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryBuildsTrigger,
				appaccess.PermDeliveryReleasesTrigger,
			},
		},
	}), nil, repo)
	service.SetDeliveryServices(&fakeApplicationService{}, delivery)
	service.SetOperationRecorder(operations)

	result, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName: "delivery.actions.trigger",
		Input: map[string]any{
			"applicationId":            "app-1",
			"action":                   "build",
			"applicationEnvironmentId": "binding-1",
		},
	})
	if err != nil {
		t.Fatalf("InvokeTool returned error: %v", err)
	}
	requestID := mustMapFieldAs[string](t, result.RelatedIDs, "approvalRequestId")
	decision, err := service.ApproveApprovalRequest(context.Background(), testPrincipal("admin"), requestID, domainaigateway.ApprovalDecisionInput{Comment: "ship it"})
	if err != nil {
		t.Fatalf("ApproveApprovalRequest returned error: %v", err)
	}
	assertApprovedDeliveryDecision(t, decision, delivery, requestID)
	assertApprovalOperationEntries(t, operations.entries, requestID)
	assertApprovalGatewayAudits(t, repo.auditLogs, requestID)
}

func TestApproveApprovalRequestCanTriggerRollbackDeliveryAction(t *testing.T) {
	delivery := &fakeDeliveryService{workflowRunID: "workflow-rollback-1"}
	repo := &memoryGatewayRepository{}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"admin": {
				appaccess.PermAIGatewayManage,
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryBuildsTrigger,
				appaccess.PermDeliveryReleasesTrigger,
			},
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryBuildsTrigger,
				appaccess.PermDeliveryReleasesTrigger,
			},
		},
	}), nil, repo)
	service.SetDeliveryServices(&fakeApplicationService{}, delivery)

	result, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName: "delivery.actions.trigger",
		Input: map[string]any{
			"applicationId":            "app-1",
			"applicationEnvironmentId": "binding-1",
			"action":                   "rollback",
			"releaseBundleId":          "bundle-prev",
		},
	})
	if err != nil {
		t.Fatalf("InvokeTool returned error: %v", err)
	}
	if delivery.triggered {
		t.Fatalf("delivery service should not be called before approval")
	}

	requestID := mustMapFieldAs[string](t, result.RelatedIDs, "approvalRequestId")
	decision, err := service.ApproveApprovalRequest(context.Background(), testPrincipal("admin"), requestID, domainaigateway.ApprovalDecisionInput{Comment: "rollback approved"})
	if err != nil {
		t.Fatalf("ApproveApprovalRequest returned error: %v", err)
	}
	if !delivery.triggered {
		t.Fatalf("approval should execute rollback through delivery service")
	}
	if delivery.lastActionInput.Action != domaindelivery.ApplicationDeliveryActionRollback || delivery.lastActionInput.ReleaseBundleID != "bundle-prev" {
		t.Fatalf("expected rollback action input with release bundle, got %#v", delivery.lastActionInput)
	}
	if decision.Invocation == nil || decision.Invocation.Result != "success" || decision.Invocation.RelatedIDs["workflowRunId"] != "workflow-rollback-1" {
		t.Fatalf("expected rollback workflow linkage in invocation, got %#v", decision.Invocation)
	}
	if decision.Request.RelatedIDs["workflowRunId"] != "workflow-rollback-1" {
		t.Fatalf("expected approval request workflow linkage, got %#v", decision.Request.RelatedIDs)
	}
}

func TestRejectAndCancelApprovalRequestTransitionWithoutMutation(t *testing.T) {
	delivery := &fakeDeliveryService{}
	repo := &memoryGatewayRepository{}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"admin": {
				appaccess.PermAIGatewayManage,
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryBuildsTrigger,
				appaccess.PermDeliveryReleasesTrigger,
			},
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryBuildsTrigger,
				appaccess.PermDeliveryReleasesTrigger,
			},
		},
	}), nil, repo)
	service.SetDeliveryServices(&fakeApplicationService{}, delivery)

	first, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{ToolName: "delivery.actions.trigger", Input: map[string]any{"applicationId": "app-1", "action": "build"}})
	if err != nil {
		t.Fatalf("InvokeTool returned error: %v", err)
	}
	firstRequestID := mustMapFieldAs[string](t, first.RelatedIDs, "approvalRequestId")
	rejected, err := service.RejectApprovalRequest(context.Background(), testPrincipal("admin"), firstRequestID, domainaigateway.ApprovalDecisionInput{Comment: "no window"})
	if err != nil {
		t.Fatalf("RejectApprovalRequest returned error: %v", err)
	}
	if rejected.Request.Status != "rejected" || delivery.triggered {
		t.Fatalf("expected rejected without mutation, request=%#v delivery=%#v", rejected.Request, delivery)
	}

	second, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{ToolName: "delivery.actions.trigger", Input: map[string]any{"applicationId": "app-1", "action": "build"}})
	if err != nil {
		t.Fatalf("InvokeTool returned error: %v", err)
	}
	secondRequestID := mustMapFieldAs[string](t, second.RelatedIDs, "approvalRequestId")
	canceled, err := service.CancelApprovalRequest(context.Background(), testPrincipal("admin"), secondRequestID, domainaigateway.ApprovalDecisionInput{Comment: "duplicate"})
	if err != nil {
		t.Fatalf("CancelApprovalRequest returned error: %v", err)
	}
	if canceled.Request.Status != "canceled" || delivery.triggered {
		t.Fatalf("expected canceled without mutation, request=%#v delivery=%#v", canceled.Request, delivery)
	}
}

func TestListApprovalRequestsExpiresTimedOutRequests(t *testing.T) {
	expiredAt := time.Now().UTC().Add(-time.Minute)
	repo := &memoryGatewayRepository{
		approvalRequests: []domainaigateway.ApprovalRequest{
			{
				ID:            "approval-1",
				Status:        "pending",
				Strategy:      "require_approval",
				ActorType:     "user",
				ActorID:       "user-1",
				ToolName:      "delivery.actions.trigger",
				RiskLevel:     domainaigateway.RiskLevelExecute,
				ResourceScope: map[string]any{},
				ToolInput:     map[string]any{"applicationId": "app-1"},
				RelatedIDs:    map[string]any{"approvalRequestId": "approval-1"},
				Summary:       "pending",
				ExpiresAt:     &expiredAt,
				CreatedAt:     expiredAt.Add(-time.Hour),
				UpdatedAt:     expiredAt.Add(-time.Hour),
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"admin": {appaccess.PermAIGatewayManage},
		},
	}), nil, repo)

	items, err := service.ListApprovalRequests(context.Background(), testPrincipal("admin"), domainaigateway.ApprovalRequestFilter{Status: "timeout"})
	if err != nil {
		t.Fatalf("ListApprovalRequests returned error: %v", err)
	}
	if len(items) != 1 || items[0].Status != "timeout" {
		t.Fatalf("expected timed out request, got %#v", items)
	}
}

func TestListApprovalRequestsFiltersByID(t *testing.T) {
	now := time.Now().UTC()
	repo := &memoryGatewayRepository{
		approvalRequests: []domainaigateway.ApprovalRequest{
			{
				ID:        "approval-1",
				Status:    "executed",
				Strategy:  "require_approval",
				ActorType: "user",
				ActorID:   "user-1",
				ToolName:  "delivery.actions.trigger",
				RiskLevel: domainaigateway.RiskLevelExecute,
				Summary:   "executed",
				CreatedAt: now,
				UpdatedAt: now,
			},
			{
				ID:        "approval-2",
				Status:    "pending",
				Strategy:  "require_approval",
				ActorType: "user",
				ActorID:   "user-2",
				ToolName:  "delivery.actions.trigger",
				RiskLevel: domainaigateway.RiskLevelExecute,
				Summary:   "pending",
				CreatedAt: now,
				UpdatedAt: now,
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"admin": {appaccess.PermAIGatewayManage},
		},
	}), nil, repo)

	items, err := service.ListApprovalRequests(context.Background(), testPrincipal("admin"), domainaigateway.ApprovalRequestFilter{ID: "approval-1"})
	if err != nil {
		t.Fatalf("ListApprovalRequests returned error: %v", err)
	}
	if len(items) != 1 || items[0].ID != "approval-1" || items[0].Status != "executed" {
		t.Fatalf("expected approval-1 only, got %#v", items)
	}
}

func TestApprovalRequestUsesInlineApprovalPolicySLAAndRouting(t *testing.T) {
	now := time.Now().UTC()
	apps := &fakeApplicationService{}
	repo := &memoryGatewayRepository{
		accessPolicies: []domainaigateway.AccessPolicy{
			{
				ID:           "policy-approval",
				Enabled:      true,
				SubjectType:  "role",
				SubjectID:    "developer",
				Effect:       "allow",
				ToolPatterns: []string{"delivery.applications.create"},
				ApprovalPolicy: map[string]any{
					"strategy":          "require_approval",
					"approvalPolicyRef": "gateway-fast",
					"approvalMode":      "all",
					"approverRoles":     []any{"release-manager", "security-reviewer"},
					"requiredApprovals": 2,
					"slaMinutes":        15,
					"requiredTeamApprovals": map[string]any{
						"platform-ops": 1,
					},
					"changeWindow": map[string]any{
						"startsAt": now.Add(-time.Hour).Format(time.RFC3339),
						"endsAt":   now.Add(time.Hour).Format(time.RFC3339),
					},
				},
			},
		},
	}
	service := newGroupQuotaTestService(repo, apps)

	held, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName: "delivery.applications.create",
		Input:    map[string]any{"name": "Payments", "key": "payments"},
	})
	if err != nil {
		t.Fatalf("InvokeTool returned error: %v", err)
	}
	if len(repo.approvalRequests) != 1 {
		t.Fatalf("expected approval request, got %#v", repo.approvalRequests)
	}
	request := repo.approvalRequests[0]
	if request.ApprovalPolicyRef != "gateway-fast" {
		t.Fatalf("expected approval policy ref, got %#v", request)
	}
	if request.ExpiresAt == nil || request.ExpiresAt.Before(now.Add(14*time.Minute)) || request.ExpiresAt.After(now.Add(16*time.Minute)) {
		t.Fatalf("expected inline SLA-based expiry around 15m, got %#v", request.ExpiresAt)
	}
	routing := mapValue(request.RelatedIDs["approvalRouting"])
	if routing["approvalMode"] != "all" || routing["requiredApprovals"] != 2 {
		t.Fatalf("expected inline approval routing, got %#v", routing)
	}
	if fmt.Sprint(routing["candidateRoles"]) != "[release-manager security-reviewer]" {
		t.Fatalf("expected Gateway policy roles, got %#v", routing["candidateRoles"])
	}
	if fmt.Sprint(routing["requiredTeamApprovals"]) != "map[platform-ops:1]" || len(mapValue(routing["changeWindow"])) == 0 {
		t.Fatalf("expected inline team quota and change window, got %#v", routing)
	}

	releaseApprover := testPrincipal("release-manager")
	releaseApprover.UserID = "release-1"
	releaseApprover.Teams = []string{"platform-ops"}
	requestID := mustMapFieldAs[string](t, held.RelatedIDs, "approvalRequestId")
	first, err := service.ApproveApprovalRequest(context.Background(), releaseApprover, requestID, domainaigateway.ApprovalDecisionInput{Comment: "release"})
	if err != nil {
		t.Fatalf("release approval returned error: %v", err)
	}
	if first.Request.Status != "pending" || apps.created {
		t.Fatalf("expected inline policy quorum to keep request pending, request=%#v apps=%#v", first.Request, apps)
	}
	securityApprover := testPrincipal("security-reviewer")
	securityApprover.UserID = "security-1"
	final, err := service.ApproveApprovalRequest(context.Background(), securityApprover, requestID, domainaigateway.ApprovalDecisionInput{Comment: "security"})
	if err != nil {
		t.Fatalf("security approval returned error: %v", err)
	}
	if final.Request.Status != "executed" || !apps.created {
		t.Fatalf("expected inline policy quorum to execute after second approval, request=%#v apps=%#v", final.Request, apps)
	}
}

func TestApprovalRequestStoresRoutingAndRestrictsDecisionCandidates(t *testing.T) {
	now := time.Now().UTC()
	apps := &fakeApplicationService{}
	repo := &memoryGatewayRepository{
		accessPolicies: []domainaigateway.AccessPolicy{
			{
				ID:           "policy-routed-approval",
				Enabled:      true,
				SubjectType:  "role",
				SubjectID:    "developer",
				Effect:       "allow",
				ToolPatterns: []string{"delivery.applications.create"},
				ApprovalPolicy: map[string]any{
					"strategy":      "require_approval",
					"approverRoles": []any{"release-manager"},
					"approverTeams": []any{"platform-ops"},
					"onCallRef":     "oncall-prod",
					"changeWindow": map[string]any{
						"startsAt": now.Add(-time.Hour).Format(time.RFC3339),
						"endsAt":   now.Add(time.Hour).Format(time.RFC3339),
						"timezone": "UTC",
					},
				},
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"admin": {
				appaccess.PermAIGatewayManage,
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsCreate,
			},
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsCreate,
			},
			"release-manager": {
				appaccess.PermAIGatewayManage,
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsCreate,
			},
		},
	}), nil, repo)
	service.SetDeliveryServices(apps, &fakeDeliveryService{})

	held, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName: "delivery.applications.create",
		Input:    map[string]any{"name": "Payments", "key": "payments"},
	})
	if err != nil {
		t.Fatalf("InvokeTool returned error: %v", err)
	}
	requestID := mustMapFieldAs[string](t, held.RelatedIDs, "approvalRequestId")
	request := repo.approvalRequests[0]
	routing := mapValue(request.RelatedIDs["approvalRouting"])
	if fmt.Sprint(routing["candidateRoles"]) != "[release-manager]" || fmt.Sprint(routing["candidateTeams"]) != "[platform-ops]" || routing["onCallRef"] != "oncall-prod" {
		t.Fatalf("expected approval routing metadata, got %#v", routing)
	}
	if len(mapValue(routing["changeWindow"])) == 0 {
		t.Fatalf("expected change window metadata, got %#v", routing)
	}

	if _, err := service.ApproveApprovalRequest(context.Background(), testPrincipal("admin"), requestID, domainaigateway.ApprovalDecisionInput{Comment: "not my queue"}); err == nil || !strings.Contains(err.Error(), "candidate approvers") {
		t.Fatalf("expected non-candidate approval rejection, got %v", err)
	}
	decision, err := service.ApproveApprovalRequest(context.Background(), testPrincipal("release-manager"), requestID, domainaigateway.ApprovalDecisionInput{Comment: "approved"})
	if err != nil {
		t.Fatalf("ApproveApprovalRequest returned error for candidate: %v", err)
	}
	if decision.Request.Status != "executed" || !apps.created {
		t.Fatalf("expected candidate approval to execute owning service, request=%#v apps=%#v", decision.Request, apps)
	}
}

func TestApprovalRequestResolvesOnCallCandidate(t *testing.T) {
	apps := &fakeApplicationService{}
	repo := &memoryGatewayRepository{
		accessPolicies: []domainaigateway.AccessPolicy{
			{
				ID:           "policy-oncall-approval",
				Enabled:      true,
				SubjectType:  "role",
				SubjectID:    "developer",
				Effect:       "allow",
				ToolPatterns: []string{"delivery.applications.create"},
				ApprovalPolicy: map[string]any{
					"strategy":  "require_approval",
					"onCallRef": "prod-release-oncall",
				},
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsCreate,
			},
			"release-manager": {
				appaccess.PermAIGatewayManage,
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsCreate,
			},
		},
	}), nil, repo)
	oncall := &fakeOnCallResolver{
		current: map[string]any{
			"currentParticipant": "oncall-user",
			"participants":       []any{"oncall-user", "backup-user"},
			"scheduleId":         "schedule-prod",
			"schedule":           "Production",
			"rotationId":         "rotation-prod",
			"rotation":           "Primary",
			"windowStart":        "2026-05-29T00:00:00Z",
			"windowEnd":          "2026-05-29T12:00:00Z",
		},
	}
	service.SetDeliveryServices(apps, &fakeDeliveryService{})
	service.SetOnCallResolver(oncall)

	held, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName: "delivery.applications.create",
		Input: map[string]any{
			"name":      "Payments",
			"key":       "payments",
			"service":   "payments-api",
			"clusterId": "cluster-a",
			"namespace": "prod",
		},
	})
	if err != nil {
		t.Fatalf("InvokeTool returned error: %v", err)
	}
	requestID := mustMapFieldAs[string](t, held.RelatedIDs, "approvalRequestId")
	if oncall.lastRef != "prod-release-oncall" {
		t.Fatalf("expected on-call ref lookup, got %q", oncall.lastRef)
	}
	routing := mapValue(repo.approvalRequests[0].RelatedIDs["approvalRouting"])
	if fmt.Sprint(routing["candidateUserIds"]) != "[oncall-user]" || fmt.Sprint(routing["onCallCandidateUserIds"]) != "[oncall-user]" {
		t.Fatalf("expected resolved on-call candidate routing, got %#v", routing)
	}
	resolution := mapValue(routing["onCallResolution"])
	if resolution["status"] != "resolved" || resolution["source"] != "current_oncall" || resolution["scheduleId"] != "schedule-prod" {
		t.Fatalf("expected resolved on-call metadata, got %#v", resolution)
	}

	if _, err := service.ApproveApprovalRequest(context.Background(), testPrincipal("release-manager"), requestID, domainaigateway.ApprovalDecisionInput{Comment: "not on call"}); err == nil || !strings.Contains(err.Error(), "candidate approvers") {
		t.Fatalf("expected non-on-call approval rejection, got %v", err)
	}
	approver := testPrincipal("release-manager")
	approver.UserID = "oncall-user"
	approver.UserName = "On Call User"
	decision, err := service.ApproveApprovalRequest(context.Background(), approver, requestID, domainaigateway.ApprovalDecisionInput{Comment: "current on-call"})
	if err != nil {
		t.Fatalf("on-call approval returned error: %v", err)
	}
	if decision.Request.Status != "executed" || !apps.created {
		t.Fatalf("expected on-call approval to execute owning service, request=%#v apps=%#v", decision.Request, apps)
	}
}

func TestApprovalRequestRequiresMultipleApprovalsBeforeReplay(t *testing.T) {
	apps := &fakeApplicationService{}
	repo := &memoryGatewayRepository{
		accessPolicies: []domainaigateway.AccessPolicy{
			{
				ID:           "policy-multi-approval",
				Enabled:      true,
				SubjectType:  "role",
				SubjectID:    "developer",
				Effect:       "allow",
				ToolPatterns: []string{"delivery.applications.create"},
				ApprovalPolicy: map[string]any{
					"strategy":          "require_approval",
					"approverUsers":     []any{"approver-1", "approver-2"},
					"requiredApprovals": 2,
				},
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsCreate,
			},
			"release-manager": {
				appaccess.PermAIGatewayManage,
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsCreate,
			},
		},
	}), nil, repo)
	service.SetDeliveryServices(apps, &fakeDeliveryService{})

	held, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName: "delivery.applications.create",
		Input:    map[string]any{"name": "Payments", "key": "payments"},
	})
	if err != nil {
		t.Fatalf("InvokeTool returned error: %v", err)
	}
	requestID := mustMapFieldAs[string](t, held.RelatedIDs, "approvalRequestId")
	firstApprover := testPrincipal("release-manager")
	firstApprover.UserID = "approver-1"
	firstApprover.UserName = "Approver One"
	assertFirstAndRepeatedApproval(t, service, apps, firstApprover, requestID)
	secondApprover := testPrincipal("release-manager")
	secondApprover.UserID = "approver-2"
	secondApprover.UserName = "Approver Two"
	assertSecondApprovalExecutes(t, service, apps, secondApprover, requestID)
}

func TestApprovalRequestRequiresRoleAndTeamQuotasBeforeReplay(t *testing.T) {
	apps := &fakeApplicationService{}
	repo := &memoryGatewayRepository{
		accessPolicies: []domainaigateway.AccessPolicy{
			{
				ID:           "policy-group-quorum",
				Enabled:      true,
				SubjectType:  "role",
				SubjectID:    "developer",
				Effect:       "allow",
				ToolPatterns: []string{"delivery.applications.create"},
				ApprovalPolicy: map[string]any{
					"strategy":          "require_approval",
					"requiredApprovals": 3,
					"requiredRoleApprovals": map[string]any{
						"release-manager":   2,
						"security-reviewer": 1,
					},
					"requiredTeamApprovals": map[string]any{
						"platform-ops": 1,
					},
				},
			},
		},
	}
	service := newGroupQuotaTestService(repo, apps)

	held, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName: "delivery.applications.create",
		Input:    map[string]any{"name": "Payments", "key": "payments"},
	})
	if err != nil {
		t.Fatalf("InvokeTool returned error: %v", err)
	}
	requestID := mustMapFieldAs[string](t, held.RelatedIDs, "approvalRequestId")
	initialRouting := mapValue(repo.approvalRequests[0].RelatedIDs["approvalRouting"])
	if fmt.Sprint(initialRouting["candidateRoles"]) != "[release-manager security-reviewer]" || fmt.Sprint(initialRouting["candidateTeams"]) != "[platform-ops]" {
		t.Fatalf("expected quota groups to become candidate routing, got %#v", initialRouting)
	}

	first := testPrincipal("release-manager")
	first.UserID = "release-1"
	first.UserName = "Release One"
	firstDecision, err := service.ApproveApprovalRequest(context.Background(), first, requestID, domainaigateway.ApprovalDecisionInput{Comment: "release one"})
	if err != nil {
		t.Fatalf("first approval returned error: %v", err)
	}
	firstRouting := mapValue(firstDecision.Request.RelatedIDs["approvalRouting"])
	if firstDecision.Request.Status != "pending" || apps.created {
		t.Fatalf("expected first quota approval to stay pending, request=%#v apps=%#v", firstDecision.Request, apps)
	}
	if fmt.Sprint(firstRouting["pendingRequirements"]) != "[approvals:1/3 role:release-manager:1/2 role:security-reviewer:0/1 team:platform-ops:0/1]" {
		t.Fatalf("expected first quota pending requirements, got %#v", firstRouting["pendingRequirements"])
	}

	second := testPrincipal("release-manager")
	second.UserID = "release-2"
	second.UserName = "Release Two"
	second.Teams = []string{"platform-ops"}
	secondDecision, err := service.ApproveApprovalRequest(context.Background(), second, requestID, domainaigateway.ApprovalDecisionInput{Comment: "release two"})
	if err != nil {
		t.Fatalf("second approval returned error: %v", err)
	}
	secondRouting := mapValue(secondDecision.Request.RelatedIDs["approvalRouting"])
	if secondDecision.Request.Status != "pending" || apps.created {
		t.Fatalf("expected second quota approval to stay pending, request=%#v apps=%#v", secondDecision.Request, apps)
	}
	if fmt.Sprint(secondRouting["roleApprovedCounts"]) != "map[release-manager:2]" || fmt.Sprint(secondRouting["teamApprovedCounts"]) != "map[platform-ops:1]" {
		t.Fatalf("expected role/team approved counts after second vote, got %#v", secondRouting)
	}
	if fmt.Sprint(secondRouting["pendingRequirements"]) != "[approvals:2/3 role:security-reviewer:0/1]" {
		t.Fatalf("expected security role still pending, got %#v", secondRouting["pendingRequirements"])
	}

	security := testPrincipal("security-reviewer")
	security.UserID = "security-1"
	security.UserName = "Security One"
	finalDecision, err := service.ApproveApprovalRequest(context.Background(), security, requestID, domainaigateway.ApprovalDecisionInput{Comment: "security"})
	if err != nil {
		t.Fatalf("security approval returned error: %v", err)
	}
	finalRouting := mapValue(finalDecision.Request.RelatedIDs["approvalRouting"])
	if finalDecision.Request.Status != "executed" || !apps.created {
		t.Fatalf("expected group quota approval to execute owning service, request=%#v apps=%#v", finalDecision.Request, apps)
	}
	if finalRouting["approvedCount"] != 3 || len(gatewayApprovalDecisions(finalRouting)) != 3 {
		t.Fatalf("expected final 3/3 approval routing, got %#v", finalRouting)
	}
	if pending, ok := finalRouting["pendingRequirements"]; ok {
		t.Fatalf("expected final routing to clear pending requirements, got %#v", pending)
	}
}

func TestApprovalRequestAnyModeReplaysWhenAnyQuotaMatches(t *testing.T) {
	apps := &fakeApplicationService{}
	repo := &memoryGatewayRepository{
		accessPolicies: []domainaigateway.AccessPolicy{
			{
				ID:           "policy-any-quorum",
				Enabled:      true,
				SubjectType:  "role",
				SubjectID:    "developer",
				Effect:       "allow",
				ToolPatterns: []string{"delivery.applications.create"},
				ApprovalPolicy: map[string]any{
					"strategy":     "require_approval",
					"approvalMode": "any",
					"requiredRoleApprovals": map[string]any{
						"release-manager": 2,
					},
					"requiredTeamApprovals": map[string]any{
						"platform-ops": 1,
					},
				},
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsCreate,
			},
			"release-manager": {
				appaccess.PermAIGatewayManage,
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsCreate,
			},
		},
	}), nil, repo)
	service.SetDeliveryServices(apps, &fakeDeliveryService{})

	held, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName: "delivery.applications.create",
		Input:    map[string]any{"name": "Payments", "key": "payments"},
	})
	if err != nil {
		t.Fatalf("InvokeTool returned error: %v", err)
	}
	requestID := mustMapFieldAs[string](t, held.RelatedIDs, "approvalRequestId")
	approver := testPrincipal("release-manager")
	approver.UserID = "release-1"
	approver.UserName = "Release One"
	approver.Teams = []string{"platform-ops"}
	decision, err := service.ApproveApprovalRequest(context.Background(), approver, requestID, domainaigateway.ApprovalDecisionInput{Comment: "ops path"})
	if err != nil {
		t.Fatalf("approval returned error: %v", err)
	}
	if decision.Request.Status != "executed" || !apps.created {
		t.Fatalf("expected any-mode team quota to execute owning service, request=%#v apps=%#v", decision.Request, apps)
	}
	routing := mapValue(decision.Request.RelatedIDs["approvalRouting"])
	if routing["approvalMode"] != "any" {
		t.Fatalf("expected any approval mode, got %#v", routing)
	}
	if fmt.Sprint(routing["satisfiedRequirements"]) != "[team:platform-ops:1/1]" {
		t.Fatalf("expected team quota to satisfy any mode, got %#v", routing["satisfiedRequirements"])
	}
}

func TestApprovalRequestAdvancesApprovalStagesBeforeReplay(t *testing.T) {
	apps := &fakeApplicationService{}
	repo := &memoryGatewayRepository{
		accessPolicies: []domainaigateway.AccessPolicy{
			{
				ID:           "policy-staged-approval",
				Enabled:      true,
				SubjectType:  "role",
				SubjectID:    "developer",
				Effect:       "allow",
				ToolPatterns: []string{"delivery.applications.create"},
				ApprovalPolicy: map[string]any{
					"strategy": "require_approval",
					"approvalStages": []any{
						map[string]any{
							"name":              "release",
							"approverRoles":     []any{"release-manager"},
							"requiredApprovals": 1,
						},
						map[string]any{
							"name":              "security",
							"approverRoles":     []any{"security-reviewer"},
							"requiredApprovals": 1,
						},
					},
				},
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsCreate,
			},
			"release-manager": {
				appaccess.PermAIGatewayManage,
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsCreate,
			},
			"security-reviewer": {
				appaccess.PermAIGatewayManage,
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsCreate,
			},
		},
	}), nil, repo)
	service.SetDeliveryServices(apps, &fakeDeliveryService{})

	held, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName: "delivery.applications.create",
		Input:    map[string]any{"name": "Payments", "key": "payments"},
	})
	if err != nil {
		t.Fatalf("InvokeTool returned error: %v", err)
	}
	requestID := mustMapFieldAs[string](t, held.RelatedIDs, "approvalRequestId")
	initialRouting := mapValue(repo.approvalRequests[0].RelatedIDs["approvalRouting"])
	if initialRouting["currentStageIndex"] != 0 || initialRouting["currentStageName"] != "release" || len(gatewayApprovalStages(initialRouting)) != 2 {
		t.Fatalf("expected initial staged routing, got %#v", initialRouting)
	}

	releaseApprover := testPrincipal("release-manager")
	releaseApprover.UserID = "release-1"
	releaseApprover.UserName = "Release One"
	first, err := service.ApproveApprovalRequest(context.Background(), releaseApprover, requestID, domainaigateway.ApprovalDecisionInput{Comment: "release"})
	if err != nil {
		t.Fatalf("release approval returned error: %v", err)
	}
	firstRouting := mapValue(first.Request.RelatedIDs["approvalRouting"])
	if first.Request.Status != "pending" || apps.created {
		t.Fatalf("expected release approval to advance stage without replay, request=%#v apps=%#v", first.Request, apps)
	}
	if firstRouting["currentStageIndex"] != 1 || firstRouting["currentStageName"] != "security" {
		t.Fatalf("expected current stage to advance to security, got %#v", firstRouting)
	}
	if fmt.Sprint(firstRouting["pendingRequirements"]) != "[stage:1:approvals:0/1]" {
		t.Fatalf("expected security stage pending requirement, got %#v", firstRouting["pendingRequirements"])
	}
	if len(gatewayApprovalStageHistory(firstRouting)) != 1 {
		t.Fatalf("expected one completed stage history entry, got %#v", firstRouting["stageHistory"])
	}

	if _, err := service.ApproveApprovalRequest(context.Background(), releaseApprover, requestID, domainaigateway.ApprovalDecisionInput{Comment: "wrong stage"}); err == nil || !strings.Contains(err.Error(), "candidate approvers") {
		t.Fatalf("expected release approver to be rejected on security stage, got %v", err)
	}

	securityApprover := testPrincipal("security-reviewer")
	securityApprover.UserID = "security-1"
	securityApprover.UserName = "Security One"
	final, err := service.ApproveApprovalRequest(context.Background(), securityApprover, requestID, domainaigateway.ApprovalDecisionInput{Comment: "security"})
	if err != nil {
		t.Fatalf("security approval returned error: %v", err)
	}
	finalRouting := mapValue(final.Request.RelatedIDs["approvalRouting"])
	if final.Request.Status != "executed" || !apps.created {
		t.Fatalf("expected final staged approval to execute owning service, request=%#v apps=%#v", final.Request, apps)
	}
	if finalRouting["currentStageIndex"] != 1 || len(gatewayApprovalDecisions(finalRouting)) != 2 {
		t.Fatalf("expected final routing to include both staged decisions, got %#v", finalRouting)
	}
}

func TestGetApprovalTimelineAggregatesTraceAndAuditEvents(t *testing.T) {
	now := time.Now().UTC()
	stageTime := now.Add(2 * time.Minute)
	decisionTime := now.Add(3 * time.Minute)
	repo := approvalTimelineTestRepository(now, stageTime, decisionTime)
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"admin": {appaccess.PermAIGatewayManage},
		},
	}), nil, repo)

	timeline, err := service.GetApprovalTimeline(context.Background(), testPrincipal("admin"), "approval-1")
	if err != nil {
		t.Fatalf("GetApprovalTimeline returned error: %v", err)
	}
	assertApprovalTimeline(t, timeline)
}

func approvalTimelineTestRepository(now, stageTime, decisionTime time.Time) *memoryGatewayRepository {
	return &memoryGatewayRepository{
		approvalRequests: []domainaigateway.ApprovalRequest{
			{
				ID:        "approval-1",
				Status:    "pending",
				Strategy:  "require_approval",
				ActorType: "user",
				ActorID:   "developer-1",
				ActorName: "Developer One",
				ToolName:  "delivery.actions.trigger",
				RiskLevel: domainaigateway.RiskLevelExecute,
				RelatedIDs: map[string]any{
					"workflowRunId":   "workflow-1",
					"executionTaskId": "task-1",
					"approvalRouting": map[string]any{
						"approvalMode":        "all",
						"requiredApprovals":   2,
						"approvedCount":       1,
						"currentStageIndex":   1,
						"currentStageName":    "security",
						"stageCount":          2,
						"candidateRoles":      []any{"security-reviewer"},
						"pendingRequirements": []any{"stage:1:approvals:0/1"},
						"decisions": []any{
							map[string]any{
								"userId":     "release-1",
								"userName":   "Release One",
								"roles":      []any{"release-manager"},
								"result":     "approved",
								"comment":    "release token=secret",
								"stageIndex": 0,
								"stageName":  "release",
								"decidedAt":  decisionTime.Format(time.RFC3339),
							},
						},
						"stageHistory": []any{
							map[string]any{
								"stageIndex":  0,
								"stageName":   "release",
								"result":      "approved",
								"completedAt": stageTime.Format(time.RFC3339),
							},
						},
					},
				},
				Summary:   "pending",
				CreatedAt: now,
				UpdatedAt: decisionTime,
			},
		},
		auditLogs: []domainaigateway.AuditLog{
			{
				ID:        "audit-other",
				ToolName:  "delivery.actions.trigger",
				Action:    "ai_gateway.tool.invoke",
				Result:    "pending",
				Summary:   "other",
				Metadata:  map[string]any{"approvalRequestId": "approval-other"},
				CreatedAt: now.Add(time.Minute),
			},
			{
				ID:        "audit-1",
				ActorType: "user",
				ActorID:   "release-1",
				ActorName: "Release One",
				ToolName:  "delivery.actions.trigger",
				Action:    "ai_gateway.approval.vote",
				Result:    "pending",
				Summary:   "vote token=secret",
				Metadata: map[string]any{
					"approvalRequestId": "approval-1",
					"token":             "secret",
				},
				CreatedAt: now.Add(4 * time.Minute),
			},
		},
	}
}

func assertApprovalTimeline(t *testing.T, timeline domainaigateway.ApprovalTimeline) {
	t.Helper()
	if timeline.Request.ApprovalTrace == nil || timeline.Trace == nil {
		t.Fatalf("expected approval trace in timeline: %#v", timeline)
	}
	if timeline.Trace.WorkflowRunID != "workflow-1" || timeline.Trace.ExecutionTaskID != "task-1" {
		t.Fatalf("expected related workflow/task ids, got %#v", timeline.Trace)
	}
	if timeline.Trace.CurrentStageIndex == nil || *timeline.Trace.CurrentStageIndex != 1 || timeline.Trace.CurrentStageName != "security" {
		t.Fatalf("expected current security stage, got %#v", timeline.Trace)
	}
	if len(timeline.Trace.Decisions) != 1 || timeline.Trace.Decisions[0].Comment != "release token=[REDACTED]" {
		t.Fatalf("expected redacted decision trace, got %#v", timeline.Trace.Decisions)
	}
	if len(timeline.Trace.StageHistory) != 1 || timeline.Trace.StageHistory[0].StageName != "release" {
		t.Fatalf("expected stage history, got %#v", timeline.Trace.StageHistory)
	}
	if len(timeline.Events) != 4 {
		t.Fatalf("expected request, decision, stage, and matching audit events, got %#v", timeline.Events)
	}
	if timeline.Events[0].Kind != "request" || timeline.Events[len(timeline.Events)-1].ID != "audit-1" {
		t.Fatalf("expected chronological timeline events, got %#v", timeline.Events)
	}
	if timeline.Events[len(timeline.Events)-1].Metadata["token"] != "[REDACTED]" {
		t.Fatalf("expected timeline audit metadata to be redacted, got %#v", timeline.Events[len(timeline.Events)-1].Metadata)
	}
}

func TestApprovalRequestRejectsApproveOutsideChangeWindow(t *testing.T) {
	now := time.Now().UTC()
	apps := &fakeApplicationService{}
	repo := &memoryGatewayRepository{
		accessPolicies: []domainaigateway.AccessPolicy{
			{
				ID:           "policy-window-approval",
				Enabled:      true,
				SubjectType:  "role",
				SubjectID:    "developer",
				Effect:       "allow",
				ToolPatterns: []string{"delivery.applications.create"},
				ApprovalPolicy: map[string]any{
					"strategy":      "require_approval",
					"approverRoles": []any{"release-manager"},
					"changeWindow": map[string]any{
						"startsAt": now.Add(time.Hour).Format(time.RFC3339),
						"endsAt":   now.Add(2 * time.Hour).Format(time.RFC3339),
					},
				},
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsCreate,
			},
			"release-manager": {
				appaccess.PermAIGatewayManage,
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsCreate,
			},
		},
	}), nil, repo)
	service.SetDeliveryServices(apps, &fakeDeliveryService{})

	held, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName: "delivery.applications.create",
		Input:    map[string]any{"name": "Payments", "key": "payments"},
	})
	if err != nil {
		t.Fatalf("InvokeTool returned error: %v", err)
	}
	requestID := mustMapFieldAs[string](t, held.RelatedIDs, "approvalRequestId")
	_, err = service.ApproveApprovalRequest(context.Background(), testPrincipal("release-manager"), requestID, domainaigateway.ApprovalDecisionInput{Comment: "too early"})
	if err == nil || !strings.Contains(err.Error(), "change window") {
		t.Fatalf("expected change-window approval rejection, got %v", err)
	}
	if apps.created {
		t.Fatalf("application service should not be called outside change window")
	}
}

func TestInvokeToolCanTriggerDeliveryActionWhenRiskPolicyAllows(t *testing.T) {
	delivery := &fakeDeliveryService{}
	repo := &memoryGatewayRepository{
		accessPolicies: []domainaigateway.AccessPolicy{
			{
				ID:             "policy-allow-execute",
				Enabled:        true,
				SubjectType:    "role",
				SubjectID:      "developer",
				Effect:         "allow",
				ToolPatterns:   []string{"delivery.actions.trigger"},
				RiskLevels:     []domainaigateway.RiskLevel{domainaigateway.RiskLevelExecute},
				ApprovalPolicy: map[string]any{"strategy": "allow"},
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryBuildsTrigger,
				appaccess.PermDeliveryReleasesTrigger,
			},
		},
	}), nil, repo)
	service.SetDeliveryServices(&fakeApplicationService{}, delivery)

	result, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName: "delivery.actions.trigger",
		Input: map[string]any{
			"applicationId":            "app-1",
			"action":                   "build",
			"applicationEnvironmentId": "binding-1",
		},
	})
	if err != nil {
		t.Fatalf("InvokeTool returned error: %v", err)
	}
	if !delivery.triggered {
		t.Fatalf("expected delivery service trigger to be called")
	}
	if result.RelatedIDs["executionTaskId"] != "task-1" {
		t.Fatalf("expected related task id, got %#v", result.RelatedIDs)
	}
}

func TestInvokeToolDryRunOnlyPolicyDoesNotMutate(t *testing.T) {
	apps := &fakeApplicationService{}
	repo := &memoryGatewayRepository{
		accessPolicies: []domainaigateway.AccessPolicy{
			{
				ID:             "policy-dry-run",
				Enabled:        true,
				SubjectType:    "role",
				SubjectID:      "developer",
				Effect:         "allow",
				ToolPatterns:   []string{"delivery.applications.create"},
				ApprovalPolicy: map[string]any{"strategy": "dry_run_only"},
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsCreate,
			},
		},
	}), nil, repo)
	service.SetDeliveryServices(apps, &fakeDeliveryService{})

	result, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName: "delivery.applications.create",
		Input: map[string]any{
			"name": "Payments",
			"key":  "payments",
		},
	})
	if err != nil {
		t.Fatalf("InvokeTool returned error: %v", err)
	}
	if apps.created {
		t.Fatalf("application service create should not be called for dry-run-only policy")
	}
	if result.Result != "dry_run" || result.RelatedIDs["dryRunId"] == "" {
		t.Fatalf("expected dry-run result, got %#v", result)
	}
	if len(repo.auditLogs) != 1 || repo.auditLogs[0].Result != "dry_run" {
		t.Fatalf("expected dry-run audit log, got %#v", repo.auditLogs)
	}
}

func TestInvokeToolHumanConfirmPolicyDoesNotMutate(t *testing.T) {
	apps := &fakeApplicationService{}
	repo := &memoryGatewayRepository{
		accessPolicies: []domainaigateway.AccessPolicy{
			{
				ID:             "policy-confirm",
				Enabled:        true,
				SubjectType:    "role",
				SubjectID:      "developer",
				Effect:         "allow",
				ToolPatterns:   []string{"delivery.applications.create"},
				ApprovalPolicy: map[string]any{"strategy": "require_human_confirm"},
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsCreate,
			},
		},
	}), nil, repo)
	service.SetDeliveryServices(apps, &fakeDeliveryService{})

	result, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName: "delivery.applications.create",
		Input:    map[string]any{"name": "Payments", "key": "payments"},
	})
	if err != nil {
		t.Fatalf("InvokeTool returned error: %v", err)
	}
	if apps.created {
		t.Fatalf("application service create should not be called before human confirmation")
	}
	if result.Result != "pending_human_confirm" || result.RelatedIDs["confirmationRequestId"] == "" {
		t.Fatalf("expected pending human confirmation result, got %#v", result)
	}
}

func TestInvokeToolDenyStrategyRejects(t *testing.T) {
	apps := &fakeApplicationService{}
	repo := &memoryGatewayRepository{
		accessPolicies: []domainaigateway.AccessPolicy{
			{
				ID:             "policy-deny-strategy",
				Enabled:        true,
				SubjectType:    "role",
				SubjectID:      "developer",
				Effect:         "allow",
				ToolPatterns:   []string{"delivery.applications.create"},
				ApprovalPolicy: map[string]any{"strategy": "deny"},
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsCreate,
			},
		},
	}), nil, repo)
	service.SetDeliveryServices(apps, &fakeDeliveryService{})

	_, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName: "delivery.applications.create",
		Input:    map[string]any{"name": "Payments", "key": "payments"},
	})
	if err == nil {
		t.Fatalf("expected deny strategy to reject invocation")
	}
	if apps.created {
		t.Fatalf("application service create should not be called after deny strategy")
	}
}

func TestInvokeKubernetesPodLogsRoutesThroughResourceServiceAndRedacts(t *testing.T) {
	resources := &fakeResourceService{}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"sre": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermWorkspaceResourceView,
				appaccess.PermPlatformWorkloadsView,
			},
		},
	}), nil)
	service.SetResourceService(resources)

	result, err := service.InvokeTool(context.Background(), testPrincipal("sre"), domainaigateway.ToolInvocationRequest{
		ToolName: "k8s.pods.logs",
		Input: map[string]any{
			"clusterId": "cluster-a",
			"namespace": "prod",
			"podName":   "api-7d9f",
			"container": "api",
			"tailLines": 50,
		},
	})
	if err != nil {
		t.Fatalf("InvokeTool returned error: %v", err)
	}
	if !resources.readLogs || resources.clusterID != "cluster-a" || resources.namespace != "prod" || resources.podName != "api-7d9f" || resources.tailLines != 50 {
		t.Fatalf("expected resource service log call, got %#v", resources)
	}
	logs, ok := result.Output.(domainresource.PodLogsView)
	if !ok {
		t.Fatalf("expected pod logs output, got %#v", result.Output)
	}
	if logs.Content != "startup failed password=[REDACTED]" {
		t.Fatalf("expected redacted log content, got %q", logs.Content)
	}
	if result.RelatedIDs["podName"] != "api-7d9f" {
		t.Fatalf("expected related pod id, got %#v", result.RelatedIDs)
	}
}

func TestInvokeKubernetesP1DiagnosticsUseResourceService(t *testing.T) {
	resources := &fakeResourceService{
		pods: []domainresource.PodView{
			{Name: "api-7d9f", Namespace: "prod", Phase: "Running", Labels: map[string]string{"app": "api"}},
			{Name: "worker-6c4d", Namespace: "prod", Phase: "Running", Labels: map[string]string{"app": "worker"}},
		},
		clusterEvents: []domainresource.ClusterEventView{
			{Name: "event-1", Namespace: "prod", Type: "Warning", Reason: "ProgressDeadlineExceeded", InvolvedKind: "Deployment", InvolvedName: "api", Message: "deployment api exceeded progress deadline"},
			{Name: "event-2", Namespace: "prod", Type: "Normal", Reason: "Scheduled", InvolvedKind: "Pod", InvolvedName: "worker-6c4d", Message: "scheduled"},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"sre": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermWorkspaceResourceView,
				appaccess.PermPlatformWorkloadsView,
				appaccess.PermPlatformNetworkView,
				appaccess.PermPlatformStorageView,
				appaccess.PermPlatformNodesView,
				appaccess.PermObserveEventsView,
			},
		},
	}), nil)
	service.SetResourceService(resources)
	principal := testPrincipal("sre")

	assertKubernetesWorkloadDiagnostics(t, service, principal)
	assertKubernetesNetworkDiagnostics(t, service, principal)
	assertKubernetesInfrastructureDiagnostics(t, service, principal)
}

func TestInvokeKubernetesListRequiresResourceService(t *testing.T) {
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"sre": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermWorkspaceResourceView,
				appaccess.PermPlatformWorkloadsView,
			},
		},
	}), nil)

	_, err := service.InvokeTool(context.Background(), testPrincipal("sre"), domainaigateway.ToolInvocationRequest{
		ToolName: "k8s.pods.list",
		Input:    map[string]any{"clusterId": "cluster-a", "namespace": "prod"},
	})
	if err == nil {
		t.Fatalf("expected missing resource service to be rejected")
	}
}

func TestInvokeReleaseFailureDiagnosisCollectsDeliveryAndRuntimeEvidence(t *testing.T) {
	resources := &fakeResourceService{}
	delivery := &fakeDeliveryService{}
	recorder := &fakeAnalysisArtifactRecorder{}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"sre": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermObserveAIChatUse,
				appaccess.PermDeliveryExecutionTasksView,
			},
		},
	}), nil)
	service.SetDeliveryServices(&fakeApplicationService{}, delivery)
	service.SetResourceService(resources)
	service.SetAnalysisArtifactRecorder(recorder)

	result, err := service.InvokeTool(context.Background(), testPrincipal("sre"), domainaigateway.ToolInvocationRequest{
		ToolName: "diagnosis.release_failure.analyze",
		Input: map[string]any{
			"applicationId":            "app-1",
			"applicationEnvironmentId": "binding-1",
			"releaseBundleId":          "bundle-1",
			"executionTaskId":          "task-1",
			"clusterId":                "cluster-a",
			"namespace":                "prod",
			"workloadName":             "api",
			"podName":                  "api-7d9f",
			"eventLimit":               25,
		},
	})
	if err != nil {
		t.Fatalf("InvokeTool returned error: %v", err)
	}
	output := assertReleaseFailureEvidence(t, resources, result)
	assertReleaseFailureArtifact(t, recorder, result, output)
}

func TestInvokeReleaseFailureDiagnosisQueuesExternalAgentRuntime(t *testing.T) {
	resources := &fakeResourceService{}
	delivery := &fakeDeliveryService{}
	recorder := &fakeAnalysisArtifactRecorder{}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"sre": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermObserveAIChatUse,
				appaccess.PermDeliveryExecutionTasksView,
			},
		},
	}), nil)
	service.SetDeliveryServices(&fakeApplicationService{}, delivery)
	service.SetResourceService(resources)
	service.SetAnalysisArtifactRecorder(recorder)

	result, err := service.InvokeTool(context.Background(), testPrincipal("sre"), domainaigateway.ToolInvocationRequest{
		ToolName: "diagnosis.release_failure.analyze",
		Input: map[string]any{
			"applicationId":      "app-1",
			"executionTaskId":    "task-1",
			"clusterId":          "cluster-a",
			"namespace":          "prod",
			"workloadName":       "api",
			"deepAnalysis":       true,
			"timeoutSeconds":     900,
			"apiKeyShouldHide":   "provider-secret",
			"rawTokenShouldHide": "token-value",
		},
	})
	if err != nil {
		t.Fatalf("InvokeTool returned error: %v", err)
	}
	output := mustValueAs[map[string]any](t, result.Output)
	artifact := mustMapFieldAs[map[string]any](t, output, "analysisArtifact")
	if artifact["queued"] != true || artifact["artifactStored"] != false || artifact["agentRunId"] != "agent:queued-1" {
		t.Fatalf("expected queued external Agent Runtime metadata, got %#v", artifact)
	}
	if artifact["providerId"] != "hermes" || artifact["runtime"] != "agent_runtime_claim_callback" {
		t.Fatalf("expected external provider runtime metadata, got %#v", artifact)
	}
	if result.RelatedIDs["agentRunId"] != "agent:queued-1" || result.RelatedIDs["agentProviderId"] != "hermes" || result.RelatedIDs["analysisArtifactCount"] != nil {
		t.Fatalf("expected queued related ids without stored artifact count, got %#v", result.RelatedIDs)
	}
	if recorder.input.CapabilityID != "" {
		t.Fatalf("internal artifact recorder should not run for external analysis, got %#v", recorder.input)
	}
	if recorder.queuedInput.AgentProviderID != "" || recorder.queuedInput.CapabilityID != "delivery_failure" || recorder.queuedInput.TimeoutSeconds != 900 {
		t.Fatalf("expected external agent queue input, got %#v", recorder.queuedInput)
	}
	if recorder.queuedInput.DataSourceSnapshot["deepAnalysis"] != true {
		t.Fatalf("expected Gateway data source snapshot to carry external runtime request, got %#v", recorder.queuedInput.DataSourceSnapshot)
	}
	if fmt.Sprint(recorder.queuedInput.Input) == "" || strings.Contains(fmt.Sprint(recorder.queuedInput.Input), "provider-secret") || strings.Contains(fmt.Sprint(recorder.queuedInput.Input), "token-value") {
		t.Fatalf("queued agent input leaked sensitive values: %#v", recorder.queuedInput.Input)
	}
}

func TestListAuditLogsRequiresManageAndFilters(t *testing.T) {
	now := time.Now().UTC()
	repo := &memoryGatewayRepository{
		auditLogs: []domainaigateway.AuditLog{
			{
				ID:         "audit-1",
				ActorType:  "user",
				ActorID:    "user-1",
				AIClientID: "codex",
				SkillID:    "k8s-sre",
				ToolName:   "k8s.pods.logs",
				RiskLevel:  domainaigateway.RiskLevelRead,
				Action:     "ai_gateway.tool.invoke",
				Result:     "success",
				Summary:    "ok",
				CreatedAt:  now,
			},
			{
				ID:         "audit-2",
				ActorType:  "user",
				ActorID:    "user-2",
				AIClientID: "other",
				ToolName:   "delivery.actions.trigger",
				RiskLevel:  domainaigateway.RiskLevelExecute,
				Action:     "ai_gateway.tool.invoke",
				Result:     "deny",
				Summary:    "denied",
				CreatedAt:  now,
			},
		},
	}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"admin":     {appaccess.PermAIGatewayManage},
			"developer": {appaccess.PermAIGatewayView},
		},
	}), nil, repo)

	items, err := service.ListAuditLogs(context.Background(), testPrincipal("admin"), domainaigateway.AuditLogFilter{
		AIClientID: "codex",
		ToolName:   "k8s.pods.logs",
		Limit:      50,
	})
	if err != nil {
		t.Fatalf("ListAuditLogs returned error: %v", err)
	}
	if len(items) != 1 || items[0].ID != "audit-1" {
		t.Fatalf("expected filtered audit-1, got %#v", items)
	}

	if _, err := service.ListAuditLogs(context.Background(), testPrincipal("developer"), domainaigateway.AuditLogFilter{}); err == nil {
		t.Fatalf("expected ai.gateway.manage to be required")
	}
}

func testPrincipal(role string) domainidentity.Principal {
	return domainidentity.Principal{
		UserID:   "user-1",
		UserName: "User One",
		Roles:    []string{role},
	}
}

func fakeGitHubPATForTest() string {
	return "ghp_" + "abcdefghijklmnopqrstuvwxyz123456"
}

func fakeProviderKeyForTest(char string) string {
	return "sk-" + strings.Repeat(char, 32)
}

func fakeAWSAccessKeyIDForTest() string {
	return "AKIA" + "1234567890ABCDEF"
}

func governanceRecommendationHasServiceTokenRef(action domainaigateway.GovernanceRecommendationAction) bool {
	refs, ok := action.Metadata["tokenRefs"].([]map[string]any)
	if !ok {
		return false
	}
	return slices.ContainsFunc(refs, func(item map[string]any) bool {
		return item["kind"] == "service_account_token" && item["id"] == "sat-stale"
	})
}

func floatNear(value any, expected float64) bool {
	number, ok := gatewayPositiveFloat(value)
	if !ok {
		return false
	}
	diff := number - expected
	return diff < 0.000001 && diff > -0.000001
}

func hasTool(items []domainaigateway.ToolCapability, name string) bool {
	for _, item := range items {
		if item.Name == name {
			return true
		}
	}
	return false
}

func hasResource(items []domainaigateway.ResourceCapability, name string) bool {
	for _, item := range items {
		if item.Name == name {
			return true
		}
	}
	return false
}

func hasPrompt(items []domainaigateway.PromptCapability, name string) bool {
	for _, item := range items {
		if item.Name == name {
			return true
		}
	}
	return false
}

func anyStringSet(value any) map[string]bool {
	out := map[string]bool{}
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			out[fmt.Sprint(item)] = true
		}
	case []string:
		for _, item := range typed {
			out[item] = true
		}
	}
	return out
}

func hasSkill(items []domainaigateway.SkillCapability, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}
