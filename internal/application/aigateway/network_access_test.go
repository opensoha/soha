package aigateway

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	appnetworkaccess "github.com/opensoha/soha/internal/application/networkaccess"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainnetworkaccess "github.com/opensoha/soha/internal/domain/networkaccess"
	domainnetworkingest "github.com/opensoha/soha/internal/domain/networkingest"
	domainnetworkruntime "github.com/opensoha/soha/internal/domain/networkruntime"
)

type fakeNetworkAccessPreviewService struct {
	input          appnetworkaccess.PreviewInput
	conflictRanges []domainnetworkaccess.ConflictRange
	sessionFilter  domainnetworkaccess.SessionFilter
	actionSession  string
	actionInput    domainnetworkaccess.SessionActionInput
	gatewayID      string
	gatewayInput   domainnetworkaccess.GatewayInput
}

type fakeNetworkEnrollmentService struct {
	created appnetworkaccess.EnrollmentInput
	revoked string
	item    domainnetworkruntime.EnrollmentChallenge
}

type fakeNetworkAccessGrantService struct {
	sessionID string
	created   appnetworkaccess.AccessGrantInput
	revoked   string
	item      domainnetworkruntime.AccessGrant
}

type fakeNetworkTelemetryService struct {
	filter  domainnetworkingest.SummaryFilter
	summary domainnetworkingest.Summary
}

func (s *fakeNetworkTelemetryService) Summary(_ context.Context, filter domainnetworkingest.SummaryFilter) (domainnetworkingest.Summary, error) {
	s.filter = filter
	return s.summary, nil
}

func (s *fakeNetworkAccessGrantService) Create(_ context.Context, _ domainidentity.Principal, sessionID string, input appnetworkaccess.AccessGrantInput) (domainnetworkruntime.AccessGrantSecret, error) {
	s.sessionID, s.created = sessionID, input
	return domainnetworkruntime.AccessGrantSecret{Grant: s.item, Token: "0123456789abcdef0123456789abcdef"}, nil
}

func (s *fakeNetworkAccessGrantService) List(context.Context, domainidentity.Principal, domainnetworkruntime.AccessGrantFilter) ([]domainnetworkruntime.AccessGrant, error) {
	return []domainnetworkruntime.AccessGrant{s.item}, nil
}

func (s *fakeNetworkAccessGrantService) Revoke(_ context.Context, _ domainidentity.Principal, id string) (domainnetworkruntime.AccessGrant, error) {
	s.revoked = id
	item := s.item
	item.Status = domainnetworkruntime.AccessGrantRevoked
	return item, nil
}

func (s *fakeNetworkEnrollmentService) Create(_ context.Context, _ domainidentity.Principal, input appnetworkaccess.EnrollmentInput) (domainnetworkruntime.EnrollmentSecret, error) {
	s.created = input
	return domainnetworkruntime.EnrollmentSecret{EnrollmentChallenge: s.item, Token: "0123456789abcdef0123456789abcdef"}, nil
}

func (s *fakeNetworkEnrollmentService) List(context.Context, domainidentity.Principal, int) ([]domainnetworkruntime.EnrollmentChallenge, error) {
	return []domainnetworkruntime.EnrollmentChallenge{s.item}, nil
}

func (s *fakeNetworkEnrollmentService) Get(context.Context, domainidentity.Principal, string) (domainnetworkruntime.EnrollmentChallenge, error) {
	return s.item, nil
}

func (s *fakeNetworkEnrollmentService) Revoke(_ context.Context, _ domainidentity.Principal, id string) (domainnetworkruntime.EnrollmentChallenge, error) {
	s.revoked = id
	item := s.item
	item.Status = domainnetworkruntime.EnrollmentRevoked
	return item, nil
}

func (s *fakeNetworkAccessPreviewService) PreviewPolicy(_ context.Context, _ domainidentity.Principal, input appnetworkaccess.PreviewInput) (domainnetworkaccess.PolicyPreview, error) {
	s.input = input
	return domainnetworkaccess.PolicyPreview{Decision: domainnetworkaccess.DecisionAllow, Path: domainnetworkaccess.PathWireGuardZTNA, NetworkProfile: "vpn_ztna", PolicyVersion: 7, Protected: true, NetworkLeaseRequired: true, ResourceLeaseRequired: true, Reasons: []string{"team_policy_allow"}}, nil
}

func (s *fakeNetworkAccessPreviewService) AnalyzeConflicts(_ context.Context, _ domainidentity.Principal, ranges []domainnetworkaccess.ConflictRange) (domainnetworkaccess.ConflictAnalysis, error) {
	s.conflictRanges = ranges
	return domainnetworkaccess.ConflictAnalysis{Valid: false, RangesAnalyzed: 3, Conflicts: []domainnetworkaccess.Conflict{{Left: ranges[0], Right: domainnetworkaccess.ConflictRange{SourceType: domainnetworkaccess.ConflictSourceNetworkSpace, SourceID: "space-1", Name: "corp", CIDR: "10.42.0.0/24"}, Reason: domainnetworkaccess.ConflictOverlap}}, Warnings: []string{}}, nil
}

func (s *fakeNetworkAccessPreviewService) ListSessions(_ context.Context, _ domainidentity.Principal, filter domainnetworkaccess.SessionFilter) ([]domainnetworkaccess.Session, error) {
	s.sessionFilter = filter
	return []domainnetworkaccess.Session{{ID: "session-1", RuntimeID: "runtime-1", SubjectID: "user-1", DeviceID: "device-1", NASID: "nas-1", Mode: domainnetworkaccess.ModeInternalDirect, Path: domainnetworkaccess.PathSiteDirect, AccessProfile: domainnetworkaccess.ProfileFull, Status: "active", PolicyVersion: 7}}, nil
}

func (s *fakeNetworkAccessPreviewService) PlanSessionAction(_ context.Context, _ domainidentity.Principal, sessionID string, input domainnetworkaccess.SessionActionInput) (domainnetworkaccess.SessionActionPlan, error) {
	s.actionSession, s.actionInput = sessionID, input
	return domainnetworkaccess.SessionActionPlan{SessionID: sessionID, RuntimeID: "runtime-1", NASID: "nas-1", RequestedAction: input.Action, EffectiveAction: input.Action, CurrentAccessProfile: domainnetworkaccess.ProfileFull, TargetAccessProfile: input.TargetAccessProfile, ReasonCode: input.ReasonCode, CommandExpiresAt: time.Now().Add(time.Minute), PlanHash: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, nil
}

func (s *fakeNetworkAccessPreviewService) ExecuteSessionAction(_ context.Context, _ domainidentity.Principal, sessionID string, input domainnetworkaccess.SessionActionInput) (domainnetworkaccess.SessionCommand, error) {
	s.actionSession, s.actionInput = sessionID, input
	return domainnetworkaccess.SessionCommand{ID: "command-1", SessionID: sessionID, RuntimeID: "runtime-1", NASID: "nas-1", Action: input.Action, TargetAccessProfile: input.TargetAccessProfile, PolicyVersion: 7, Status: domainnetworkaccess.SessionCommandPending, ReasonCode: input.ReasonCode, PlanHash: input.PlanHash, EffectiveAt: time.Now(), ExpiresAt: time.Now().Add(time.Minute), CreatedAt: time.Now()}, nil
}

func (s *fakeNetworkAccessPreviewService) CreateGateway(_ context.Context, _ domainidentity.Principal, input domainnetworkaccess.GatewayInput) (domainnetworkaccess.Gateway, error) {
	s.gatewayInput = input
	return domainnetworkaccess.Gateway{ID: "gateway-1", RuntimeID: input.RuntimeID, SiteID: input.SiteID, Name: input.Name, AdministrativeStatus: input.AdministrativeStatus, Status: domainnetworkaccess.GatewayOffline, PublicEndpointHost: input.PublicEndpointHost, PublicEndpointPort: input.PublicEndpointPort, OverlayCIDR: input.OverlayCIDR, RoutingMode: input.RoutingMode, MTU: input.MTU, PersistentKeepaliveSeconds: input.PersistentKeepaliveSeconds, DNSServers: input.DNSServers, Capabilities: []string{}, CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil
}

func (s *fakeNetworkAccessPreviewService) UpdateGateway(_ context.Context, _ domainidentity.Principal, id string, input domainnetworkaccess.GatewayInput) (domainnetworkaccess.Gateway, error) {
	s.gatewayID, s.gatewayInput = id, input
	result, err := s.CreateGateway(context.Background(), domainidentity.Principal{}, input)
	result.ID = id
	return result, err
}

func (s *fakeNetworkAccessPreviewService) ListMihomoProfiles(context.Context, domainidentity.Principal, domainnetworkaccess.MihomoProfileFilter) ([]domainnetworkaccess.MihomoProfile, error) {
	return []domainnetworkaccess.MihomoProfile{{ID: "mihomo-1", DeviceID: "device-1", Name: "managed", Mode: domainnetworkaccess.MihomoModeManagedFollow, Status: "active", SubscriptionConfigured: true, SubscriptionURLCiphertext: "do-not-return", Revision: 1, MixedPort: 7890, ControllerPort: 9090, DNSMode: domainnetworkaccess.MihomoDNSFakeIP, SelectorGroup: "PROXY", BypassCIDRs: []string{}, BypassHosts: []string{}, FailClosed: true}}, nil
}

func TestNetworkMihomoListAndTelemetryToolsExposeOnlySafeAggregates(t *testing.T) {
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{}), nil, &memoryGatewayRepository{})
	networkAccess := &fakeNetworkAccessPreviewService{}
	from := time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC)
	telemetry := &fakeNetworkTelemetryService{summary: domainnetworkingest.Summary{From: from, To: from.Add(time.Hour), EventCount: 1, ProxyFlowCount: 1, Producers: []domainnetworkingest.ProducerSummary{}, ProxyFlows: []domainnetworkingest.ProxyFlowSummary{}}}
	service.SetNetworkAccessPreviewService(networkAccess)
	service.SetNetworkTelemetryService(telemetry)

	for _, name := range []string{"network_access.mihomo_profiles.list", "network_access.telemetry.summary"} {
		tool, ok := service.toolByName(name)
		if !ok || tool.RiskLevel != domainaigateway.RiskLevelRead || tool.RequiresApproval {
			t.Fatalf("tool %s = %#v, present=%v", name, tool, ok)
		}
	}
	mihomo, _, err := service.invokeNetworkAccessTool(context.Background(), domainidentity.Principal{}, "", "network_access.mihomo_profiles.list", map[string]any{"deviceId": "device-1", "limit": 10})
	raw, marshalErr := json.Marshal(mihomo)
	if err != nil || marshalErr != nil || strings.Contains(string(raw), "do-not-return") || strings.Contains(string(raw), "subscriptionURL") {
		t.Fatalf("unsafe mihomo output=%s err=%v marshal=%v", raw, err, marshalErr)
	}
	result, related, err := service.invokeNetworkAccessTool(context.Background(), domainidentity.Principal{}, "", "network_access.telemetry.summary", map[string]any{"from": from.Format(time.RFC3339), "to": from.Add(time.Hour).Format(time.RFC3339), "producerId": "endpoint-1", "limit": 10})
	if err != nil || telemetry.filter.ProducerID != "endpoint-1" || telemetry.filter.Limit != 10 || related["eventCount"] != int64(1) || networkToolResult[domainnetworkingest.Summary](t, result).ProxyFlowCount != 1 {
		t.Fatalf("telemetry output=%#v related=%#v filter=%#v err=%v", result, related, telemetry.filter, err)
	}
}

func TestNetworkAccessPolicyPreviewToolUsesSharedPolicyService(t *testing.T) {
	repo := &memoryGatewayRepository{accessPolicies: []domainaigateway.AccessPolicy{{
		ID: "allow-network-preview", Enabled: true, SubjectType: "role", SubjectID: "security-admin", Effect: "allow",
		ToolPatterns: []string{"network_access.policy.preview"}, RiskLevels: []domainaigateway.RiskLevel{domainaigateway.RiskLevelAnalyze},
		ApprovalPolicy: map[string]any{"strategy": "allow"},
	}}}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{matrix: map[string][]string{
		"security-admin": {appaccess.PermAIGatewayView, appaccess.PermAIGatewayInvoke, appaccess.PermNetworkAccessPolicyView},
	}}), nil, repo)
	preview := &fakeNetworkAccessPreviewService{}
	service.SetNetworkAccessPreviewService(preview)
	principal := testPrincipal("security-admin")

	manifest, err := service.Capabilities(context.Background(), principal, domainaigateway.ManifestRequest{})
	if err != nil || !hasTool(manifest.Tools, "network_access.policy.preview") {
		t.Fatalf("network preview capability missing: summary=%#v err=%v", manifest.Summary, err)
	}
	result, err := service.InvokeTool(context.Background(), principal, domainaigateway.ToolInvocationRequest{ToolName: "network_access.policy.preview", Input: map[string]any{
		"subjectUserId": "user-1", "deviceId": "device-1", "resourceId": "resource-1", "siteId": "site-1", "mode": "external_vpn_ztna",
	}})
	if err != nil || result.Result != "success" || result.RelatedIDs["deviceId"] != "device-1" || result.RelatedIDs["resourceId"] != "resource-1" {
		t.Fatalf("network preview result=%#v err=%v", result, err)
	}
	if preview.input.Mode != domainnetworkaccess.ModeExternalVPNZTNA || preview.input.SubjectUserID != "user-1" {
		t.Fatalf("network preview input=%#v", preview.input)
	}
}

func TestNetworkAccessConflictToolUsesSharedPolicyService(t *testing.T) {
	repo := &memoryGatewayRepository{accessPolicies: []domainaigateway.AccessPolicy{{
		ID: "allow-network-conflicts", Enabled: true, SubjectType: "role", SubjectID: "security-admin", Effect: "allow",
		ToolPatterns: []string{"network_access.conflicts.analyze"}, RiskLevels: []domainaigateway.RiskLevel{domainaigateway.RiskLevelAnalyze},
		ApprovalPolicy: map[string]any{"strategy": "allow"},
	}}}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{matrix: map[string][]string{
		"security-admin": {appaccess.PermAIGatewayView, appaccess.PermAIGatewayInvoke, appaccess.PermNetworkAccessPolicyView},
	}}), nil, repo)
	networkAccess := &fakeNetworkAccessPreviewService{}
	service.SetNetworkAccessPreviewService(networkAccess)
	principal := testPrincipal("security-admin")

	manifest, err := service.Capabilities(context.Background(), principal, domainaigateway.ManifestRequest{})
	if err != nil || !hasTool(manifest.Tools, "network_access.conflicts.analyze") {
		t.Fatalf("network conflict capability missing: summary=%#v err=%v", manifest.Summary, err)
	}
	result, err := service.InvokeTool(context.Background(), principal, domainaigateway.ToolInvocationRequest{ToolName: "network_access.conflicts.analyze", Input: map[string]any{
		"runtimeRanges": []any{map[string]any{"sourceType": "wireguard_overlay", "sourceId": "gateway-1", "name": "overlay", "cidr": "10.42.0.0/16"}},
	}})
	if err != nil || result.Result != "success" || result.RelatedIDs["conflictCount"] != 1 || result.RelatedIDs["rangesAnalyzed"] != 3 {
		t.Fatalf("network conflict result=%#v err=%v", result, err)
	}
	if len(networkAccess.conflictRanges) != 1 || networkAccess.conflictRanges[0].SourceType != domainnetworkaccess.ConflictSourceWireGuardOverlay {
		t.Fatalf("network conflict input=%#v", networkAccess.conflictRanges)
	}
}

func TestNetworkGatewayToolsUseSharedServiceAndRequireApproval(t *testing.T) {
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{}), nil, &memoryGatewayRepository{})
	networkAccess := &fakeNetworkAccessPreviewService{}
	service.SetNetworkAccessPreviewService(networkAccess)
	input := map[string]any{
		"runtimeId": "gateway-hq", "siteId": "site-hq", "name": "HQ gateway", "administrativeStatus": "active",
		"publicEndpointHost": "vpn.example.com", "publicEndpointPort": 51820, "overlayCidr": "100.96.0.0/24",
		"routingMode": "routed", "mtu": 1420, "persistentKeepaliveSeconds": 25, "dnsServers": []any{"10.0.0.53"},
	}
	for _, name := range []string{"network_access.gateways.create", "network_access.gateways.update"} {
		tool, ok := service.toolByName(name)
		if !ok || tool.RiskLevel != domainaigateway.RiskLevelHigh || !tool.RequiresApproval {
			t.Fatalf("tool %s = %#v, present=%v", name, tool, ok)
		}
	}
	output, related, err := service.invokeNetworkAccessTool(context.Background(), domainidentity.Principal{}, "", "network_access.gateways.create", input)
	if err != nil || networkToolResult[domainnetworkaccess.Gateway](t, output).ID != "gateway-1" || related["runtimeId"] != "gateway-hq" || networkAccess.gatewayInput.OverlayCIDR != "100.96.0.0/24" {
		t.Fatalf("create gateway output=%#v related=%#v input=%#v err=%v", output, related, networkAccess.gatewayInput, err)
	}
	input["gatewayId"] = "gateway-1"
	_, related, err = service.invokeNetworkAccessTool(context.Background(), domainidentity.Principal{}, "", "network_access.gateways.update", input)
	if err != nil || networkAccess.gatewayID != "gateway-1" || related["gatewayId"] != "gateway-1" {
		t.Fatalf("update gateway related=%#v id=%q err=%v", related, networkAccess.gatewayID, err)
	}
}

func TestNetworkEnrollmentToolsUseSharedServiceAndNeverExposeDigest(t *testing.T) {
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{}), nil, &memoryGatewayRepository{})
	now := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	enrollments := &fakeNetworkEnrollmentService{item: domainnetworkruntime.EnrollmentChallenge{
		ID: "enrollment-1", ChallengeID: "challenge-1", ChallengeHash: "sha256:do-not-return",
		RuntimeID: "endpoint-1", RuntimeKind: "endpoint", DeviceID: "device-1", SubjectID: "user-1",
		Status: domainnetworkruntime.EnrollmentPending, ExpiresAt: now.Add(10 * time.Minute), CreatedBy: "operator-1", CreatedAt: now,
	}}
	service.SetNetworkEnrollmentService(enrollments)

	for _, name := range []string{"network_access.enrollments.list", "network_access.enrollments.create", "network_access.enrollments.revoke"} {
		tool, ok := service.toolByName(name)
		if !ok {
			t.Fatalf("missing tool %s", name)
		}
		if name != "network_access.enrollments.list" && (tool.RiskLevel != domainaigateway.RiskLevelHigh || !tool.RequiresApproval) {
			t.Fatalf("tool %s risk=%s approval=%v", name, tool.RiskLevel, tool.RequiresApproval)
		}
	}

	output, related, err := service.invokeNetworkAccessTool(context.Background(), domainidentity.Principal{}, "", "network_access.enrollments.create", map[string]any{
		"runtimeId": "endpoint-1", "runtimeKind": "endpoint", "deviceId": "device-1", "subjectId": "user-1", "ttlSeconds": 300,
	})
	if err != nil || enrollments.created.TTL != 5*time.Minute || related["enrollmentId"] != "enrollment-1" {
		t.Fatalf("create output=%#v related=%#v input=%#v err=%v", output, related, enrollments.created, err)
	}
	scope := standardGatewayScope(map[string]any{"runtimeId": "endpoint-1", "deviceId": "device-1", "subjectId": "user-1"}, nil)
	if scope["runtimeId"] != "endpoint-1" || scope["deviceId"] != "device-1" || scope["subjectUserId"] != "user-1" {
		t.Fatalf("network invocation scope = %#v", scope)
	}
	created := networkToolResult[map[string]any](t, output)
	if created["token"] == "" || created["challengeHash"] != nil || networkToolResult[map[string]any](t, created["enrollment"])["challengeHash"] != nil {
		t.Fatalf("unsafe enrollment create output: %#v", created)
	}

	output, _, err = service.invokeNetworkAccessTool(context.Background(), domainidentity.Principal{}, "", "network_access.enrollments.list", map[string]any{"limit": 10})
	if err != nil || networkToolResult[[]map[string]any](t, networkToolResult[map[string]any](t, output)["items"])[0]["challengeHash"] != nil {
		t.Fatalf("unsafe enrollment list output: %#v err=%v", output, err)
	}
	output, _, err = service.invokeNetworkAccessTool(context.Background(), domainidentity.Principal{}, "", "network_access.enrollments.revoke", map[string]any{"enrollmentId": "enrollment-1"})
	if err != nil || enrollments.revoked != "enrollment-1" || networkToolResult[map[string]any](t, output)["status"] != domainnetworkruntime.EnrollmentRevoked {
		t.Fatalf("revoke output=%#v revoked=%q err=%v", output, enrollments.revoked, err)
	}
}

func TestNetworkAccessGrantToolsBindSessionAndNeverExposeDigest(t *testing.T) {
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{}), nil, &memoryGatewayRepository{})
	now := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	grants := &fakeNetworkAccessGrantService{item: domainnetworkruntime.AccessGrant{
		ID: "grant-1", SubjectID: "user-1", AuthSessionID: "do-not-return", DeviceID: "device-1", SiteID: "site-1",
		NetworkSpaceID: "space-1", Mode: domainnetworkaccess.ModeExternalDirectZTNA, ResourceIDs: []string{"resource-1"},
		PolicyVersion: 7, Status: domainnetworkruntime.AccessGrantIssued, TokenHash: "sha256:do-not-return",
		ExpiresAt: now.Add(5 * time.Minute), CreatedBy: "operator-1", CreatedAt: now,
	}}
	service.SetNetworkAccessGrantService(grants)

	for _, name := range []string{"network_access.access_grants.list", "network_access.access_grants.create", "network_access.access_grants.revoke"} {
		tool, ok := service.toolByName(name)
		if !ok || (name != "network_access.access_grants.list" && (tool.RiskLevel != domainaigateway.RiskLevelHigh || !tool.RequiresApproval)) {
			t.Fatalf("tool %s = %#v, present=%v", name, tool, ok)
		}
	}

	output, related, err := service.invokeNetworkAccessTool(context.Background(), domainidentity.Principal{}, "auth-session-1", "network_access.access_grants.create", map[string]any{
		"deviceId": "device-1", "siteId": "site-1", "networkSpaceId": "space-1", "mode": "external_direct_ztna",
		"resourceIds": []any{"resource-1"}, "ttlSeconds": 300,
	})
	if err != nil || grants.sessionID != "auth-session-1" || grants.created.TTL != 5*time.Minute || related["grantId"] != "grant-1" {
		t.Fatalf("create output=%#v related=%#v session=%q input=%#v err=%v", output, related, grants.sessionID, grants.created, err)
	}
	created := networkToolResult[map[string]any](t, output)
	grant := networkToolResult[map[string]any](t, created["grant"])
	if created["token"] == "" || grant["tokenHash"] != nil || grant["authSessionId"] != nil {
		t.Fatalf("unsafe grant create output: %#v", created)
	}

	output, _, err = service.invokeNetworkAccessTool(context.Background(), domainidentity.Principal{}, "", "network_access.access_grants.list", map[string]any{"limit": 10})
	listed := networkToolResult[[]map[string]any](t, networkToolResult[map[string]any](t, output)["items"])[0]
	if err != nil || listed["tokenHash"] != nil || listed["authSessionId"] != nil {
		t.Fatalf("unsafe grant list output=%#v err=%v", output, err)
	}
	output, _, err = service.invokeNetworkAccessTool(context.Background(), domainidentity.Principal{}, "", "network_access.access_grants.revoke", map[string]any{"grantId": "grant-1"})
	if err != nil || grants.revoked != "grant-1" || networkToolResult[map[string]any](t, output)["status"] != domainnetworkruntime.AccessGrantRevoked {
		t.Fatalf("revoke output=%#v revoked=%q err=%v", output, grants.revoked, err)
	}
}

func TestApprovedNetworkAccessGrantPreservesPrivateActorSession(t *testing.T) {
	repo := &memoryGatewayRepository{accessPolicies: []domainaigateway.AccessPolicy{{
		ID: "approve-network-grant", Enabled: true, SubjectType: "role", SubjectID: "operator", Effect: "allow",
		ToolPatterns: []string{"network_access.access_grants.create"}, ApprovalPolicy: map[string]any{"strategy": "require_approval"},
	}}}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{matrix: map[string][]string{
		"operator": {appaccess.PermAIGatewayInvoke, appaccess.PermNetworkAccessAccessGrantsCreate},
		"admin":    {appaccess.PermAIGatewayManage, appaccess.PermAIGatewayInvoke},
	}}), nil, repo)
	grants := &fakeNetworkAccessGrantService{item: domainnetworkruntime.AccessGrant{
		ID: "grant-1", SubjectID: "user-1", DeviceID: "device-1", SiteID: "site-1", NetworkSpaceID: "space-1",
		Mode: domainnetworkaccess.ModeInternalZTNA, ResourceIDs: []string{"resource-1"}, PolicyVersion: 7,
		Status: domainnetworkruntime.AccessGrantIssued, ExpiresAt: time.Now().Add(5 * time.Minute), CreatedBy: "user-1", CreatedAt: time.Now(),
	}}
	service.SetNetworkAccessGrantService(grants)

	held, err := service.InvokeTool(context.Background(), testPrincipal("operator"), domainaigateway.ToolInvocationRequest{
		SessionID: "auth-session-1", ToolName: "network_access.access_grants.create",
		Input: map[string]any{"deviceId": "device-1", "siteId": "site-1", "networkSpaceId": "space-1", "mode": "internal_ztna", "resourceIds": []any{"resource-1"}, "ttlSeconds": 300},
	})
	if err != nil || held.Result != "pending_approval" || grants.sessionID != "" || len(repo.approvalRequests) != 1 || repo.approvalRequests[0].ActorSessionID != "auth-session-1" {
		t.Fatalf("held=%#v approvals=%#v session=%q err=%v", held, repo.approvalRequests, grants.sessionID, err)
	}
	encoded, err := json.Marshal(repo.approvalRequests[0])
	if err != nil || strings.Contains(string(encoded), "auth-session-1") {
		t.Fatalf("approval JSON exposed actor session: %s, %v", encoded, err)
	}
	decision, err := service.ApproveApprovalRequest(context.Background(), testPrincipal("admin"), repo.approvalRequests[0].ID, domainaigateway.ApprovalDecisionInput{Comment: "approved"})
	if err != nil || decision.Invocation == nil || grants.sessionID != "auth-session-1" {
		t.Fatalf("approved decision=%#v replay session=%q err=%v", decision, grants.sessionID, err)
	}
}

func TestNetworkSessionToolsUseSharedServiceAndGateExecution(t *testing.T) {
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{}), nil, &memoryGatewayRepository{})
	networkAccess := &fakeNetworkAccessPreviewService{}
	service.SetNetworkAccessPreviewService(networkAccess)

	for name, expected := range map[string]struct {
		risk     domainaigateway.RiskLevel
		approval bool
	}{
		"network_access.sessions.list":       {domainaigateway.RiskLevelRead, false},
		"network_access.session.coa.plan":    {domainaigateway.RiskLevelAnalyze, false},
		"network_access.session.coa.execute": {domainaigateway.RiskLevelHigh, true},
	} {
		tool, ok := service.toolByName(name)
		if !ok || tool.RiskLevel != expected.risk || tool.RequiresApproval != expected.approval {
			t.Fatalf("tool %s = %#v, present=%v", name, tool, ok)
		}
	}

	output, related, err := service.invokeNetworkAccessTool(context.Background(), domainidentity.Principal{}, "", "network_access.sessions.list", map[string]any{"runtimeId": "runtime-1", "status": "active", "limit": 10})
	if err != nil || networkAccess.sessionFilter.RuntimeID != "runtime-1" || related["count"] != 1 || len(networkToolResult[[]domainnetworkaccess.Session](t, networkToolResult[map[string]any](t, output)["items"])) != 1 {
		t.Fatalf("session list output=%#v related=%#v filter=%#v err=%v", output, related, networkAccess.sessionFilter, err)
	}

	planHash := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	_, related, err = service.invokeNetworkAccessTool(context.Background(), domainidentity.Principal{}, "", "network_access.session.coa.plan", map[string]any{"sessionId": "session-1", "action": "coa", "targetAccessProfile": "restricted", "reasonCode": "risk_changed"})
	if err != nil || networkAccess.actionSession != "session-1" || networkAccess.actionInput.PlanHash != "" || related["sessionId"] != "session-1" {
		t.Fatalf("session plan related=%#v input=%#v err=%v", related, networkAccess.actionInput, err)
	}
	output, related, err = service.invokeNetworkAccessTool(context.Background(), domainidentity.Principal{}, "", "network_access.session.coa.execute", map[string]any{"sessionId": "session-1", "action": "coa", "targetAccessProfile": "restricted", "reasonCode": "risk_changed", "planHash": planHash})
	command := networkToolResult[domainnetworkaccess.SessionCommand](t, output)
	if err != nil || networkAccess.actionInput.PlanHash != planHash || command.ID != "command-1" || related["commandId"] != "command-1" {
		t.Fatalf("session execute output=%#v related=%#v input=%#v err=%v", output, related, networkAccess.actionInput, err)
	}
}

func TestNetworkAccessToolCatalogDeclaresResourceScopes(t *testing.T) {
	for name, expected := range map[string]string{
		"network_access.mihomo_profiles.list": "device",
		"network_access.telemetry.summary":    "producer",
		"network_access.access_grants.list":   "grant",
		"network_access.enrollments.list":     "enrollment",
		"network_access.sessions.list":        "session",
		"network_access.session.coa.plan":     "session",
		"network_access.session.coa.execute":  "session",
	} {
		tool, ok := toolByNameFrom(name, defaultToolCatalog)
		if !ok || len(tool.RequiredScopes) != 1 || tool.RequiredScopes[0] != expected {
			t.Fatalf("tool %s scopes = %#v, present=%v; want [%s]", name, tool.RequiredScopes, ok, expected)
		}
	}
}

func networkToolResult[T any](t *testing.T, value any) T {
	t.Helper()
	result, ok := value.(T)
	if !ok {
		t.Fatalf("unexpected network tool result type %T", value)
	}
	return result
}
