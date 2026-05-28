package aigateway

import (
	"context"
	"slices"
	"testing"
	"time"

	appaccess "github.com/soha/soha/internal/application/access"
	domainaigateway "github.com/soha/soha/internal/domain/aigateway"
	domainapp "github.com/soha/soha/internal/domain/application"
	domainaudit "github.com/soha/soha/internal/domain/audit"
	domaindelivery "github.com/soha/soha/internal/domain/delivery"
	domainidentity "github.com/soha/soha/internal/domain/identity"
	domainresource "github.com/soha/soha/internal/domain/resource"
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

type memoryGatewayRepository struct {
	personalTokens       []domainaigateway.PersonalAccessToken
	serviceAccounts      map[string]domainaigateway.ServiceAccount
	serviceAccountTokens []domainaigateway.ServiceAccountToken
	aiClients            map[string]domainaigateway.AIClient
	toolGrants           []domainaigateway.ToolGrant
	accessPolicies       []domainaigateway.AccessPolicy
	skillBindings        []domainaigateway.SkillBinding
	auditLogs            []domainaigateway.AuditLog
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

func (r *memoryGatewayRepository) CreatePersonalAccessToken(_ context.Context, item domainaigateway.PersonalAccessToken) (domainaigateway.PersonalAccessToken, error) {
	r.personalTokens = append(r.personalTokens, item)
	return item, nil
}

func (r *memoryGatewayRepository) RevokePersonalAccessToken(_ context.Context, userID, tokenID string) error {
	for index := range r.personalTokens {
		if r.personalTokens[index].UserID == userID && r.personalTokens[index].ID == tokenID {
			return nil
		}
	}
	return nil
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

func (r *memoryGatewayRepository) RevokeServiceAccountToken(context.Context, string) error {
	return nil
}

func (r *memoryGatewayRepository) ListAIClients(context.Context) ([]domainaigateway.AIClient, error) {
	items := make([]domainaigateway.AIClient, 0, len(r.aiClients))
	for _, item := range r.aiClients {
		items = append(items, item)
	}
	return items, nil
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
	listed  bool
	created bool
}

func (s *fakeApplicationService) List(_ context.Context, _ domainidentity.Principal, filter domainapp.Filter) ([]domainapp.App, error) {
	s.listed = true
	return []domainapp.App{{ID: "app-1", Name: "Billing", Key: "billing", BusinessLineID: "core"}}, nil
}

func (s *fakeApplicationService) Create(_ context.Context, _ domainidentity.Principal, input domainapp.UpsertInput) (domainapp.App, error) {
	s.created = true
	return domainapp.App{ID: firstNonEmpty(input.ID, "app-created"), Name: input.Name, Key: input.Key}, nil
}

type fakeDeliveryService struct {
	triggered bool
}

func (s *fakeDeliveryService) GetApplicationDetail(context.Context, domainidentity.Principal, string) (domaindelivery.ApplicationDetail, error) {
	return domaindelivery.ApplicationDetail{Bindings: []domaindelivery.ApplicationBindingSummary{{ApplicationEnvironmentID: "binding-1"}}}, nil
}

func (s *fakeDeliveryService) GetApplicationEnvironmentDetail(context.Context, domainidentity.Principal, string) (domaindelivery.ApplicationEnvironmentDetail, error) {
	return domaindelivery.ApplicationEnvironmentDetail{}, nil
}

func (s *fakeDeliveryService) TriggerApplicationDeliveryAction(_ context.Context, _ domainidentity.Principal, applicationID string, input domaindelivery.ApplicationDeliveryActionInput) (domaindelivery.ApplicationDeliveryActionResult, error) {
	s.triggered = true
	return domaindelivery.ApplicationDeliveryActionResult{
		Action:                   input.Action,
		ApplicationID:            applicationID,
		ApplicationEnvironmentID: input.ApplicationEnvironmentID,
		RelatedIDs: domaindelivery.ApplicationDeliveryActionRelatedIDs{
			ReleaseBundleID: "bundle-1",
			ExecutionTaskID: "task-1",
		},
	}, nil
}

func (s *fakeDeliveryService) ListReleaseBundles(context.Context, domainidentity.Principal, domaindelivery.ReleaseBundleFilter) ([]domaindelivery.ReleaseBundle, error) {
	return []domaindelivery.ReleaseBundle{{ID: "bundle-1"}}, nil
}

func (s *fakeDeliveryService) ListReleaseBundleArtifacts(context.Context, domainidentity.Principal, string) ([]domaindelivery.ExecutionArtifact, error) {
	return []domaindelivery.ExecutionArtifact{{ID: "artifact-1"}}, nil
}

func (s *fakeDeliveryService) ListExecutionTasks(context.Context, domainidentity.Principal, domaindelivery.ExecutionTaskFilter) ([]domaindelivery.ExecutionTask, error) {
	return []domaindelivery.ExecutionTask{{ID: "task-1"}}, nil
}

func (s *fakeDeliveryService) ListExecutionLogs(context.Context, domainidentity.Principal, string, int) ([]domaindelivery.ExecutionLog, error) {
	return []domaindelivery.ExecutionLog{{ID: "log-1", Message: "build failed token=secret-token", Metadata: map[string]any{"password": "secret"}}}, nil
}

type fakeResourceService struct {
	listedPods    bool
	readLogs      bool
	listedEvents  bool
	tailLines     int64
	logLimit      int
	eventLimit    int
	clusterID     string
	namespace     string
	podName       string
	deployments   []domainresource.DeploymentView
	services      []domainresource.ServiceView
	pods          []domainresource.PodView
	clusterEvents []domainresource.ClusterEventView
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

func (s *fakeResourceService) ListServices(_ context.Context, _ domainidentity.Principal, clusterID, namespace string) ([]domainresource.ServiceView, error) {
	s.clusterID = clusterID
	s.namespace = namespace
	if s.services != nil {
		return s.services, nil
	}
	return []domainresource.ServiceView{{Name: "api", Namespace: namespace, Type: "ClusterIP"}}, nil
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
	service := New(appaccess.NewPermissionResolver(stubRolePermissionReader{
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
	service := New(appaccess.NewPermissionResolver(stubRolePermissionReader{
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

func TestCapabilitiesExposeExecuteToolWithTriggerPermissions(t *testing.T) {
	service := New(appaccess.NewPermissionResolver(stubRolePermissionReader{
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
	service := New(appaccess.NewPermissionResolver(stubRolePermissionReader{
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
	service := New(appaccess.NewPermissionResolver(stubRolePermissionReader{
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
	service := New(appaccess.NewPermissionResolver(stubRolePermissionReader{
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
	service := New(appaccess.NewPermissionResolver(stubRolePermissionReader{
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
	service := New(appaccess.NewPermissionResolver(stubRolePermissionReader{
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
	service := New(appaccess.NewPermissionResolver(stubRolePermissionReader{
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
}

func TestCreatePersonalAccessTokenCapsPermissionsToRequest(t *testing.T) {
	repo := &memoryGatewayRepository{}
	service := New(appaccess.NewPermissionResolver(stubRolePermissionReader{
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
	service := New(appaccess.NewPermissionResolver(stubRolePermissionReader{
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
	service := New(appaccess.NewPermissionResolver(stubRolePermissionReader{
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

func TestCreateAIClientRequiresManageAndPersistsClient(t *testing.T) {
	repo := &memoryGatewayRepository{}
	service := New(appaccess.NewPermissionResolver(stubRolePermissionReader{
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

func TestCreateToolGrantDefaultsRiskFromKnownTool(t *testing.T) {
	repo := &memoryGatewayRepository{}
	service := New(appaccess.NewPermissionResolver(stubRolePermissionReader{
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

func TestCreateAccessPolicyAndSkillBindingRequireManage(t *testing.T) {
	repo := &memoryGatewayRepository{}
	service := New(appaccess.NewPermissionResolver(stubRolePermissionReader{
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

func TestInvokeToolRoutesDeliveryListThroughApplicationService(t *testing.T) {
	audit := &captureAuditRecorder{}
	apps := &fakeApplicationService{}
	repo := &memoryGatewayRepository{}
	service := New(appaccess.NewPermissionResolver(stubRolePermissionReader{
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

func TestInvokeToolRejectsMissingBusinessPermission(t *testing.T) {
	audit := &captureAuditRecorder{}
	apps := &fakeApplicationService{}
	service := New(appaccess.NewPermissionResolver(stubRolePermissionReader{
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
	service := New(appaccess.NewPermissionResolver(stubRolePermissionReader{
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
	service := New(appaccess.NewPermissionResolver(stubRolePermissionReader{
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
	service := New(appaccess.NewPermissionResolver(stubRolePermissionReader{
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

func TestInvokeToolCanTriggerDeliveryActionThroughDeliveryService(t *testing.T) {
	delivery := &fakeDeliveryService{}
	service := New(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryBuildsTrigger,
				appaccess.PermDeliveryReleasesTrigger,
			},
		},
	}), nil)
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

func TestInvokeKubernetesPodLogsRoutesThroughResourceServiceAndRedacts(t *testing.T) {
	resources := &fakeResourceService{}
	service := New(appaccess.NewPermissionResolver(stubRolePermissionReader{
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

func TestInvokeKubernetesListRequiresResourceService(t *testing.T) {
	service := New(appaccess.NewPermissionResolver(stubRolePermissionReader{
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
	service := New(appaccess.NewPermissionResolver(stubRolePermissionReader{
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
	if !resources.listedPods || !resources.listedEvents || !resources.readLogs || resources.eventLimit != 25 {
		t.Fatalf("expected runtime evidence collection, got %#v", resources)
	}
	output, ok := result.Output.(map[string]any)
	if !ok {
		t.Fatalf("expected diagnosis context map, got %#v", result.Output)
	}
	deliveryEvidence := output["delivery"].(map[string]any)
	logs := deliveryEvidence["executionLogs"].([]domaindelivery.ExecutionLog)
	if logs[0].Message != "build failed token=[REDACTED]" || logs[0].Metadata["password"] != "[REDACTED]" {
		t.Fatalf("expected redacted delivery logs, got %#v", logs[0])
	}
	runtimeEvidence := output["runtime"].(map[string]any)
	podLogs := runtimeEvidence["podLogs"].(domainresource.PodLogsView)
	if podLogs.Content != "startup failed password=[REDACTED]" {
		t.Fatalf("expected redacted pod logs, got %#v", podLogs)
	}
	if result.RelatedIDs["executionTaskId"] != "task-1" || result.RelatedIDs["clusterId"] != "cluster-a" {
		t.Fatalf("expected related ids, got %#v", result.RelatedIDs)
	}
}

func testPrincipal(role string) domainidentity.Principal {
	return domainidentity.Principal{
		UserID:   "user-1",
		UserName: "User One",
		Roles:    []string{role},
	}
}

func hasTool(items []domainaigateway.ToolCapability, name string) bool {
	for _, item := range items {
		if item.Name == name {
			return true
		}
	}
	return false
}

func hasSkill(items []domainaigateway.SkillCapability, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}
