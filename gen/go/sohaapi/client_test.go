package sohaapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDeliveryPlanStatusLegacyConstants(t *testing.T) {
	statuses := map[DeliveryPlanStatus]string{
		Draft:           "draft",
		WaitingApproval: "waiting_approval",
		Confirming:      "confirming",
		Confirmed:       "confirmed",
	}
	for status, want := range statuses {
		if string(status) != want || !status.Valid() {
			t.Fatalf("DeliveryPlanStatus %q is not the compatible value %q", status, want)
		}
	}
}

func TestRuntimeAndWorkbenchLegacyEnumConstants(t *testing.T) {
	scopes := map[RuntimeNetworkUsageScope]string{
		NetworkNamespace: "network_namespace",
		Process:          "process",
		Unavailable:      "unavailable",
	}
	for scope, want := range scopes {
		if string(scope) != want || !scope.Valid() {
			t.Fatalf("RuntimeNetworkUsageScope %q is not the compatible value %q", scope, want)
		}
	}
	statuses := map[WorkbenchAgentStatusEventStatus]string{
		Cancelled: "cancelled",
		Failed:    "failed",
		Queued:    "queued",
		Running:   "running",
		Succeeded: "succeeded",
	}
	for status, want := range statuses {
		if string(status) != want || !status.Valid() {
			t.Fatalf("WorkbenchAgentStatusEventStatus %q is not the compatible value %q", status, want)
		}
	}
}

func TestObservabilityCredentialKeyLegacyTypes(t *testing.T) {
	dataSource := ObservabilityDataSource{
		CredentialKeys: []ObservabilityDataSourceCredentialKeys{
			ObservabilityDataSourceCredentialKeysBearerToken,
		},
	}
	input := ObservabilityDataSourceInput{
		ClearCredentialKeys: []ObservabilityDataSourceInputClearCredentialKeys{
			ObservabilityDataSourceInputClearCredentialKeysPassword,
		},
	}
	if !dataSource.CredentialKeys[0].Valid() || !input.ClearCredentialKeys[0].Valid() {
		t.Fatal("legacy observability credential key constants must remain valid")
	}
}

func TestSAMLAttributeMappingLegacyConstants(t *testing.T) {
	targets := map[SAMLAttributeMappingTarget]string{
		Subject:      "subject",
		Username:     "username",
		Email:        "email",
		DisplayName:  "displayName",
		Role:         "role",
		Organization: "organization",
		Team:         "team",
		Project:      "project",
	}
	for target, want := range targets {
		if string(target) != want || !target.Valid() {
			t.Fatalf("SAMLAttributeMappingTarget %q is not the compatible value %q", target, want)
		}
	}
}

func TestSystemHealthAndReadinessUsePublicRequests(t *testing.T) {
	cases := []struct {
		name string
		path string
		call func(*Client) (GenericObject, error)
	}{
		{
			name: "healthz",
			path: "/api/v1/healthz",
			call: func(client *Client) (GenericObject, error) {
				return client.GetHealthz(context.Background())
			},
		},
		{
			name: "readyz",
			path: "/api/v1/readyz",
			call: func(client *Client) (GenericObject, error) {
				return client.GetReadyz(context.Background())
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				assertPublicRequest(t, r, http.MethodGet, tt.path)
				writeJSON(t, w, map[string]any{"status": "ok", "check": tt.name})
			})

			result, err := tt.call(client)
			if err != nil {
				t.Fatalf("%s returned error: %v", tt.name, err)
			}
			if got := result["status"]; got != "ok" {
				t.Fatalf("%s status = %v, want ok", tt.name, got)
			}
		})
	}
}

func TestRunnerClaimClientsAcceptNoContent(t *testing.T) {
	cases := []struct {
		name string
		path string
		call func(*Client) (string, error)
	}{
		{
			name: "execution task",
			path: "/api/v1/delivery/execution-tasks/claim",
			call: func(client *Client) (string, error) {
				item, err := client.ClaimExecutionTask(context.Background(), ExecutionTaskClaimRequest{
					AgentID: "agent-1", ProviderKinds: []string{"ci_agent_runner"},
				})
				return item.ID, err
			},
		},
		{
			name: "docker operation",
			path: "/api/v1/docker/operations/claim",
			call: func(client *Client) (string, error) {
				item, err := client.ClaimDockerOperation(context.Background(), DockerOperationClaimRequest{
					AgentID: "agent-1", WorkerID: "worker-1", OperationKinds: []string{"host_sync"},
				})
				return item.ID, err
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				assertCommonRequest(t, r, http.MethodPost, tt.path)
				w.WriteHeader(http.StatusNoContent)
			})

			id, err := tt.call(client)
			if err != nil {
				t.Fatalf("claim returned error: %v", err)
			}
			if id != "" {
				t.Fatalf("claim ID = %q, want empty", id)
			}
		})
	}
}

func TestExchangeDockerHostAgentEnrollmentIsPublicAndReturnsCredentials(t *testing.T) {
	issuedAt := time.Now().UTC().Truncate(time.Second)
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assertPublicRequest(t, r, http.MethodPost, "/api/v1/docker/agent-installations/operation-1/enroll")
		request := decodeJSONRequestBody[DockerHostAgentEnrollmentRequest](t, r)
		if request.AgentID != "host-1" || request.EnrollmentToken != "enrollment-secret-0123456789012345" {
			t.Fatalf("enrollment request = %#v", request)
		}
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusCreated)
		writeJSON(t, w, map[string]any{"data": map[string]any{
			"hostId": "host-1", "operationId": "operation-1", "agentId": "host-1",
			"agentBearerToken":   "agent-secret-012345678901234567890",
			"runtimeBearerToken": "runtime-secret-012345678901234567",
			"issuedAt":           issuedAt,
		}})
	})

	credentials, err := client.ExchangeDockerHostAgentEnrollment(context.Background(), "operation-1", DockerHostAgentEnrollmentRequest{
		AgentID: "host-1", EnrollmentToken: "enrollment-secret-0123456789012345",
	})
	if err != nil {
		t.Fatalf("exchange enrollment: %v", err)
	}
	if credentials.HostID != "host-1" || credentials.OperationID != "operation-1" || credentials.RuntimeBearerToken == "" || credentials.AgentBearerToken == "" {
		t.Fatalf("credentials = %#v", credentials)
	}
}

func TestRedeemSecretLeaseSendsRunnerBindingHeaders(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assertCommonRequest(t, r, http.MethodPost, "/api/v1/runner/secret-leases/lease%2Fone/redeem")
		if got := r.Header.Get("X-Soha-Secret-Lease-Token"); got != "one-time-token" {
			t.Fatalf("lease token = %q", got)
		}
		if got := r.Header.Get("X-Soha-Agent-ID"); got != "agent-1" {
			t.Fatalf("agent id = %q", got)
		}
		writeJSON(t, w, map[string]any{"data": map[string]any{
			"leaseId": "lease/one", "expiresAt": time.Now().UTC().Add(time.Minute), "values": map[string]string{"REGISTRY_TOKEN": "secret-value"},
		}})
	})

	result, err := client.RedeemSecretLease(context.Background(), "lease/one", RedeemSecretLeaseParams{
		XSohaSecretLeaseToken: "one-time-token",
		XSohaAgentID:          "agent-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Values["REGISTRY_TOKEN"] != "secret-value" {
		t.Fatalf("redeemed value = %q", result.Values["REGISTRY_TOKEN"])
	}
}

func TestIdentityOutpostRuntimeClientSurface(t *testing.T) {
	now := time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC)
	requests := 0
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch requests {
		case 1:
			assertCommonRequest(t, r, http.MethodPost, "/api/v1/identity/outposts/runtime/claim")
			body := decodeJSONRequestBody[IdentityOutpostClaimRequest](t, r)
			if body.AgentID != "agent/one" || body.CurrentConfigurationVersion != 7 {
				t.Fatalf("unexpected claim request %#v", body)
			}
			writeJSON(t, w, map[string]any{"data": map[string]any{
				"outpostId": "outpost/one", "protocolVersion": "1", "configurationVersion": 8,
				"issuedAt": now, "expiresAt": now.Add(time.Minute), "keyId": "key-1", "checkUrl": "https://soha.example/api/v1/identity/outposts/outpost%2Fone/check",
				"routes": []any{}, "signature": "signature",
			}})
		case 2:
			assertCommonRequest(t, r, http.MethodPost, "/api/v1/identity/outposts/outpost%2Fone/heartbeat")
			body := decodeJSONRequestBody[IdentityOutpostHeartbeatRequest](t, r)
			if body.AgentID != "agent/one" || body.ConfigurationVersion != 8 {
				t.Fatalf("unexpected heartbeat request %#v", body)
			}
			writeJSON(t, w, map[string]any{"data": map[string]any{"accepted": true, "desiredConfigurationVersion": 8}})
		case 3:
			assertCommonRequest(t, r, http.MethodPost, "/api/v1/identity/outposts/outpost%2Fone/check")
			body := decodeJSONRequestBody[IdentityOutpostAccessCheckRequest](t, r)
			if body.RequestPath != "/admin" || body.ConfigurationVersion != 8 {
				t.Fatalf("unexpected access check request %#v", body)
			}
			writeJSON(t, w, map[string]any{"data": map[string]any{"decision": "allow", "statusCode": 200}})
		case 4:
			assertCommonRequest(t, r, http.MethodPost, "/api/v1/identity/outposts/outpost%2Fone/events")
			body := decodeJSONRequestBody[IdentityOutpostEventBatchRequest](t, r)
			if body.AgentID != "agent/one" || len(body.Events) != 1 {
				t.Fatalf("unexpected event batch %#v", body)
			}
			writeJSON(t, w, map[string]any{"status": "accepted"})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})

	config, err := client.ClaimIdentityOutpostConfig(context.Background(), IdentityOutpostClaimRequest{
		AgentID: "agent/one", SupportedProtocolVersion: "1", CurrentConfigurationVersion: 7,
	})
	if err != nil || config == nil || config.ConfigurationVersion != 8 {
		t.Fatalf("ClaimIdentityOutpostConfig = %#v, %v", config, err)
	}
	heartbeat, err := client.HeartbeatIdentityOutpost(context.Background(), "outpost/one", IdentityOutpostHeartbeatRequest{
		AgentID: "agent/one", ConfigurationVersion: 8, Status: IdentityOutpostHeartbeatRequestStatusHealthy, CheckedAt: now,
	})
	if err != nil || !heartbeat.Accepted {
		t.Fatalf("HeartbeatIdentityOutpost = %#v, %v", heartbeat, err)
	}
	decision, err := client.CheckIdentityOutpostAccess(context.Background(), "outpost/one", IdentityOutpostAccessCheckRequest{
		ConfigurationVersion: 8, ProviderID: "provider-1", OriginalURL: "https://app.example/admin",
		Method: http.MethodGet, RequestHost: "app.example", RequestPath: "/admin",
	})
	if err != nil || decision.Decision != IdentityOutpostAccessCheckDecisionAllow {
		t.Fatalf("CheckIdentityOutpostAccess = %#v, %v", decision, err)
	}
	status, err := client.AppendIdentityOutpostEvents(context.Background(), "outpost/one", IdentityOutpostEventBatchRequest{
		AgentID: "agent/one",
		Events: []IdentityOutpostRuntimeEvent{{
			ID: "event-1", Type: ConfigurationApplied, ConfigurationVersion: 8, OccurredAt: now,
		}},
	})
	if err != nil || status.Status != "accepted" {
		t.Fatalf("AppendIdentityOutpostEvents = %#v, %v", status, err)
	}
}

func TestIdentityMFAClientSurface(t *testing.T) {
	now := time.Date(2026, 7, 27, 11, 0, 0, 0, time.UTC)
	requests := 0
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch requests {
		case 1:
			assertCommonRequest(t, r, http.MethodPost, "/api/v1/identity/mfa/webauthn/authenticate")
			body := decodeJSONRequestBody[MFAWebAuthnAuthenticationRequest](t, r)
			if body.Purpose != MFAWebAuthnPurposeStepUp || body.ApplicationID != "app/one" {
				t.Fatalf("unexpected WebAuthn authentication request %#v", body)
			}
			writeJSON(t, w, map[string]any{"data": map[string]any{
				"challengeId": "challenge-1", "challenge": "challenge", "rpId": "soha.example.com",
				"timeoutMilliseconds": 60000, "userVerification": "required", "expiresAt": now.Add(time.Minute),
			}})
		case 2:
			assertCommonRequest(t, r, http.MethodPost, "/api/v1/identity/users/user%2Fone/mfa/credentials/credential%2Fone/revoke")
			writeJSON(t, w, map[string]any{"status": "revoked"})
		case 3:
			assertCommonRequest(t, r, http.MethodPost, "/api/v1/identity/users/user%2Fone/mfa/reset")
			body := decodeJSONRequestBody[MFAAdminResetRequest](t, r)
			if body.Reason != "security incident" || !body.RevokeSessions {
				t.Fatalf("unexpected MFA reset request %#v", body)
			}
			writeJSON(t, w, map[string]any{"data": map[string]any{
				"userId": "user/one", "revokedCredentialCount": 2, "recoveryCodesRevoked": 8,
				"sessionsRevoked": 3, "resetAt": now,
			}})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})

	options, err := client.BeginWebAuthnAuthentication(context.Background(), MFAWebAuthnAuthenticationRequest{
		Purpose: MFAWebAuthnPurposeStepUp, ApplicationID: "app/one",
	})
	if err != nil || options.ChallengeID != "challenge-1" || options.UserVerification != MFAWebAuthnUserVerificationRequired {
		t.Fatalf("BeginWebAuthnAuthentication = %#v, %v", options, err)
	}
	status, err := client.AdminRevokeUserMFACredential(context.Background(), "user/one", "credential/one")
	if err != nil || status.Status != "revoked" {
		t.Fatalf("AdminRevokeUserMFACredential = %#v, %v", status, err)
	}
	reset, err := client.AdminResetUserMFA(context.Background(), "user/one", MFAAdminResetRequest{
		Reason: "security incident", RevokeSessions: true,
	})
	if err != nil || reset.RevokedCredentialCount != 2 || reset.SessionsRevoked != 3 {
		t.Fatalf("AdminResetUserMFA = %#v, %v", reset, err)
	}
}

func TestComputeTaskCenterClientSurface(t *testing.T) {
	requests := 0
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch requests {
		case 1:
			assertCommonRequest(t, r, http.MethodGet, "/api/v1/compute/tasks")
			assertQuery(t, r, map[string]string{
				"domain": "virtualization", "resourceKind": "cluster", "resourceId": "cluster-1", "sortBy": "status", "sortOrder": "desc", "limit": "1",
			})
			writeJSON(t, w, map[string]any{"items": []any{}})
		case 2:
			assertCommonRequest(t, r, http.MethodGet, "/api/v1/compute/tasks/virtualization/task%2Fone")
			writeJSON(t, w, computeTaskEnvelopeFixture("task/one"))
		case 3:
			assertCommonRequest(t, r, http.MethodGet, "/api/v1/compute/tasks/virtualization/task%2Fone/logs")
			writeJSON(t, w, map[string]any{"items": []map[string]any{{
				"id": "log-1", "taskId": "task/one", "logLevel": "info", "message": "started", "createdAt": "2026-07-16T08:00:00Z",
			}}})
		case 4:
			assertCommonRequest(t, r, http.MethodPost, "/api/v1/compute/tasks/virtualization/task%2Fone/cancel")
			if key := r.Header.Get("Idempotency-Key"); !strings.HasPrefix(key, "compute-") {
				t.Fatalf("cancel Idempotency-Key = %q", key)
			}
			if body := decodeJSONRequestBody[ComputeTaskMutationRequest](t, r); body.Reason != "operator request" {
				t.Fatalf("cancel reason = %q", body.Reason)
			}
			writeJSON(t, w, computeTaskEnvelopeFixture("task/one"))
		case 5:
			assertCommonRequest(t, r, http.MethodPost, "/api/v1/compute/tasks/virtualization/task%2Fone/retry")
			if key := r.Header.Get("Idempotency-Key"); key != "retry-key-1" {
				t.Fatalf("retry Idempotency-Key = %q", key)
			}
			writeJSON(t, w, computeTaskEnvelopeFixture("task/two"))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})

	items, err := client.ListComputeTasks(context.Background(), ListComputeTasksParams{
		Domain: ComputeTaskDomainVirtualization, ResourceKind: "cluster", ResourceID: "cluster-1", SortBy: "status", SortOrder: ListComputeTasksParamsSortOrder("desc"), Limit: 1,
	})
	if err != nil || len(items.Items) != 0 {
		t.Fatalf("ListComputeTasks = %#v, %v", items, err)
	}
	if task, err := client.GetComputeTask(context.Background(), ComputeTaskDomainVirtualization, "task/one"); err != nil || task.ID != "task/one" {
		t.Fatalf("GetComputeTask = %#v, %v", task, err)
	}
	if logs, err := client.ListComputeTaskLogs(context.Background(), ComputeTaskDomainVirtualization, "task/one"); err != nil || len(logs) != 1 {
		t.Fatalf("ListComputeTaskLogs = %#v, %v", logs, err)
	}
	if _, err := client.CancelComputeTask(context.Background(), ComputeTaskDomainVirtualization, "task/one", ComputeTaskMutationRequest{Reason: "operator request"}); err != nil {
		t.Fatalf("CancelComputeTask returned error: %v", err)
	}
	if task, err := client.RetryComputeTaskWithKey(context.Background(), ComputeTaskDomainVirtualization, "task/one", "retry-key-1", ComputeTaskMutationRequest{}); err != nil || task.ID != "task/two" {
		t.Fatalf("RetryComputeTask = %#v, %v", task, err)
	}
}

func TestComputeInventoryClientSurface(t *testing.T) {
	requests := 0
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch requests {
		case 1:
			assertCommonRequest(t, r, http.MethodGet, "/api/v1/compute/capabilities")
			writeJSON(t, w, map[string]any{"data": map[string]any{"generation": 1, "features": []any{}}})
		case 2:
			assertCommonRequest(t, r, http.MethodGet, "/api/v1/compute/overview")
			writeJSON(t, w, map[string]any{"data": map[string]any{"attention": []any{}, "providerHealth": []any{}, "partial": false, "warnings": []any{}}})
		case 3:
			assertCommonRequest(t, r, http.MethodGet, "/api/v1/compute/access-sources")
			assertQuery(t, r, map[string]string{"sourceType": "virtualization_connection", "providerKey": "pve", "cursor": "next", "limit": "10"})
			writeJSON(t, w, map[string]any{"items": []any{}})
		case 4:
			assertCommonRequest(t, r, http.MethodGet, "/api/v1/compute/providers")
			assertQuery(t, r, map[string]string{"domain": "virtualization", "source": "builtin", "limit": "10"})
			writeJSON(t, w, map[string]any{"items": []any{}})
		case 5:
			assertCommonRequest(t, r, http.MethodGet, "/api/v1/compute/provider-instances")
			assertQuery(t, r, map[string]string{"domain": "virtualization", "providerKey": "pve", "limit": "10"})
			writeJSON(t, w, map[string]any{"items": []any{}})
		case 6:
			assertCommonRequest(t, r, http.MethodGet, "/api/v1/compute/provider-instances/virtualization/pve/connection%2Fone")
			writeJSON(t, w, map[string]any{"data": map[string]any{}})
		case 7:
			assertCommonRequest(t, r, http.MethodPost, "/api/v1/compute/provider-instances/virtualization/pve/connection%2Fone/health-checks")
			if key := r.Header.Get("Idempotency-Key"); key != "health-key-1" {
				t.Fatalf("health Idempotency-Key = %q", key)
			}
			writeJSON(t, w, map[string]any{"data": map[string]any{"healthy": true, "status": "healthy", "checkedAt": "2026-09-22T00:00:00Z"}})
		case 8:
			assertCommonRequest(t, r, http.MethodPost, "/api/v1/compute/provider-instances/virtualization/pve/connection%2Fone/discoveries")
			if key := r.Header.Get("Idempotency-Key"); key != "discover-key-1" {
				t.Fatalf("discover Idempotency-Key = %q", key)
			}
			writeJSON(t, w, computeTaskEnvelopeFixture("discover-task"))
		case 9:
			assertCommonRequest(t, r, http.MethodGet, "/api/v1/compute/resources/virtualization/vm/vm%2Fone")
			writeJSON(t, w, map[string]any{"data": map[string]any{"id": "vm/one"}})
		case 10:
			assertCommonRequest(t, r, http.MethodGet, "/api/v1/compute/resources/virtualization/vm/vm%2Fone/relations")
			assertQuery(t, r, map[string]string{"cursor": "next", "limit": "20"})
			writeJSON(t, w, map[string]any{"data": map[string]any{"resource": map[string]any{"domain": "virtualization", "kind": "vm", "id": "vm/one", "displayName": "vm-one"}, "relations": []any{}}})
		case 11:
			assertCommonRequest(t, r, http.MethodPost, "/api/v1/compute/resources/virtualization/vm/vm%2Fone/actions/start")
			if key := r.Header.Get("Idempotency-Key"); key != "action-key-1" {
				t.Fatalf("action Idempotency-Key = %q", key)
			}
			writeJSON(t, w, computeTaskEnvelopeFixture("action-task"))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})

	ctx := context.Background()
	if _, err := client.GetComputeCapabilities(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetComputeOverview(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListComputeAccessSources(ctx, ListComputeAccessSourcesParams{SourceType: ComputeAccessSourceTypeVirtualizationConnection, ProviderKey: "pve", Cursor: "next", Limit: 10}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListComputeProviders(ctx, ListComputeProvidersParams{Domain: ComputeProviderDomainVirtualization, Source: ComputeProviderSourceBuiltin, Limit: 10}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListComputeProviderInstances(ctx, ListComputeProviderInstancesParams{Domain: ComputeProviderDomainVirtualization, ProviderKey: "pve", Limit: 10}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetComputeProviderInstance(ctx, ComputeProviderDomainVirtualization, "pve", "connection/one"); err != nil {
		t.Fatal(err)
	}
	if result, err := client.CheckComputeProviderInstanceHealth(ctx, ComputeProviderDomainVirtualization, "pve", "connection/one", "health-key-1", ComputeProviderReadRequest{ExpectedGeneration: 1}); err != nil || !result.Healthy || result.Status != "healthy" || result.CheckedAt.IsZero() {
		t.Fatalf("CheckComputeProviderInstanceHealth = %#v, %v", result, err)
	}
	if _, err := client.DiscoverComputeProviderInstance(ctx, ComputeProviderDomainVirtualization, "pve", "connection/one", "discover-key-1", ComputeProviderDiscoverRequest{ExpectedGeneration: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetComputeResource(ctx, ComputeDomainVirtualization, ComputeResourceKindVM, "vm/one"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListComputeResourceRelations(ctx, ComputeDomainVirtualization, ComputeResourceKindVM, "vm/one", ListComputeResourceRelationsParams{Cursor: "next", Limit: 20}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ExecuteComputeResourceAction(ctx, ComputeDomainVirtualization, ComputeResourceKindVM, "vm/one", "start", "action-key-1", ComputeResourceActionRequest{}); err != nil {
		t.Fatal(err)
	}
}

func TestComputeProviderHealthPreservesDiagnostics(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{"data": map[string]any{
			"healthy": false, "status": "degraded", "checkedAt": "2026-09-22T00:00:00Z",
			"httpStatus": 401, "reason": "authentication_failed", "message": "Credentials rejected", "nextAction": "Update credentials",
		}})
	})
	result, err := client.CheckComputeProviderInstanceHealth(context.Background(), ComputeProviderDomainVirtualization, "pve", "connection-1", "", ComputeProviderReadRequest{})
	if err != nil || result.Healthy || result.Status != "degraded" || result.HTTPStatus != 401 || result.Reason != "authentication_failed" || result.Message != "Credentials rejected" || result.NextAction != "Update credentials" || result.CheckedAt.IsZero() {
		t.Fatalf("CheckComputeProviderInstanceHealth = %#v, %v", result, err)
	}
}

func TestComputeProviderHealthRejectsLegacyTask(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		writeJSON(t, w, computeTaskEnvelopeFixture("health-task"))
	})
	if _, err := client.CheckComputeProviderInstanceHealth(context.Background(), ComputeProviderDomainVirtualization, "pve", "connection-1", "", ComputeProviderReadRequest{}); err == nil || !strings.Contains(err.Error(), "upgrade the server") {
		t.Fatalf("legacy task response should require a server upgrade, got %v", err)
	}
}

func TestStreamComputeTask(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.EscapedPath() != "/api/v1/compute/tasks/virtualization/task%2Fone/stream" {
			t.Fatalf("request = %s %s", r.Method, r.URL.EscapedPath())
		}
		if accept := r.Header.Get("Accept"); accept != "text/event-stream" {
			t.Fatalf("Accept = %q", accept)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" || r.Header.Get("User-Agent") != "sohaapi-test" {
			t.Fatalf("unexpected authenticated stream headers: %#v", r.Header)
		}
		_, _ = io.WriteString(w, "event: snapshot\ndata: {\"type\":\"snapshot\",\"observedAt\":\"2026-08-24T12:00:00Z\",\"sequence\":1}\n\n")
	})

	var events []ComputeTaskStreamEvent
	err := client.StreamComputeTask(context.Background(), ComputeTaskDomainVirtualization, "task/one", func(event ComputeTaskStreamEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil || len(events) != 1 || events[0].Sequence != 1 {
		t.Fatalf("StreamComputeTask events=%#v err=%v", events, err)
	}
}

func TestResourceLogClientSurface(t *testing.T) {
	requests := []string{
		"/api/v1/clusters/cluster%2Fone/logs/query",
		"/api/v1/docker/projects/project%2Fone/logs/query",
		"/api/v1/delivery/applications/app%2Fone/environments/env%2Fone/logs/query",
	}
	requestIndex := 0
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assertCommonRequest(t, r, http.MethodPost, requests[requestIndex])
		query := decodeJSONRequestBody[LogQuery](t, r)
		if query.Selector == nil || query.Selector.Namespace != "default" {
			t.Fatalf("log query selector = %#v", query.Selector)
		}
		writeJSON(t, w, map[string]any{"data": map[string]any{
			"entries": []any{}, "partial": false, "truncated": false, "scopeRestricted": false,
		}})
		requestIndex++
	})

	query := LogQuery{Selector: &LogSourceSelector{Namespace: "default"}}
	if _, err := client.QueryClusterLogs(context.Background(), "cluster/one", query); err != nil {
		t.Fatal(err)
	}
	if _, err := client.QueryDockerProjectLogs(context.Background(), "project/one", query); err != nil {
		t.Fatal(err)
	}
	if _, err := client.QueryDeliveryEnvironmentLogs(context.Background(), "app/one", "env/one", query); err != nil {
		t.Fatal(err)
	}
	if requestIndex != len(requests) {
		t.Fatalf("requests = %d, want %d", requestIndex, len(requests))
	}
}

func computeTaskEnvelopeFixture(id string) map[string]any {
	return map[string]any{"data": map[string]any{
		"id": id, "domain": "virtualization", "sourceType": "operation", "sourceId": id,
		"kind": "sync", "category": "sync", "normalizedStatus": "running", "rawStatus": "running",
		"resources": []any{}, "attemptCount": 1, "cancelable": true, "retryable": false,
		"availableActions": []string{"logs", "cancel"}, "createdAt": "2026-07-16T08:00:00Z",
	}}
}

func TestListAuthProvidersBuildsPublicRequestAndDecodesItems(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assertPublicRequest(t, r, http.MethodGet, "/api/v1/auth/providers")
		assertQuery(t, r, map[string]string{})
		writeJSON(t, w, map[string]any{
			"items": []map[string]any{
				{
					"id":       "local",
					"name":     "Password",
					"type":     "password",
					"enabled":  true,
					"loginUrl": "/auth/login",
				},
			},
		})
	})

	items, err := client.ListAuthProviders(context.Background())
	if err != nil {
		t.Fatalf("ListAuthProviders returned error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d auth providers, want 1", len(items))
	}
	if got := items[0].ID; got != "local" {
		t.Fatalf("auth provider ID = %q, want local", got)
	}
	if !items[0].Enabled {
		t.Fatal("auth provider Enabled = false, want true")
	}
}

func TestKnowledgeClientSearchAndListPaths(t *testing.T) {
	requests := 0
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch requests {
		case 1:
			assertCommonRequest(t, r, http.MethodGet, "/api/v1/ai/knowledge-bases/base%2Fone/documents")
			assertQuery(t, r, map[string]string{"limit": "25"})
			writeJSON(t, w, map[string]any{"items": []any{}})
		case 2:
			assertCommonRequest(t, r, http.MethodPost, "/api/v1/ai/knowledge/search")
			body := decodeJSONRequestBody[KnowledgeSearchRequest](t, r)
			if body.Query != "rollback" || len(body.KnowledgeBaseIDs) != 1 || body.KnowledgeBaseIDs[0] != "base/one" {
				t.Fatalf("unexpected knowledge search body %#v", body)
			}
			writeJSON(t, w, map[string]any{"data": map[string]any{
				"query": "rollback", "hits": []any{}, "citations": []any{}, "candidateCount": 0,
				"timingMs": 2, "noAnswer": true, "traceId": "trace-1",
			}})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})

	items, err := client.ListKnowledgeDocuments(context.Background(), "base/one", 25)
	if err != nil || len(items) != 0 {
		t.Fatalf("ListKnowledgeDocuments = %#v, %v", items, err)
	}
	result, err := client.SearchKnowledge(context.Background(), KnowledgeSearchRequest{
		KnowledgeBaseIDs: []string{"base/one"}, Query: "rollback", TopK: 5,
	})
	if err != nil {
		t.Fatalf("SearchKnowledge returned error: %v", err)
	}
	if !result.NoAnswer || result.TraceID != "trace-1" {
		t.Fatalf("unexpected knowledge result %#v", result)
	}
}

func TestAgentProviderControlPlaneClientSurface(t *testing.T) {
	observedAt := time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC)
	catalog := AgentProviderCatalog{
		SchemaVersion: "opensoha.dev/agent-provider-catalog/v1",
		Revision:      3,
		Digest:        "sha256:catalog",
		CreatedAt:     observedAt,
		Providers:     []AgentProviderDefinition{},
	}
	ack := AgentProviderRegistryAcknowledgement{
		RunnerID: "runner/one", Revision: 3, ActiveRevision: 3, Accepted: true, ObservedAt: observedAt,
	}
	snapshot := AgentProviderRegistrySnapshot{
		SchemaVersion: "opensoha.dev/agent-provider-registry/v1",
		Revision:      3,
		Digest:        "sha256:snapshot",
		IssuedAt:      observedAt,
		Providers:     []AgentProviderDefinition{},
	}

	requests := 0
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch requests {
		case 1:
			assertCommonRequest(t, r, http.MethodGet, "/api/v1/ai/agent-providers/catalog")
			writeJSON(t, w, map[string]any{"data": catalog})
		case 2:
			assertCommonRequest(t, r, http.MethodGet, "/api/v1/ai/agent-providers/runtime-status")
			writeJSON(t, w, map[string]any{"data": AgentProviderRuntimeStatus{
				CatalogRevision: 3, CatalogDigest: catalog.Digest, RunnerCount: 1,
				Acknowledgements: []AgentProviderRegistryAcknowledgement{ack},
			}})
		case 3:
			assertCommonRequest(t, r, http.MethodGet, "/api/v1/ai/agent-providers/registry-snapshot")
			assertQuery(t, r, map[string]string{"runnerId": "runner/one"})
			writeJSON(t, w, map[string]any{"data": snapshot})
		case 4:
			assertCommonRequest(t, r, http.MethodPost, "/api/v1/ai/agent-providers/registry-acks")
			body := decodeJSONRequestBody[AgentProviderRegistryAcknowledgement](t, r)
			if body.RunnerID != ack.RunnerID || body.Revision != ack.Revision || !body.Accepted {
				t.Fatalf("unexpected registry acknowledgement %#v", body)
			}
			writeJSON(t, w, map[string]any{"data": ack})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})

	if item, err := client.GetAgentProviderCatalog(context.Background()); err != nil || item.Revision != 3 {
		t.Fatalf("GetAgentProviderCatalog = %#v, %v", item, err)
	}
	if item, err := client.GetAgentProviderRuntimeStatus(context.Background()); err != nil || item.RunnerCount != 1 {
		t.Fatalf("GetAgentProviderRuntimeStatus = %#v, %v", item, err)
	}
	if item, err := client.GetAgentProviderRegistrySnapshot(context.Background(), ack.RunnerID); err != nil || item.Revision != 3 {
		t.Fatalf("GetAgentProviderRegistrySnapshot = %#v, %v", item, err)
	}
	if item, err := client.AcknowledgeAgentProviderRegistry(context.Background(), ack); err != nil || item.RunnerID != ack.RunnerID {
		t.Fatalf("AcknowledgeAgentProviderRegistry = %#v, %v", item, err)
	}
}

func TestEvaluationClientSurface(t *testing.T) {
	createdAt := time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC)
	dataset := EvaluationDataset{
		SchemaVersion: "opensoha.dev/evaluation-dataset/v1",
		ID:            "rag-regression",
		Name:          "RAG regression",
		Version:       "v1",
		Samples:       []EvaluationDatasetSample{{ID: "sample-1", Input: "What failed?"}},
		CreatedAt:     createdAt,
	}
	run := EvaluationRun{
		SchemaVersion:  "opensoha.dev/evaluation-run/v1",
		ID:             "run/1",
		DatasetID:      dataset.ID,
		DatasetVersion: dataset.Version,
		CandidateRefs:  map[string]string{"prompt": "prompt:v2"},
		Status:         EvaluationRunStatus("running"),
		StartedAt:      createdAt,
	}

	requests := 0
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch requests {
		case 1:
			assertCommonRequest(t, r, http.MethodGet, "/api/v1/ai/evaluations/datasets")
			writeJSON(t, w, map[string]any{"items": []EvaluationDataset{dataset}})
		case 2:
			assertCommonRequest(t, r, http.MethodPost, "/api/v1/ai/evaluations/datasets")
			if body := decodeJSONRequestBody[EvaluationDataset](t, r); body.ID != dataset.ID {
				t.Fatalf("dataset id = %q", body.ID)
			}
			writeJSON(t, w, map[string]any{"data": dataset})
		case 3:
			assertCommonRequest(t, r, http.MethodGet, "/api/v1/ai/evaluations/runs")
			writeJSON(t, w, map[string]any{"items": []EvaluationRun{run}})
		case 4:
			assertCommonRequest(t, r, http.MethodPost, "/api/v1/ai/evaluations/runs")
			if body := decodeJSONRequestBody[EvaluationRun](t, r); body.CandidateRefs["prompt"] != "prompt:v2" {
				t.Fatalf("candidate refs = %#v", body.CandidateRefs)
			}
			writeJSON(t, w, map[string]any{"data": run})
		case 5:
			assertCommonRequest(t, r, http.MethodGet, "/api/v1/ai/evaluations/runs/run%2F1")
			writeJSON(t, w, map[string]any{"data": run})
		case 6:
			assertCommonRequest(t, r, http.MethodGet, "/api/v1/ai/evaluations/runs/run%2F1/results")
			writeJSON(t, w, map[string]any{"items": []EvaluationResult{{
				SchemaVersion: "opensoha.dev/evaluation-result/v1", SampleID: "sample-1",
				Scores: map[string]float32{"fact_recall": 1}, Passed: true,
			}}})
		case 7:
			assertCommonRequest(t, r, http.MethodPost, "/api/v1/ai/evaluations/runs/run%2F1/complete")
			body := decodeJSONRequestBody[EvaluationCompleteRunInput](t, r)
			if len(body.Outputs) != 1 || body.Outputs[0].SampleID != "sample-1" {
				t.Fatalf("completion input = %#v", body)
			}
			completed := run
			completed.Status = EvaluationRunStatus("completed")
			completed.AggregateScores = map[string]float32{"fact_recall": 1}
			writeJSON(t, w, map[string]any{"data": completed})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})

	if items, err := client.ListEvaluationDatasets(context.Background()); err != nil || len(items) != 1 {
		t.Fatalf("ListEvaluationDatasets = %#v, %v", items, err)
	}
	if item, err := client.CreateEvaluationDataset(context.Background(), dataset); err != nil || item.ID != dataset.ID {
		t.Fatalf("CreateEvaluationDataset = %#v, %v", item, err)
	}
	if items, err := client.ListEvaluationRuns(context.Background()); err != nil || len(items) != 1 {
		t.Fatalf("ListEvaluationRuns = %#v, %v", items, err)
	}
	if item, err := client.StartEvaluationRun(context.Background(), run); err != nil || item.ID != run.ID {
		t.Fatalf("StartEvaluationRun = %#v, %v", item, err)
	}
	if item, err := client.GetEvaluationRun(context.Background(), run.ID); err != nil || item.ID != run.ID {
		t.Fatalf("GetEvaluationRun = %#v, %v", item, err)
	}
	if items, err := client.ListEvaluationRunResults(context.Background(), run.ID); err != nil || len(items) != 1 || !items[0].Passed {
		t.Fatalf("ListEvaluationRunResults = %#v, %v", items, err)
	}
	completed, err := client.CompleteEvaluationRun(context.Background(), run.ID, EvaluationCompleteRunInput{
		Outputs: []EvaluationSampleOutput{{SampleID: "sample-1", ProducedFacts: []string{"fact"}}},
	})
	if err != nil || completed.Status != EvaluationRunStatus("completed") {
		t.Fatalf("CompleteEvaluationRun = %#v, %v", completed, err)
	}
}

func TestGetAuthLoginOptionsBuildsPublicRequestAndDecodesData(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assertPublicRequest(t, r, http.MethodGet, "/api/v1/auth/login-options")
		writeJSON(t, w, map[string]any{
			"data": map[string]any{
				"verification": map[string]any{"sliderEnabled": true},
			},
		})
	})

	options, err := client.GetAuthLoginOptions(context.Background())
	if err != nil {
		t.Fatalf("GetAuthLoginOptions returned error: %v", err)
	}
	if !options.Verification.SliderEnabled {
		t.Fatal("login options slider enabled = false, want true")
	}
}

func TestPasswordLoginPostsPublicBodyAndDecodesData(t *testing.T) {
	expiresAt := time.Date(2026, 6, 9, 8, 0, 0, 0, time.UTC)
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assertPublicRequest(t, r, http.MethodPost, "/api/v1/auth/login")
		body := decodeJSONRequestBody[PasswordLoginRequest](t, r)
		if got := body.Login; got != "admin" {
			t.Fatalf("password login login = %q, want admin", got)
		}
		if got := body.Password; got != "secret" {
			t.Fatalf("password login password = %q, want secret", got)
		}
		writeJSON(t, w, authResultJSON(expiresAt, "access-login"))
	})

	result, err := client.PasswordLogin(context.Background(), PasswordLoginRequest{
		Login:    "admin",
		Password: "secret",
	})
	if err != nil {
		t.Fatalf("PasswordLogin returned error: %v", err)
	}
	if got := result.Tokens.AccessToken; got != "access-login" {
		t.Fatalf("password login access token = %q, want access-login", got)
	}
	if got := result.User.UserID; got != "user-1" {
		t.Fatalf("password login user ID = %q, want user-1", got)
	}
}

func TestRefreshAuthSessionPostsPublicOptionalBodyAndDecodesData(t *testing.T) {
	expiresAt := time.Date(2026, 6, 9, 9, 0, 0, 0, time.UTC)
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assertPublicRequest(t, r, http.MethodPost, "/api/v1/auth/refresh")
		body := decodeJSONRequestBody[RefreshRequest](t, r)
		if got := body.RefreshToken; got != "refresh-1" {
			t.Fatalf("refresh token = %q, want refresh-1", got)
		}
		writeJSON(t, w, authResultJSON(expiresAt, "access-refresh"))
	})

	result, err := client.RefreshAuthSession(context.Background(), &RefreshRequest{RefreshToken: "refresh-1"})
	if err != nil {
		t.Fatalf("RefreshAuthSession returned error: %v", err)
	}
	if got := result.Tokens.AccessToken; got != "access-refresh" {
		t.Fatalf("refresh access token = %q, want access-refresh", got)
	}
	if got := result.Tokens.ExpiresAt; !got.Equal(expiresAt) {
		t.Fatalf("refresh ExpiresAt = %s, want %s", got, expiresAt)
	}
}

func TestExchangeOIDCCodePostsPublicBodyAndDecodesData(t *testing.T) {
	expiresAt := time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC)
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assertPublicRequest(t, r, http.MethodPost, "/api/v1/auth/oidc/exchange")
		body := decodeJSONRequestBody[OIDCExchangeRequest](t, r)
		if got := body.Code; got != "oidc-code" {
			t.Fatalf("OIDC exchange code = %q, want oidc-code", got)
		}
		writeJSON(t, w, authResultJSON(expiresAt, "access-oidc"))
	})

	result, err := client.ExchangeOIDCCode(context.Background(), OIDCExchangeRequest{Code: "oidc-code"})
	if err != nil {
		t.Fatalf("ExchangeOIDCCode returned error: %v", err)
	}
	if got := result.Tokens.AccessToken; got != "access-oidc" {
		t.Fatalf("OIDC exchange access token = %q, want access-oidc", got)
	}
}

func TestCurrentAuthMethodsBuildBearerRequestsAndDecodeData(t *testing.T) {
	lastLoginAt := time.Date(2026, 6, 9, 11, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		path string
		call func(*testing.T, *Client) error
		body map[string]any
	}{
		{
			name: "me",
			path: "/api/v1/auth/me",
			call: func(t *testing.T, client *Client) error {
				t.Helper()
				principal, err := client.GetCurrentPrincipal(context.Background())
				if err != nil {
					return err
				}
				if got := principal.UserID; got != "user-1" {
					t.Fatalf("current principal user ID = %q, want user-1", got)
				}
				return nil
			},
			body: map[string]any{"data": principalJSON("user-1")},
		},
		{
			name: "profile",
			path: "/api/v1/auth/profile",
			call: func(t *testing.T, client *Client) error {
				t.Helper()
				profile, err := client.GetCurrentUserProfile(context.Background())
				if err != nil {
					return err
				}
				if got := profile.Username; got != "admin" {
					t.Fatalf("current user profile username = %q, want admin", got)
				}
				if profile.LastLoginAt == nil || !profile.LastLoginAt.Equal(lastLoginAt) {
					t.Fatalf("current user profile LastLoginAt = %v, want %s", profile.LastLoginAt, lastLoginAt)
				}
				return nil
			},
			body: map[string]any{
				"data": map[string]any{
					"userId":      "user-1",
					"username":    "admin",
					"displayName": "Admin User",
					"email":       "admin@example.com",
					"roles":       []string{"admin"},
					"teams":       []string{"platform"},
					"projects":    []string{"default"},
					"tags":        []string{"ops"},
					"status":      "active",
					"lastLoginAt": lastLoginAt,
					"sessions":    []map[string]any{{"id": "session-1"}},
				},
			},
		},
		{
			name: "bootstrap",
			path: "/api/v1/auth/bootstrap",
			call: func(t *testing.T, client *Client) error {
				t.Helper()
				bootstrap, err := client.GetAuthBootstrap(context.Background())
				if err != nil {
					return err
				}
				if got := bootstrap.CurrentUser.UserID; got != "user-1" {
					t.Fatalf("auth bootstrap current user ID = %q, want user-1", got)
				}
				if got := bootstrap.Branding["productName"]; got != "OpenSoha" {
					t.Fatalf("auth bootstrap branding product name = %v, want OpenSoha", got)
				}
				return nil
			},
			body: map[string]any{
				"data": map[string]any{
					"branding":           map[string]any{"productName": "OpenSoha"},
					"currentUser":        principalJSON("user-1"),
					"permissionSnapshot": map[string]any{"allowed": []string{"auth.read"}},
					"user":               principalJSON("user-1"),
				},
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				assertCommonRequest(t, r, http.MethodGet, tt.path)
				writeJSON(t, w, tt.body)
			})

			if err := tt.call(t, client); err != nil {
				t.Fatalf("%s returned error: %v", tt.name, err)
			}
		})
	}
}

func TestLogoutAuthSessionPostsBearerBodyAndAcceptsNoContent(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assertCommonRequest(t, r, http.MethodPost, "/api/v1/auth/logout")
		body := decodeJSONRequestBody[RefreshRequest](t, r)
		if got := body.RefreshToken; got != "refresh-logout" {
			t.Fatalf("logout refresh token = %q, want refresh-logout", got)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	if err := client.LogoutAuthSession(context.Background(), &RefreshRequest{RefreshToken: "refresh-logout"}); err != nil {
		t.Fatalf("LogoutAuthSession returned error: %v", err)
	}
}

func TestIssueStreamTicketPostsBearerBodyAndDecodesData(t *testing.T) {
	expiresAt := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assertCommonRequest(t, r, http.MethodPost, "/api/v1/auth/stream-ticket")
		body := decodeJSONRequestBody[StreamTicketRequest](t, r)
		if got := body.Path; got != "/api/v1/events/stream" {
			t.Fatalf("stream ticket path = %q, want /api/v1/events/stream", got)
		}
		writeJSON(t, w, map[string]any{
			"data": map[string]any{
				"ticket":    "ticket-1",
				"expiresAt": expiresAt,
			},
		})
	})

	ticket, err := client.IssueStreamTicket(context.Background(), StreamTicketRequest{Path: "/api/v1/events/stream"})
	if err != nil {
		t.Fatalf("IssueStreamTicket returned error: %v", err)
	}
	if got := ticket.Ticket; got != "ticket-1" {
		t.Fatalf("stream ticket = %q, want ticket-1", got)
	}
	if got := ticket.ExpiresAt; !got.Equal(expiresAt) {
		t.Fatalf("stream ticket ExpiresAt = %s, want %s", got, expiresAt)
	}
}

func TestListAIGatewayAuditLogsBuildsRequestAndDecodesItems(t *testing.T) {
	from := time.Date(2026, 6, 10, 8, 9, 10, 123000000, time.UTC)
	to := time.Date(2026, 6, 10, 9, 9, 10, 0, time.UTC)
	createdAt := time.Date(2026, 6, 10, 8, 30, 0, 0, time.UTC)
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assertCommonRequest(t, r, http.MethodGet, "/api/v1/ai-gateway/audit-logs")
		assertQuery(t, r, map[string]string{
			"action":            "tool.invoke",
			"actorId":           "user 1",
			"actorType":         "user",
			"aiClientId":        "client-1",
			"approvalRequestId": "approval-1",
			"from":              from.Format(time.RFC3339Nano),
			"limit":             "50",
			"result":            "success",
			"riskLevel":         "high",
			"skillId":           "skill-1",
			"to":                to.Format(time.RFC3339Nano),
			"toolName":          "deploy.apply",
		})
		writeJSON(t, w, map[string]any{
			"items": []map[string]any{
				{
					"id":           "audit-1",
					"action":       "tool.invoke",
					"actorId":      "user-1",
					"actorType":    "user",
					"createdAt":    createdAt,
					"result":       "success",
					"riskLevel":    "high",
					"summary":      "tool invocation allowed",
					"toolName":     "deploy.apply",
					"aiClientId":   "client-1",
					"aiClientName": "CLI",
				},
			},
		})
	})

	items, err := client.ListAIGatewayAuditLogs(context.Background(), ListAIGatewayAuditLogsParams{
		Action:            "tool.invoke",
		ActorID:           "user 1",
		ActorType:         "user",
		AIClientID:        "client-1",
		ApprovalRequestID: "approval-1",
		From:              from,
		Limit:             50,
		Result:            "success",
		RiskLevel:         "high",
		SkillID:           "skill-1",
		To:                to,
		ToolName:          "deploy.apply",
	})
	if err != nil {
		t.Fatalf("ListAIGatewayAuditLogs returned error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d audit logs, want 1", len(items))
	}
	if got := items[0].ID; got != "audit-1" {
		t.Fatalf("audit log ID = %q, want audit-1", got)
	}
	if got := items[0].CreatedAt; !got.Equal(createdAt) {
		t.Fatalf("audit log CreatedAt = %s, want %s", got, createdAt)
	}
}

func TestListAIGatewayApprovalRequestsBuildsRequestAndDecodesItems(t *testing.T) {
	from := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)
	createdAt := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 6, 1, 10, 5, 0, 0, time.UTC)
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assertCommonRequest(t, r, http.MethodGet, "/api/v1/ai-gateway/approval-requests")
		assertQuery(t, r, map[string]string{
			"actorId":    "user-1",
			"actorType":  "user",
			"aiClientId": "client-1",
			"from":       from.Format(time.RFC3339Nano),
			"id":         "approval-1",
			"limit":      "25",
			"riskLevel":  "critical",
			"skillId":    "skill-1",
			"status":     "pending",
			"strategy":   "multi-stage",
			"to":         to.Format(time.RFC3339Nano),
			"toolName":   "deploy.apply",
		})
		writeJSON(t, w, map[string]any{
			"items": []map[string]any{
				{
					"id":               "approval-1",
					"actorId":          "user-1",
					"actorType":        "user",
					"createdAt":        createdAt,
					"requiresApproval": true,
					"riskLevel":        "critical",
					"status":           "pending",
					"strategy":         "multi-stage",
					"summary":          "deployment requires approval",
					"toolName":         "deploy.apply",
					"updatedAt":        updatedAt,
				},
			},
		})
	})

	items, err := client.ListAIGatewayApprovalRequests(context.Background(), ListAIGatewayApprovalRequestsParams{
		ActorID:    "user-1",
		ActorType:  "user",
		AIClientID: "client-1",
		From:       from,
		ID:         "approval-1",
		Limit:      25,
		RiskLevel:  "critical",
		SkillID:    "skill-1",
		Status:     "pending",
		Strategy:   "multi-stage",
		To:         to,
		ToolName:   "deploy.apply",
	})
	if err != nil {
		t.Fatalf("ListAIGatewayApprovalRequests returned error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d approval requests, want 1", len(items))
	}
	if got := items[0].Status; got != "pending" {
		t.Fatalf("approval status = %q, want pending", got)
	}
	if got := items[0].RiskLevel; got != "critical" {
		t.Fatalf("approval risk level = %q, want critical", got)
	}
}

func TestGetAIGatewayApprovalTimelineEscapesPathAndDecodesData(t *testing.T) {
	createdAt := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assertCommonRequest(t, r, http.MethodGet, "/api/v1/ai-gateway/approval-requests/request%2F1/timeline")
		writeJSON(t, w, map[string]any{
			"data": map[string]any{
				"request": map[string]any{
					"id":               "request/1",
					"actorId":          "user-1",
					"actorType":        "user",
					"createdAt":        createdAt,
					"requiresApproval": true,
					"riskLevel":        "high",
					"status":           "pending",
					"strategy":         "manual",
					"summary":          "manual approval",
					"toolName":         "deploy.apply",
					"updatedAt":        createdAt,
				},
				"events": []map[string]any{
					{
						"id":        "event-1",
						"action":    "created",
						"createdAt": createdAt,
						"kind":      "approval",
						"result":    "pending",
					},
				},
			},
		})
	})

	timeline, err := client.GetAIGatewayApprovalTimeline(context.Background(), "request/1")
	if err != nil {
		t.Fatalf("GetAIGatewayApprovalTimeline returned error: %v", err)
	}
	if got := timeline.Request.ID; got != "request/1" {
		t.Fatalf("timeline request ID = %q, want request/1", got)
	}
	if len(timeline.Events) != 1 {
		t.Fatalf("got %d timeline events, want 1", len(timeline.Events))
	}
}

func TestDecideAIGatewayApprovalRequestPostsBodyAndDecodesData(t *testing.T) {
	decidedAt := time.Date(2026, 6, 4, 13, 0, 0, 0, time.UTC)
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assertCommonRequest(t, r, http.MethodPost, "/api/v1/ai-gateway/approval-requests/request%2F1/approve")
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("Content-Type = %q, want application/json", got)
		}
		var body ApprovalDecisionInput
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("decode request body %q: %v", string(raw), err)
		}
		if got := body.Comment; got != "ship it" {
			t.Fatalf("decision comment = %q, want ship it", got)
		}
		writeJSON(t, w, map[string]any{
			"data": map[string]any{
				"request": map[string]any{
					"id":               "request/1",
					"actorId":          "user-1",
					"actorType":        "user",
					"createdAt":        decidedAt,
					"decidedAt":        decidedAt,
					"decisionComment":  "ship it",
					"requiresApproval": true,
					"riskLevel":        "high",
					"status":           "approved",
					"strategy":         "manual",
					"summary":          "manual approval",
					"toolName":         "deploy.apply",
					"updatedAt":        decidedAt,
				},
				"invocation": map[string]any{
					"requiresApproval": false,
					"result":           "success",
					"riskLevel":        "high",
					"toolName":         "deploy.apply",
				},
			},
		})
	})

	result, err := client.DecideAIGatewayApprovalRequest(context.Background(), "request/1", Approve, &ApprovalDecisionInput{Comment: "ship it"})
	if err != nil {
		t.Fatalf("DecideAIGatewayApprovalRequest returned error: %v", err)
	}
	if got := result.Request.Status; got != "approved" {
		t.Fatalf("decision request status = %q, want approved", got)
	}
	if result.Invocation == nil {
		t.Fatal("decision invocation is nil, want decoded invocation")
	}
	if got := result.Invocation.Result; got != "success" {
		t.Fatalf("decision invocation result = %q, want success", got)
	}
}

func TestGetAIGatewayGovernanceStatusBuildsRequestAndDecodesData(t *testing.T) {
	generatedAt := time.Date(2026, 6, 5, 14, 0, 0, 0, time.UTC)
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assertCommonRequest(t, r, http.MethodGet, "/api/v1/ai-gateway/governance/status")
		assertQuery(t, r, map[string]string{"windowHours": "24"})
		writeJSON(t, w, map[string]any{
			"data": map[string]any{
				"generatedAt": generatedAt,
				"windowHours": 24,
				"health":      map[string]any{"status": "healthy", "message": "all checks passed"},
				"approvals":   map[string]any{"pending": 2, "overdue": 1, "stalePending": 1, "dueSoon": 1},
				"clients":     map[string]any{"total": 3, "active": 2, "disabled": 1, "pendingApproval": 0, "registrationApproval": "manual"},
				"metrics":     map[string]any{"totalCalls": 10, "successCount": 8, "failureCount": 1, "denyCount": 1, "dryRunCount": 0, "pendingApprovalCount": 2},
				"redaction":   map[string]any{"totalMatches": 1, "auditsWithRedaction": 1, "fieldMatches": 1, "inputAudits": 1, "outputAudits": 0, "secretClassifierMatches": 0, "sensitiveKeyMatches": 1, "sensitiveTextMatches": 0, "structuredSecretMatches": 0, "valuePatternMatches": 0},
				"policyCoverage": map[string]any{
					"accessPolicies": 1, "activeAccessPolicies": 1, "activeSkillBindings": 1, "activeToolGrants": 1, "budgetPolicies": 1,
					"budgetState": "enabled", "rateLimitPolicies": 1, "rateLimitState": "enabled", "redactionPolicies": 1, "redactionPolicyState": "enabled",
					"resourceScopeState": "enabled", "resourceScopedAccessPolicies": 1, "resourceScopedToolGrants": 1, "skillBindings": 1, "toolGrants": 1,
				},
				"tokens": map[string]any{
					"lastUsedTrackingState": "enabled",
					"personalAccessTokens":  map[string]any{"total": 2, "active": 1, "revoked": 1, "expired": 0, "expiringSoon": 0, "neverUsed": 1, "stale": 0},
					"serviceAccountTokens":  map[string]any{"total": 1, "active": 1, "revoked": 0, "expired": 0, "expiringSoon": 0, "neverUsed": 0, "stale": 0},
				},
			},
		})
	})

	status, err := client.GetAIGatewayGovernanceStatus(context.Background(), GetAIGatewayGovernanceStatusParams{WindowHours: 24})
	if err != nil {
		t.Fatalf("GetAIGatewayGovernanceStatus returned error: %v", err)
	}
	if got := status.WindowHours; got != 24 {
		t.Fatalf("window hours = %d, want 24", got)
	}
	if got := status.Health.Status; got != "healthy" {
		t.Fatalf("health status = %q, want healthy", got)
	}
	if got := status.Tokens.PersonalAccessTokens.Total; got != 2 {
		t.Fatalf("personal access token count = %d, want 2", got)
	}
}

func TestListPersonalAccessTokensBuildsRequestAndDecodesItems(t *testing.T) {
	createdAt := time.Date(2026, 6, 6, 8, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 6, 6, 8, 1, 0, 0, time.UTC)
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assertCommonRequest(t, r, http.MethodGet, "/api/v1/ai-gateway/personal-access-tokens")
		assertQuery(t, r, map[string]string{
			"scope":  "ai:gateway",
			"userId": "user-1",
		})
		writeJSON(t, w, map[string]any{
			"items": []map[string]any{
				{
					"id":             "pat-1",
					"userId":         "user-1",
					"name":           "CLI",
					"tokenPrefix":    "soha_pat_1234",
					"scopes":         []string{"ai:gateway"},
					"permissionKeys": []string{"ai_gateway.audit.read"},
					"createdBy":      "user-1",
					"createdAt":      createdAt,
					"updatedAt":      updatedAt,
				},
			},
		})
	})

	items, err := client.ListPersonalAccessTokens(context.Background(), ListPersonalAccessTokensParams{
		Scope:  "ai:gateway",
		UserID: "user-1",
	})
	if err != nil {
		t.Fatalf("ListPersonalAccessTokens returned error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d personal access tokens, want 1", len(items))
	}
	if got := items[0].ID; got != "pat-1" {
		t.Fatalf("personal access token ID = %q, want pat-1", got)
	}
	if got := items[0].UpdatedAt; !got.Equal(updatedAt) {
		t.Fatalf("personal access token UpdatedAt = %s, want %s", got, updatedAt)
	}
}

func TestCreatePersonalAccessTokenPostsBodyAndDecodesData(t *testing.T) {
	expiresAt := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	createdAt := time.Date(2026, 6, 6, 9, 0, 0, 0, time.UTC)
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assertCommonRequest(t, r, http.MethodPost, "/api/v1/ai-gateway/personal-access-tokens")
		body := decodeJSONRequestBody[PersonalAccessTokenInput](t, r)
		if got := body.Name; got != "release bot" {
			t.Fatalf("personal access token name = %q, want release bot", got)
		}
		if body.ExpiresAt == nil || !body.ExpiresAt.Equal(expiresAt) {
			t.Fatalf("personal access token ExpiresAt = %v, want %s", body.ExpiresAt, expiresAt)
		}
		if got := body.Metadata["purpose"]; got != "release" {
			t.Fatalf("personal access token metadata purpose = %v, want release", got)
		}
		writeJSON(t, w, map[string]any{
			"data": map[string]any{
				"value": "soha_pat_secret",
				"token": map[string]any{
					"id":             "pat-2",
					"userId":         "user-1",
					"name":           "release bot",
					"tokenPrefix":    "soha_pat_5678",
					"scopes":         []string{"ai:gateway"},
					"permissionKeys": []string{"ai_gateway.tokens.write"},
					"expiresAt":      expiresAt,
					"createdBy":      "user-1",
					"createdAt":      createdAt,
					"updatedAt":      createdAt,
				},
			},
		})
	})

	result, err := client.CreatePersonalAccessToken(context.Background(), PersonalAccessTokenInput{
		Name:           "release bot",
		Scopes:         []string{"ai:gateway"},
		PermissionKeys: []string{"ai_gateway.tokens.write"},
		Metadata:       map[string]any{"purpose": "release"},
		ExpiresAt:      &expiresAt,
	})
	if err != nil {
		t.Fatalf("CreatePersonalAccessToken returned error: %v", err)
	}
	if got := result.Value; got != "soha_pat_secret" {
		t.Fatalf("created personal access token value = %q, want soha_pat_secret", got)
	}
	if got := result.Token.ID; got != "pat-2" {
		t.Fatalf("created personal access token ID = %q, want pat-2", got)
	}
	if result.Token.ExpiresAt == nil || !result.Token.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("created personal access token ExpiresAt = %v, want %s", result.Token.ExpiresAt, expiresAt)
	}
}

func TestRevokePersonalAccessTokenEscapesPathAndDecodesStatus(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assertCommonRequest(t, r, http.MethodPost, "/api/v1/ai-gateway/personal-access-tokens/pat%2F1/revoke")
		writeJSON(t, w, map[string]any{"status": "revoked"})
	})

	status, err := client.RevokePersonalAccessToken(context.Background(), "pat/1")
	if err != nil {
		t.Fatalf("RevokePersonalAccessToken returned error: %v", err)
	}
	if got := status.Status; got != "revoked" {
		t.Fatalf("personal access token revoke status = %q, want revoked", got)
	}
}

func TestRotatePersonalAccessTokenEscapesPathPostsBodyAndDecodesData(t *testing.T) {
	expiresAt := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	rotatedAt := time.Date(2026, 6, 6, 10, 0, 0, 0, time.UTC)
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assertCommonRequest(t, r, http.MethodPost, "/api/v1/ai-gateway/personal-access-tokens/pat%2F1/rotate")
		body := decodeJSONRequestBody[TokenRotationInput](t, r)
		if body.ExpiresAt == nil || !body.ExpiresAt.Equal(expiresAt) {
			t.Fatalf("personal access token rotation ExpiresAt = %v, want %s", body.ExpiresAt, expiresAt)
		}
		writeJSON(t, w, map[string]any{
			"data": map[string]any{
				"value": "soha_pat_rotated",
				"token": map[string]any{
					"id":             "pat-rotated",
					"userId":         "user-1",
					"name":           "rotated",
					"tokenPrefix":    "soha_pat_9999",
					"scopes":         []string{"ai:gateway"},
					"permissionKeys": []string{"ai_gateway.tokens.write"},
					"expiresAt":      expiresAt,
					"createdBy":      "user-1",
					"createdAt":      rotatedAt,
					"updatedAt":      rotatedAt,
				},
			},
		})
	})

	result, err := client.RotatePersonalAccessToken(context.Background(), "pat/1", &TokenRotationInput{ExpiresAt: &expiresAt})
	if err != nil {
		t.Fatalf("RotatePersonalAccessToken returned error: %v", err)
	}
	if got := result.Value; got != "soha_pat_rotated" {
		t.Fatalf("rotated personal access token value = %q, want soha_pat_rotated", got)
	}
	if got := result.Token.ID; got != "pat-rotated" {
		t.Fatalf("rotated personal access token ID = %q, want pat-rotated", got)
	}
}

func TestListServiceAccountsBuildsRequestAndDecodesItems(t *testing.T) {
	createdAt := time.Date(2026, 6, 7, 8, 0, 0, 0, time.UTC)
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assertCommonRequest(t, r, http.MethodGet, "/api/v1/ai-gateway/service-accounts")
		assertQuery(t, r, map[string]string{})
		writeJSON(t, w, map[string]any{
			"items": []map[string]any{
				{
					"id":            "svc-1",
					"name":          "deploy service",
					"description":   "automation",
					"status":        "active",
					"roleIds":       []string{"runner"},
					"teamIds":       []string{"platform"},
					"scopeGrantIds": []string{"gateway"},
					"createdBy":     "user-1",
					"createdAt":     createdAt,
					"updatedAt":     createdAt,
				},
			},
		})
	})

	items, err := client.ListServiceAccounts(context.Background())
	if err != nil {
		t.Fatalf("ListServiceAccounts returned error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d service accounts, want 1", len(items))
	}
	if got := items[0].ID; got != "svc-1" {
		t.Fatalf("service account ID = %q, want svc-1", got)
	}
	if got := items[0].RoleIDs[0]; got != "runner" {
		t.Fatalf("service account role ID = %q, want runner", got)
	}
}

func TestCreateServiceAccountPostsBodyAndDecodesData(t *testing.T) {
	createdAt := time.Date(2026, 6, 7, 9, 0, 0, 0, time.UTC)
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assertCommonRequest(t, r, http.MethodPost, "/api/v1/ai-gateway/service-accounts")
		body := decodeJSONRequestBody[ServiceAccountInput](t, r)
		if got := body.ID; got != "svc-create" {
			t.Fatalf("service account input ID = %q, want svc-create", got)
		}
		if got := body.Metadata["owner"]; got != "platform" {
			t.Fatalf("service account metadata owner = %v, want platform", got)
		}
		writeJSON(t, w, map[string]any{
			"data": map[string]any{
				"id":            "svc-create",
				"name":          "created service",
				"description":   "created by test",
				"status":        "active",
				"ownerUserId":   "user-1",
				"roleIds":       []string{"runner"},
				"teamIds":       []string{"platform"},
				"scopeGrantIds": []string{"gateway"},
				"metadata":      map[string]any{"owner": "platform"},
				"createdBy":     "user-1",
				"createdAt":     createdAt,
				"updatedAt":     createdAt,
			},
		})
	})

	result, err := client.CreateServiceAccount(context.Background(), ServiceAccountInput{
		ID:            "svc-create",
		Name:          "created service",
		Description:   "created by test",
		Status:        "active",
		OwnerUserID:   "user-1",
		RoleIDs:       []string{"runner"},
		TeamIDs:       []string{"platform"},
		ScopeGrantIDs: []string{"gateway"},
		Metadata:      map[string]any{"owner": "platform"},
	})
	if err != nil {
		t.Fatalf("CreateServiceAccount returned error: %v", err)
	}
	if got := result.ID; got != "svc-create" {
		t.Fatalf("created service account ID = %q, want svc-create", got)
	}
	if got := result.OwnerUserID; got != "user-1" {
		t.Fatalf("created service account owner = %q, want user-1", got)
	}
}

func TestListServiceAccountTokensBuildsRequestAndDecodesItems(t *testing.T) {
	createdAt := time.Date(2026, 6, 7, 10, 0, 0, 0, time.UTC)
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assertCommonRequest(t, r, http.MethodGet, "/api/v1/ai-gateway/service-account-tokens")
		assertQuery(t, r, map[string]string{})
		writeJSON(t, w, map[string]any{
			"items": []map[string]any{
				{
					"id":               "sat-1",
					"serviceAccountId": "svc-1",
					"name":             "deploy token",
					"tokenPrefix":      "soha_sat_1234",
					"scopes":           []string{"ai:gateway"},
					"permissionKeys":   []string{"ai_gateway.tools.invoke"},
					"createdBy":        "user-1",
					"createdAt":        createdAt,
					"updatedAt":        createdAt,
				},
			},
		})
	})

	items, err := client.ListServiceAccountTokens(context.Background())
	if err != nil {
		t.Fatalf("ListServiceAccountTokens returned error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d service account tokens, want 1", len(items))
	}
	if got := items[0].ServiceAccountID; got != "svc-1" {
		t.Fatalf("service account token service account ID = %q, want svc-1", got)
	}
}

func TestCreateServiceAccountTokenEscapesPathPostsBodyAndDecodesData(t *testing.T) {
	expiresAt := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	createdAt := time.Date(2026, 6, 7, 11, 0, 0, 0, time.UTC)
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assertCommonRequest(t, r, http.MethodPost, "/api/v1/ai-gateway/service-accounts/svc%2F1/tokens")
		body := decodeJSONRequestBody[ServiceAccountTokenInput](t, r)
		if got := body.Name; got != "deploy token" {
			t.Fatalf("service account token name = %q, want deploy token", got)
		}
		if body.ExpiresAt == nil || !body.ExpiresAt.Equal(expiresAt) {
			t.Fatalf("service account token ExpiresAt = %v, want %s", body.ExpiresAt, expiresAt)
		}
		writeJSON(t, w, map[string]any{
			"data": map[string]any{
				"value": "soha_sat_secret",
				"token": map[string]any{ // #nosec G101 -- Synthetic token metadata in an httptest response, not a usable credential.
					"id":               "sat-2",
					"serviceAccountId": "svc/1",
					"name":             "deploy token",
					"tokenPrefix":      "soha_sat_5678",
					"scopes":           []string{"ai:gateway"},
					"permissionKeys":   []string{"ai_gateway.tools.invoke"},
					"expiresAt":        expiresAt,
					"createdBy":        "user-1",
					"createdAt":        createdAt,
					"updatedAt":        createdAt,
				},
			},
		})
	})

	result, err := client.CreateServiceAccountToken(context.Background(), "svc/1", ServiceAccountTokenInput{
		Name:           "deploy token",
		Scopes:         []string{"ai:gateway"},
		PermissionKeys: []string{"ai_gateway.tools.invoke"},
		ExpiresAt:      &expiresAt,
	})
	if err != nil {
		t.Fatalf("CreateServiceAccountToken returned error: %v", err)
	}
	if got := result.Value; got != "soha_sat_secret" {
		t.Fatalf("created service account token value = %q, want soha_sat_secret", got)
	}
	if got := result.Token.ServiceAccountID; got != "svc/1" {
		t.Fatalf("created service account token service account ID = %q, want svc/1", got)
	}
}

func TestRevokeServiceAccountTokenEscapesPathAndDecodesStatus(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assertCommonRequest(t, r, http.MethodPost, "/api/v1/ai-gateway/service-account-tokens/sat%2F1/revoke")
		writeJSON(t, w, map[string]any{"status": "revoked"})
	})

	status, err := client.RevokeServiceAccountToken(context.Background(), "sat/1")
	if err != nil {
		t.Fatalf("RevokeServiceAccountToken returned error: %v", err)
	}
	if got := status.Status; got != "revoked" {
		t.Fatalf("service account token revoke status = %q, want revoked", got)
	}
}

func TestRotateServiceAccountTokenEscapesPathPostsBodyAndDecodesData(t *testing.T) {
	expiresAt := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	rotatedAt := time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC)
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assertCommonRequest(t, r, http.MethodPost, "/api/v1/ai-gateway/service-account-tokens/sat%2F1/rotate")
		body := decodeJSONRequestBody[TokenRotationInput](t, r)
		if body.ExpiresAt == nil || !body.ExpiresAt.Equal(expiresAt) {
			t.Fatalf("service account token rotation ExpiresAt = %v, want %s", body.ExpiresAt, expiresAt)
		}
		writeJSON(t, w, map[string]any{
			"data": map[string]any{
				"value": "soha_sat_rotated",
				"token": map[string]any{ // #nosec G101 -- Synthetic token metadata in an httptest response, not a usable credential.
					"id":               "sat-rotated",
					"serviceAccountId": "svc-1",
					"name":             "rotated service token",
					"tokenPrefix":      "soha_sat_9999",
					"scopes":           []string{"ai:gateway"},
					"permissionKeys":   []string{"ai_gateway.tools.invoke"},
					"expiresAt":        expiresAt,
					"createdBy":        "user-1",
					"createdAt":        rotatedAt,
					"updatedAt":        rotatedAt,
				},
			},
		})
	})

	result, err := client.RotateServiceAccountToken(context.Background(), "sat/1", &TokenRotationInput{ExpiresAt: &expiresAt})
	if err != nil {
		t.Fatalf("RotateServiceAccountToken returned error: %v", err)
	}
	if got := result.Value; got != "soha_sat_rotated" {
		t.Fatalf("rotated service account token value = %q, want soha_sat_rotated", got)
	}
	if got := result.Token.ID; got != "sat-rotated" {
		t.Fatalf("rotated service account token ID = %q, want sat-rotated", got)
	}
}

func TestListMCPCapabilitiesBuildsRequestAndDecodesItems(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assertCommonRequest(t, r, http.MethodGet, "/api/v1/mcp/capabilities")
		assertQuery(t, r, map[string]string{})
		writeJSON(t, w, map[string]any{
			"items": []map[string]any{
				{
					"adapterID":   "filesystem",
					"name":        "resource.read",
					"description": "read files through the gateway",
					"scopes":      []string{"resources:read"},
				},
			},
		})
	})

	items, err := client.ListMCPCapabilities(context.Background())
	if err != nil {
		t.Fatalf("ListMCPCapabilities returned error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d MCP capabilities, want 1", len(items))
	}
	if got := items[0].AdapterID; got != "filesystem" {
		t.Fatalf("MCP capability adapter ID = %q, want filesystem", got)
	}
	if got := items[0].Scopes[0]; got != "resources:read" {
		t.Fatalf("MCP capability scope = %q, want resources:read", got)
	}
}

func TestListMarketplacePluginsBuildsRequestAndDecodesItems(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assertCommonRequest(t, r, http.MethodGet, "/api/v1/plugins/marketplace")
		assertQuery(t, r, map[string]string{
			"publisher": "OpenSoha",
			"q":         "k8s sre",
			"type":      "skill-pack",
		})
		writeJSON(t, w, map[string]any{
			"items": []map[string]any{
				marketplacePluginJSON("k8s-sre", true),
			},
		})
	})

	items, err := client.ListMarketplacePlugins(context.Background(), ListMarketplacePluginsParams{
		Publisher: "OpenSoha",
		Q:         "k8s sre",
		Type:      "skill-pack",
	})
	if err != nil {
		t.Fatalf("ListMarketplacePlugins returned error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d marketplace plugins, want 1", len(items))
	}
	if got := items[0].ID; got != "k8s-sre" {
		t.Fatalf("marketplace plugin ID = %q, want k8s-sre", got)
	}
	if got := items[0].Manifest.Capabilities.Tools[0]; got != "k8s.rollout" {
		t.Fatalf("marketplace plugin tool = %q, want k8s.rollout", got)
	}
	if !items[0].Installed {
		t.Fatal("marketplace plugin Installed = false, want true")
	}
}

func TestGetMarketplacePluginEscapesPathAndDecodesData(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assertCommonRequest(t, r, http.MethodGet, "/api/v1/plugins/marketplace/plugin%2F1")
		writeJSON(t, w, map[string]any{
			"data": marketplacePluginJSON("plugin/1", false),
		})
	})

	plugin, err := client.GetMarketplacePlugin(context.Background(), "plugin/1")
	if err != nil {
		t.Fatalf("GetMarketplacePlugin returned error: %v", err)
	}
	if got := plugin.ID; got != "plugin/1" {
		t.Fatalf("marketplace plugin ID = %q, want plugin/1", got)
	}
	if got := plugin.Source; got != "marketplace" {
		t.Fatalf("marketplace plugin source = %q, want marketplace", got)
	}
}

func TestListInstalledPluginsBuildsRequestAndDecodesItems(t *testing.T) {
	installedAt := time.Date(2026, 6, 8, 9, 0, 0, 0, time.UTC)
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assertCommonRequest(t, r, http.MethodGet, "/api/v1/plugins/installed")
		assertQuery(t, r, map[string]string{})
		writeJSON(t, w, map[string]any{
			"items": []map[string]any{
				installedPluginJSON("k8s-sre", "enabled", installedAt),
			},
		})
	})

	items, err := client.ListInstalledPlugins(context.Background())
	if err != nil {
		t.Fatalf("ListInstalledPlugins returned error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d installed plugins, want 1", len(items))
	}
	if got := items[0].Status; got != "enabled" {
		t.Fatalf("installed plugin status = %q, want enabled", got)
	}
	if got := items[0].InstalledAt; !got.Equal(installedAt) {
		t.Fatalf("installed plugin InstalledAt = %s, want %s", got, installedAt)
	}
}

func TestInstallPluginPostsBodyAndDecodesData(t *testing.T) {
	installedAt := time.Date(2026, 6, 8, 10, 0, 0, 0, time.UTC)
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assertCommonRequest(t, r, http.MethodPost, "/api/v1/plugins/install")
		body := decodeJSONRequestBody[PluginInstallRequest](t, r)
		if got := body.PluginID; got != "k8s-sre" {
			t.Fatalf("plugin install PluginID = %q, want k8s-sre", got)
		}
		if got := body.Source; got != "marketplace" {
			t.Fatalf("plugin install Source = %q, want marketplace", got)
		}
		if got := body.ExpectedChecksum; got != "sha256:abc123" {
			t.Fatalf("plugin install ExpectedChecksum = %q, want sha256:abc123", got)
		}
		if !body.Enable {
			t.Fatal("plugin install Enable = false, want true")
		}
		if body.Manifest == nil || body.Manifest.ID != "custom-plugin" {
			t.Fatalf("plugin install Manifest ID = %v, want custom-plugin", body.Manifest)
		}
		writeJSON(t, w, map[string]any{
			"data": installedPluginJSON("k8s-sre", "enabled", installedAt),
		})
	})

	plugin, err := client.InstallPlugin(context.Background(), PluginInstallRequest{
		Enable:           true,
		ExpectedChecksum: "sha256:abc123",
		Manifest: &PluginManifest{
			ID:        "custom-plugin",
			Name:      "Custom Plugin",
			Publisher: "OpenSoha",
			Type:      "skill-pack",
			Version:   "1.0.0",
		},
		PluginID: "k8s-sre",
		Source:   "marketplace",
	})
	if err != nil {
		t.Fatalf("InstallPlugin returned error: %v", err)
	}
	if got := plugin.ID; got != "k8s-sre" {
		t.Fatalf("installed plugin ID = %q, want k8s-sre", got)
	}
	if got := plugin.Manifest.ID; got != "k8s-sre" {
		t.Fatalf("installed plugin manifest ID = %q, want k8s-sre", got)
	}
}

func TestGetInstalledPluginEscapesPathAndDecodesData(t *testing.T) {
	installedAt := time.Date(2026, 6, 8, 11, 0, 0, 0, time.UTC)
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assertCommonRequest(t, r, http.MethodGet, "/api/v1/plugins/plugin%2F1")
		writeJSON(t, w, map[string]any{
			"data": installedPluginJSON("plugin/1", "enabled", installedAt),
		})
	})

	plugin, err := client.GetInstalledPlugin(context.Background(), "plugin/1")
	if err != nil {
		t.Fatalf("GetInstalledPlugin returned error: %v", err)
	}
	if got := plugin.ID; got != "plugin/1" {
		t.Fatalf("installed plugin ID = %q, want plugin/1", got)
	}
	if got := plugin.Type; got != "skill-pack" {
		t.Fatalf("installed plugin type = %q, want skill-pack", got)
	}
}

func TestRemoveInstalledPluginEscapesPathAndDecodesStatus(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assertCommonRequest(t, r, http.MethodDelete, "/api/v1/plugins/plugin%2F1")
		writeJSON(t, w, map[string]any{"status": "removed"})
	})

	status, err := client.RemoveInstalledPlugin(context.Background(), "plugin/1")
	if err != nil {
		t.Fatalf("RemoveInstalledPlugin returned error: %v", err)
	}
	if got := status.Status; got != "removed" {
		t.Fatalf("remove installed plugin status = %q, want removed", got)
	}
}

func TestGetInstalledPluginManifestEscapesPathAndDecodesData(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assertCommonRequest(t, r, http.MethodGet, "/api/v1/plugins/plugin%2F1/manifest")
		writeJSON(t, w, map[string]any{
			"data": pluginManifestJSON("plugin/1"),
		})
	})

	manifest, err := client.GetInstalledPluginManifest(context.Background(), "plugin/1")
	if err != nil {
		t.Fatalf("GetInstalledPluginManifest returned error: %v", err)
	}
	if got := manifest.ID; got != "plugin/1" {
		t.Fatalf("installed plugin manifest ID = %q, want plugin/1", got)
	}
	if got := manifest.Permissions.Required[0]; got != "mcp.capabilities.read" {
		t.Fatalf("installed plugin manifest permission = %q, want mcp.capabilities.read", got)
	}
}

func TestInstalledPluginStateMethodsEscapePathAndDecodeData(t *testing.T) {
	cases := []struct {
		name   string
		path   string
		status string
		call   func(*Client) (InstalledPlugin, error)
	}{
		{
			name:   "enable",
			path:   "/api/v1/plugins/plugin%2F1/enable",
			status: "enabled",
			call: func(client *Client) (InstalledPlugin, error) {
				return client.EnableInstalledPlugin(context.Background(), "plugin/1")
			},
		},
		{
			name:   "disable",
			path:   "/api/v1/plugins/plugin%2F1/disable",
			status: "disabled",
			call: func(client *Client) (InstalledPlugin, error) {
				return client.DisableInstalledPlugin(context.Background(), "plugin/1")
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			updatedAt := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				assertCommonRequest(t, r, http.MethodPost, tt.path)
				writeJSON(t, w, map[string]any{
					"data": installedPluginJSON("plugin/1", tt.status, updatedAt),
				})
			})

			plugin, err := tt.call(client)
			if err != nil {
				t.Fatalf("%s installed plugin returned error: %v", tt.name, err)
			}
			if got := plugin.Status; got != tt.status {
				t.Fatalf("%s installed plugin status = %q, want %s", tt.name, got, tt.status)
			}
		})
	}
}

func TestUpgradeInstalledPluginEscapesPathPostsBodyAndDecodesData(t *testing.T) {
	updatedAt := time.Date(2026, 6, 8, 13, 0, 0, 0, time.UTC)
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assertCommonRequest(t, r, http.MethodPost, "/api/v1/plugins/plugin%2F1/upgrade")
		body := decodeJSONRequestBody[PluginInstallRequest](t, r)
		if got := body.Source; got != "marketplace" {
			t.Fatalf("plugin upgrade Source = %q, want marketplace", got)
		}
		if got := body.ExpectedChecksum; got != "sha256:def456" {
			t.Fatalf("plugin upgrade ExpectedChecksum = %q, want sha256:def456", got)
		}
		writeJSON(t, w, map[string]any{
			"data": installedPluginJSON("plugin/1", "enabled", updatedAt),
		})
	})

	plugin, err := client.UpgradeInstalledPlugin(context.Background(), "plugin/1", &PluginInstallRequest{
		ExpectedChecksum: "sha256:def456",
		Source:           "marketplace",
	})
	if err != nil {
		t.Fatalf("UpgradeInstalledPlugin returned error: %v", err)
	}
	if got := plugin.ID; got != "plugin/1" {
		t.Fatalf("upgraded installed plugin ID = %q, want plugin/1", got)
	}
}

func TestConfigureInstalledPluginEscapesPathPostsBodyAndDecodesData(t *testing.T) {
	enabled := false
	updatedAt := time.Date(2026, 6, 8, 14, 0, 0, 0, time.UTC)
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assertCommonRequest(t, r, http.MethodPut, "/api/v1/plugins/plugin%2F1/config")
		body := decodeJSONRequestBody[PluginConfigRequest](t, r)
		if body.Enabled == nil || *body.Enabled {
			t.Fatalf("plugin config Enabled = %v, want false", body.Enabled)
		}
		if got := body.Metadata["env"]; got != "prod" {
			t.Fatalf("plugin config metadata env = %v, want prod", got)
		}
		if got := body.SecretRefs["apiToken"]; got != "secret/ref" {
			t.Fatalf("plugin config secret ref = %q, want secret/ref", got)
		}
		writeJSON(t, w, map[string]any{
			"data": installedPluginJSON("plugin/1", "disabled", updatedAt),
		})
	})

	plugin, err := client.ConfigureInstalledPlugin(context.Background(), "plugin/1", PluginConfigRequest{
		Enabled:    &enabled,
		Metadata:   map[string]any{"env": "prod"},
		SecretRefs: map[string]string{"apiToken": "secret/ref"},
	})
	if err != nil {
		t.Fatalf("ConfigureInstalledPlugin returned error: %v", err)
	}
	if got := plugin.Status; got != "disabled" {
		t.Fatalf("configured installed plugin status = %q, want disabled", got)
	}
}

func TestAgentRunCallbackRequestEventsWireShape(t *testing.T) {
	legacy := AgentRunCallbackRequest{
		AgentID:       "agent-1",
		CallbackToken: "agent-run-token",
		Payload:       map[string]any{"summary": "completed"},
		RunID:         "run-1",
		Status:        "succeeded",
	}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatalf("marshal legacy callback: %v", err)
	}
	var legacyWire map[string]any
	if err := json.Unmarshal(raw, &legacyWire); err != nil {
		t.Fatalf("decode legacy callback JSON: %v", err)
	}
	if _, ok := legacyWire["events"]; ok {
		t.Fatal("legacy callback emitted events field, want omitted")
	}

	var thinkingDelta AgentRunCallbackWorkbenchStreamEvent
	if err := thinkingDelta.FromAgentRunCallbackWorkbenchThinkingDeltaEvent(AgentRunCallbackWorkbenchThinkingDeltaEvent{
		TextDelta: "Checking recent deployment events.",
		Type:      "thinking.delta",
	}); err != nil {
		t.Fatalf("build callback thinking.delta event: %v", err)
	}
	var toolDelta AgentRunCallbackWorkbenchStreamEvent
	if err := toolDelta.FromAgentRunCallbackWorkbenchToolDeltaEvent(AgentRunCallbackWorkbenchToolDeltaEvent{
		LogDelta:   "kubectl get events returned 12 rows",
		ToolCallID: "tool-1",
		Type:       "tool.delta",
	}); err != nil {
		t.Fatalf("build callback tool.delta event: %v", err)
	}
	var toolCompleted AgentRunCallbackWorkbenchStreamEvent
	if err := toolCompleted.FromAgentRunCallbackWorkbenchToolCompletedEvent(AgentRunCallbackWorkbenchToolCompletedEvent{
		ToolCall: WorkbenchToolCall{
			AdapterID: "kubernetes",
			ID:        "tool-1",
			Status:    "success",
			Summary:   "Collected recent warning events",
			ToolName:  "events.list",
		},
		Type: "tool.completed",
	}); err != nil {
		t.Fatalf("build callback tool.completed event: %v", err)
	}
	var messageDone AgentRunCallbackWorkbenchStreamEvent
	if err := messageDone.FromAgentRunCallbackWorkbenchMessageDoneEvent(AgentRunCallbackWorkbenchMessageDoneEvent{
		Content: "The rollout failed because the new pod image could not be pulled.",
		Role:    "assistant",
		Type:    "message.done",
	}); err != nil {
		t.Fatalf("build callback message.done event: %v", err)
	}

	withEvents := legacy
	withEvents.Events = []AgentRunCallbackWorkbenchStreamEvent{
		thinkingDelta,
		toolDelta,
		toolCompleted,
		messageDone,
	}
	raw, err = json.Marshal(withEvents)
	if err != nil {
		t.Fatalf("marshal callback with events: %v", err)
	}
	var eventWire map[string]any
	if err := json.Unmarshal(raw, &eventWire); err != nil {
		t.Fatalf("decode callback with events JSON: %v", err)
	}
	events, ok := eventWire["events"].([]any)
	if !ok || len(events) != 4 {
		t.Fatalf("events wire value = %#v, want four events", eventWire["events"])
	}
	wantTypes := []string{"thinking.delta", "tool.delta", "tool.completed", "message.done"}
	serverOwnedFields := []string{"id", "sessionId", "runId", "sequence", "createdAt"}
	for i, rawEvent := range events {
		event, ok := rawEvent.(map[string]any)
		if !ok {
			t.Fatalf("events[%d] = %#v, want object", i, rawEvent)
		}
		if got := event["type"]; got != wantTypes[i] {
			t.Fatalf("events[%d] type = %v, want %s", i, got, wantTypes[i])
		}
		for _, field := range serverOwnedFields {
			if _, ok := event[field]; ok {
				t.Fatalf("events[%d] emitted server-owned field %q in callback input", i, field)
			}
		}
	}

	var roundTrip AgentRunCallbackRequest
	if err := json.Unmarshal(raw, &roundTrip); err != nil {
		t.Fatalf("unmarshal callback with events: %v", err)
	}
	if len(roundTrip.Events) != 4 {
		t.Fatalf("round-trip events length = %d, want 4", len(roundTrip.Events))
	}
	thinking, err := roundTrip.Events[0].AsAgentRunCallbackWorkbenchThinkingDeltaEvent()
	if err != nil {
		t.Fatalf("decode round-trip thinking.delta event: %v", err)
	}
	if got := thinking.TextDelta; got != "Checking recent deployment events." {
		t.Fatalf("round-trip thinking.delta text = %q", got)
	}
	delta, err := roundTrip.Events[1].AsAgentRunCallbackWorkbenchToolDeltaEvent()
	if err != nil {
		t.Fatalf("decode round-trip tool.delta event: %v", err)
	}
	if got := delta.LogDelta; got != "kubectl get events returned 12 rows" {
		t.Fatalf("round-trip tool.delta log = %q", got)
	}
	completed, err := roundTrip.Events[2].AsAgentRunCallbackWorkbenchToolCompletedEvent()
	if err != nil {
		t.Fatalf("decode round-trip tool.completed event: %v", err)
	}
	if got := completed.ToolCall.Status; got != "success" {
		t.Fatalf("round-trip tool.completed status = %q", got)
	}
	done, err := roundTrip.Events[3].AsAgentRunCallbackWorkbenchMessageDoneEvent()
	if err != nil {
		t.Fatalf("decode round-trip message.done event: %v", err)
	}
	if got := done.Content; got != "The rollout failed because the new pod image could not be pulled." {
		t.Fatalf("round-trip event content = %q", got)
	}
	if done.CreatedAt != nil || done.ID != "" || done.SessionID != "" || done.RunID != "" || done.Sequence != 0 {
		t.Fatalf("round-trip message.done carried server-owned fields: %#v", done)
	}
}

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return NewClient(
		server.URL,
		WithBearerToken("test-token"),
		WithHTTPClient(server.Client()),
		WithUserAgent("sohaapi-test"),
	)
}

func decodeJSONRequestBody[T any](t *testing.T, r *http.Request) T {
	t.Helper()
	if got := r.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read request body: %v", err)
	}
	var body T
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode request body %q: %v", string(raw), err)
	}
	return body
}

func assertCommonRequest(t *testing.T, r *http.Request, method, path string) {
	t.Helper()
	if r.Method != method {
		t.Fatalf("method = %s, want %s", r.Method, method)
	}
	if got := r.URL.EscapedPath(); got != path {
		t.Fatalf("path = %q, want %q", got, path)
	}
	if got := r.Header.Get("Accept"); got != "application/json" {
		t.Fatalf("Accept = %q, want application/json", got)
	}
	if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
		t.Fatalf("Authorization = %q, want Bearer test-token", got)
	}
	if got := r.Header.Get("User-Agent"); got != "sohaapi-test" {
		t.Fatalf("User-Agent = %q, want sohaapi-test", got)
	}
}

func assertPublicRequest(t *testing.T, r *http.Request, method, path string) {
	t.Helper()
	if r.Method != method {
		t.Fatalf("method = %s, want %s", r.Method, method)
	}
	if got := r.URL.EscapedPath(); got != path {
		t.Fatalf("path = %q, want %q", got, path)
	}
	if got := r.Header.Get("Accept"); got != "application/json" {
		t.Fatalf("Accept = %q, want application/json", got)
	}
	if got := r.Header.Get("Authorization"); got != "" {
		t.Fatalf("Authorization = %q, want empty", got)
	}
	if got := r.Header.Get("User-Agent"); got != "sohaapi-test" {
		t.Fatalf("User-Agent = %q, want sohaapi-test", got)
	}
}

func assertQuery(t *testing.T, r *http.Request, want map[string]string) {
	t.Helper()
	got := r.URL.Query()
	if len(got) != len(want) {
		t.Fatalf("query keys = %v, want %v", got, want)
	}
	for key, value := range want {
		if gotValue := got.Get(key); gotValue != value {
			t.Fatalf("query[%s] = %q, want %q", key, gotValue, value)
		}
	}
}

func authResultJSON(expiresAt time.Time, accessToken string) map[string]any {
	return map[string]any{
		"data": map[string]any{
			"tokens": map[string]any{
				"accessToken":  accessToken,
				"expiresAt":    expiresAt,
				"expiresIn":    int64(3600),
				"refreshToken": "refresh-token",
				"tokenType":    "Bearer",
			},
			"user": principalJSON("user-1"),
		},
	}
}

func principalJSON(userID string) map[string]any {
	return map[string]any{
		"userId":         userID,
		"userName":       "admin",
		"email":          "admin@example.com",
		"roles":          []string{"admin"},
		"teams":          []string{"platform"},
		"projects":       []string{"default"},
		"tags":           []string{"ops"},
		"permissionKeys": []string{"auth.read"},
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("write response JSON: %v", err)
	}
}

func marketplacePluginJSON(id string, installed bool) map[string]any {
	return map[string]any{
		"id":        id,
		"name":      "Kubernetes SRE",
		"publisher": "OpenSoha",
		"riskLevel": "high",
		"source":    "marketplace",
		"summary":   "Kubernetes operations through OpenSoha",
		"type":      "skill-pack",
		"version":   "1.0.0",
		"installed": installed,
		"manifest":  pluginManifestJSON(id),
	}
}

func installedPluginJSON(id, status string, timestamp time.Time) map[string]any {
	value := map[string]any{
		"id":                   id,
		"name":                 "Kubernetes SRE",
		"publisher":            "OpenSoha",
		"type":                 "skill-pack",
		"version":              "1.0.0",
		"status":               status,
		"source":               "marketplace",
		"manifest":             pluginManifestJSON(id),
		"checksumStatus":       "verified",
		"signatureStatus":      "verified",
		"requestedPermissions": map[string]any{"required": []string{"mcp.capabilities.read"}},
		"configuredSecretRefs": map[string]string{"apiToken": "secret/ref"},
		"metadata":             map[string]any{"env": "prod"},
		"installedBy":          "user-1",
		"installedAt":          timestamp,
		"updatedAt":            timestamp,
	}
	if status == "enabled" {
		value["enabledAt"] = timestamp
	}
	if status == "disabled" {
		value["disabledAt"] = timestamp
	}
	return value
}

func pluginManifestJSON(id string) map[string]any {
	return map[string]any{
		"id":          id,
		"name":        "Kubernetes SRE",
		"publisher":   "OpenSoha",
		"type":        "skill-pack",
		"version":     "1.0.0",
		"description": "Kubernetes operations through OpenSoha",
		"capabilities": map[string]any{
			"tools": []string{"k8s.rollout"},
		},
		"permissions": map[string]any{
			"required": []string{"mcp.capabilities.read"},
		},
	}
}

func TestCLIAuthenticationTransportIsolation(t *testing.T) {
	cases := []struct {
		name, method, path string
		protected          bool
		call               func(*Client) error
	}{
		{"options", "GET", "/api/v1/auth/cli/options", false, func(c *Client) error { _, e := c.GetCLIAuthOptions(context.Background()); return e }},
		{"create", "POST", "/api/v1/auth/cli/device-authorizations", false, func(c *Client) error {
			_, e := c.CreateCLIDeviceAuthorization(context.Background(), CLIDeviceAuthorizationCreateRequest{})
			return e
		}},
		{"inspect", "POST", "/api/v1/auth/cli/device-authorizations/inspect", true, func(c *Client) error {
			_, e := c.InspectCLIDeviceAuthorization(context.Background(), CLIDeviceInspectRequest{UserCode: "001234"})
			return e
		}},
		{"decision", "POST", "/api/v1/auth/cli/device-authorizations/decision", true, func(c *Client) error {
			_, e := c.DecideCLIDeviceAuthorization(context.Background(), CLIDeviceDecisionRequest{})
			return e
		}},
		{"claim", "POST", "/api/v1/auth/cli/device-token", false, func(c *Client) error {
			_, e := c.ExchangeCLIDeviceToken(context.Background(), CLIDeviceTokenRequest{})
			return e
		}},
		{"cancel", "POST", "/api/v1/auth/cli/device-authorizations/cancel", false, func(c *Client) error {
			_, e := c.CancelCLIDeviceAuthorization(context.Background(), CLIDeviceCancelRequest{})
			return e
		}},
		{"password", "POST", "/api/v1/auth/cli/password", false, func(c *Client) error {
			_, e := c.CLIPasswordLogin(context.Background(), PasswordLoginRequest{})
			return e
		}},
		{"refresh", "POST", "/api/v1/auth/cli/refresh", false, func(c *Client) error {
			_, e := c.RefreshCLISession(context.Background(), CLIRefreshRequest{})
			return e
		}},
		{"logout", "POST", "/api/v1/auth/cli/logout", false, func(c *Client) error { return c.LogoutCLISession(context.Background(), CLILogoutRequest{}) }},
		{"context", "GET", "/api/v1/auth/context", true, func(c *Client) error { _, e := c.GetAuthContext(context.Background()); return e }},
		{"sessions", "GET", "/api/v1/auth/cli/sessions", true, func(c *Client) error {
			_, e := c.ListCLISessions(context.Background(), ListCLISessionsParams{})
			return e
		}},
		{"revoke", "POST", "/api/v1/auth/cli/sessions/session-1/revoke", true, func(c *Client) error { return c.RevokeCLISession(context.Background(), "session-1") }},
		{"rotation", "GET", "/api/v1/ai-gateway/token-rotations/request-1", true, func(c *Client) error { _, e := c.GetTokenRotation(context.Background(), "request-1"); return e }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				called = true
				if tc.protected {
					assertCommonRequest(t, r, tc.method, tc.path)
				} else {
					assertPublicRequest(t, r, tc.method, tc.path)
				}
				if tc.name == "inspect" {
					var body map[string]any
					if e := json.NewDecoder(r.Body).Decode(&body); e != nil || body["userCode"] != "001234" {
						t.Fatal("leading zeros lost")
					}
				}
				writeJSON(t, w, map[string]any{"data": map[string]any{}})
			})
			if e := tc.call(client); e != nil {
				t.Fatal(e)
			}
			if !called {
				t.Fatal("request was not sent")
			}
		})
	}
}
